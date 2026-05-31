package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/curly-hub/podorel/internal/logging"
	"github.com/curly-hub/podorel/internal/systemd"
	"github.com/curly-hub/podorel/server/internal/app"
	"github.com/curly-hub/podorel/server/internal/config"
	"github.com/curly-hub/podorel/server/internal/db"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func main() {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(2)
	}
	if cfg.Mode.IsProduction() && cfg.Auth.AdminPassword == "" {
		_, _ = os.Stderr.WriteString("PODOREL_ADMIN_PASSWORD is required in production mode\n")
		os.Exit(2)
	}

	logger := logging.New(os.Stdout, cfg.Mode, "web")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info(ctx, "startup", "podorel web starting", map[string]any{
		"listen_addr": cfg.Server.ListenAddr,
		"mode":        cfg.Mode.String(),
	})

	store, err := db.Open(ctx, cfg.Database.Path, db.DefaultMigrationDir)
	if err != nil {
		logger.Error(ctx, "db_open", "database initialization failed", map[string]any{"error": err.Error(), "db_path": cfg.Database.Path})
		os.Exit(1)
	}
	defer store.Close()

	webApp, err := app.New(ctx, cfg, store, logger, app.Options{LogDir: os.Getenv("PODOREL_LOG_DIR")})
	if err != nil {
		logger.Error(ctx, "app_init", "application initialization failed", map[string]any{"error": err.Error()})
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           webApp.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	listener, err := net.Listen("tcp", cfg.Server.ListenAddr)
	if err != nil {
		logger.Error(ctx, "http_listen", "web server could not bind", map[string]any{
			"listen_addr": cfg.Server.ListenAddr,
			"error":       err.Error(),
		})
		os.Exit(1)
	}
	if err := systemd.Ready(); err != nil {
		logger.Error(ctx, "systemd_ready", "could not notify systemd readiness", map[string]any{"error": err.Error()})
	}
	if systemd.StartWatchdog(ctx, os.Getenv) {
		logger.Info(ctx, "systemd_watchdog", "systemd watchdog heartbeat enabled", nil)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error(context.Background(), "http_shutdown", "web server graceful shutdown failed", map[string]any{"error": err.Error()})
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(ctx, "http_listen", "web server stopped unexpectedly", map[string]any{
				"listen_addr": cfg.Server.ListenAddr,
				"error":       err.Error(),
			})
			os.Exit(1)
		}
	}
}
