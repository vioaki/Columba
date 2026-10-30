package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"columba.local/backend/config"
	"columba.local/backend/logging"
	"columba.local/backend/storage"
)

func New(cfg *config.Config) *Server {

	engine := newHTTPEngine(cfg)
	server := &Server{
		config: cfg, engine: engine,
		catalog: newModelCatalog(cfg.GetModelCatalog, func() string {
			dbPath := cfg.GetDatabasePath()
			if strings.TrimSpace(dbPath) == "" {
				return ""
			}
			return filepath.Join(filepath.Dir(dbPath), "model-catalog.json")
		}),
	}
	server.initLifecycle()
	if err := cfg.ValidatePanelAccess(); err != nil {
		server.startupErr = err
		return server
	}
	server.logger = logging.New(cfg.GetLoggingOptions(), nil)
	engine.Use(server.requestLifecycle(), server.requestLogging())
	store, err := storage.OpenWithLogPath(cfg.GetDatabasePath(), cfg.GetLogDatabasePath(), cfg.GetDBEncryptionKey())
	if err != nil {
		server.startupErr = fmt.Errorf("open sqlite store: %w", err)
		return server
	}
	server.store = store
	server.logger = logging.New(cfg.GetLoggingOptions(), logging.NewWriter(store.LogStore.WriteSystemLogBatch))
	if hasEncrypted, decryptOK, err := store.SecretIntegrityProbe(context.Background()); err != nil {
		server.logEvent("error").Err(err).Msg("secret integrity probe failed")
	} else if hasEncrypted && !decryptOK {
		server.logEvent("error").Msg("encrypted secrets cannot be decrypted; restore the original .master-key or COLUMBA_MASTER_KEY. Routing and token/source administration are unavailable until repaired; rows are retained")
	}
	server.syncOutboundPolicy()
	listen := cfg.GetServer()
	server.logSystemEvent("info", "server started", map[string]any{
		"version": AppVersion, "host": listen.Host, "port": listen.Port,
	})
	return server
}

func newUsageRetention(s *Server) *usageRetention {
	ctx, cancel := context.WithCancel(context.Background())
	return &usageRetention{
		store: s.store, usageLogConfig: s.usageLogConfig,
		systemLogConfig: func() config.LogRetentionResolved {
			if s.config == nil {
				return config.LogRetentionResolved{}
			}
			return s.config.GetSystemLogConfig()
		},
		usageAssetsRoot: s.usageAssetsRoot, persistMu: &s.usagePersistMu,
		changed: func() { s.usageCache.flush(); s.usageSeq.Add(1) },
		ctx:     ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}),
		stats: retentionStats{State: "idle", Phase: "idle"},
	}
}

func (s *Server) sourceRefreshJobs() *sourceRefreshJobs {
	s.initLifecycle()
	j := &s.refreshJobs
	j.once.Do(func() {
		j.ctx, j.refresh = s.lifecycleCtx, s.refreshSourceByID
		j.exists = func(ctx context.Context, id string) (bool, error) {
			_, found, err := s.findSourceByID(ctx, id)
			return found, err
		}
		j.invalidate, j.log = s.invalidateRouteCache, s.logSystemEvent
	})
	return j
}
