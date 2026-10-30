package server

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/route"

	"columba.local/backend/config"
	"columba.local/backend/internal/admission"
	"columba.local/backend/internal/routing"
	"columba.local/backend/logging"
	"columba.local/backend/storage"
)

// Server composes application services and their HTTP adapters. Construction is
// in bootstrap.go; lifecycle.go owns request draining and resource shutdown.
type Server struct {
	nativeScheduler     routing.NativeScheduler
	nativeTransportOnce sync.Once
	nativeTransport     http.RoundTripper
	config              *config.Config
	engine              *route.Engine
	store               *storage.Store
	logger              *logging.Logger
	startupErr          error

	// Routing components own their synchronization and mutable state.
	preparationOnce sync.Once
	preparation     *admission.Limiter

	refreshJobs sourceRefreshJobs
	catalog     *modelCatalog

	// Request producers stop before usage persistence is canceled and drained.
	usageWriterMu    sync.Mutex
	usageWriter      *usageWriterState
	usageWriteGen    atomic.Uint64 // Reset discards queued records from old generations.
	usageSeq         atomic.Uint64
	usageWriteCtx    context.Context
	usageWriteCancel context.CancelFunc
	usagePersistMu   sync.Mutex
	usageStatsMu     sync.Mutex
	usageStats       logging.Stats
	usageWriteErr    error
	usageCache       usageResponseCache
	usageRetention   *usageRetention
	assetsUsageMu    sync.Mutex
	assetsUsage      usageAssetsUsage
	assetsUsageAt    time.Time

	shutdownErr         error
	shutdownOnce        sync.Once
	lifecycleOnce       sync.Once
	lifecycleCtx        context.Context
	lifecycleCancel     context.CancelFunc
	shutdownRequested   chan struct{}
	shutdownRequestOnce sync.Once
	shutdownDone        chan struct{} // Closed only after the full shutdown sequence.
	httpRequests        httpRequests

	restartMu       sync.Mutex
	restartRequired bool
	configUpdateMu  sync.Mutex // Serializes persistence, publication and reload.

	// Test-only preflight bypass for local httptest upstreams; false in production.
	skipOutboundValidation bool
}
