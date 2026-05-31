package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/curly-hub/podorel/agent/internal/config"
	"github.com/curly-hub/podorel/agent/internal/ipc"
	"github.com/curly-hub/podorel/agent/internal/podman"
	"github.com/curly-hub/podorel/internal/logging"
	"github.com/curly-hub/podorel/internal/systemd"
)

func main() {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(2)
	}

	if err := config.ValidateTokenFilePermissions(cfg.TokenFile); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}

	token, err := config.ReadToken(cfg.TokenFile)
	if err != nil && cfg.Mode.IsProduction() {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}

	logger := logging.New(os.Stdout, cfg.Mode, "agent")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info(ctx, "startup", "podorel agent starting", map[string]any{
		"mode":        cfg.Mode.String(),
		"socket_path": cfg.SocketPath,
	})

	server := ipc.Server{
		SocketPath: cfg.SocketPath,
		Token:      token,
		Mode:       cfg.Mode,
		Logger:     logger,
		Runtime:    podman.NewDefaultRuntime(logger),
		OnReady: func() error {
			err := systemd.Ready()
			if systemd.StartWatchdog(ctx, os.Getenv) {
				logger.Info(ctx, "systemd_watchdog", "systemd watchdog heartbeat enabled", nil)
			}
			return err
		},
	}
	if err := server.Serve(ctx); err != nil {
		logger.Error(ctx, "ipc_serve", "agent stopped unexpectedly", map[string]any{
			"socket_path": cfg.SocketPath,
			"error":       err.Error(),
		})
		os.Exit(1)
	}
}
