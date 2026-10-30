package main

import (
	"context"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"

	"columba.local/backend/config"
	"columba.local/backend/logging"
	"columba.local/backend/server"
	"columba.local/backend/storage"
)

func main() {
	if err := run(); err != nil {
		logging.Emergency("backend stopped with an error", err)
		os.Exit(1)
	}
}

// run owns process-level initialization. Importing config or server never parses
// command-line flags, creates configuration files, or terminates the process.
func run() error {
	configPath := flag.String("config", "config.json", "Path to config file")
	resetPassword := flag.Bool("reset-panel-password", false, "Reset the local management password and exit")
	flag.Parse()
	if *configPath == "" {
		*configPath = "config.json"
	}
	cfg, created, err := config.EnsureConfig(*configPath)
	if err != nil {
		return err
	}
	logging.Default().Configure(cfg.GetLoggingOptions())
	keyPath := cfg.SecretKeyPath
	if os.Getenv("COLUMBA_MASTER_KEY") != "" {
		keyPath = ""
	}
	if err := storage.UpgradeDatabases(cfg.GetDatabasePath(), cfg.GetLogDatabasePath(), *configPath, keyPath); err != nil {
		return err
	}
	if created {
		logging.Info().Str("path", *configPath).Msg("created local configuration")
	}
	logging.Info().Str("component", "config").Msg("config loaded successfully")
	if *resetPassword {
		store, err := storage.OpenWithLogPath(cfg.GetDatabasePath(), cfg.GetLogDatabasePath(), cfg.GetDBEncryptionKey())
		if err != nil {
			return err
		}
		defer store.Close()
		if err := store.ResetPanelPassword(context.Background()); err != nil {
			return err
		}
		logging.Info().Msg("management password reset; open the local panel to set a new password")
		return nil
	}

	// Capture termination even while startup is opening stores and preparing
	// model catalogs. All shutdown sources use the server's RequestShutdown entry point.
	signalContext, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopSignals()
	srv := server.New(cfg)
	logging.SetDefault(srv.Logger())
	go func() { <-signalContext.Done(); srv.RequestShutdown() }()
	if os.Getenv("COLUMBA_PARENT_STDIN") == "1" {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			srv.RequestShutdown()
		}()
	}

	logging.Info().Str("version", server.BuildIdentity()).Str("host", cfg.Host).Int("port", cfg.Port).Msg("starting Columba backend")

	return srv.ListenAndServe()
}
