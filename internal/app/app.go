// Package app is the composition root: it builds the dependency container and
// owns the process lifecycle (HTTP server, outbox publisher, event consumer and
// graceful shutdown).
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/loveservice/loveservice/internal/config"
)

type App struct {
	*Container
	server *http.Server
	cancel context.CancelFunc
}

func New(ctx context.Context, cfg *config.Config, log *slog.Logger, version string) (*App, error) {
	container, err := NewContainer(ctx, cfg, log, version)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           container.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return &App{Container: container, server: srv}, nil
}

func (a *App) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	defer cancel()

	a.hubSubscribe(runCtx)
	go a.Publisher.Run(runCtx)
	if a.Consumer != nil {
		go a.Consumer.Run(runCtx)
	}

	errCh := make(chan error, 1)
	go func() {
		a.Logger.Info("http server listening", "addr", a.Config.HTTPAddr, "version", a.Version,
			"storage", a.Config.StorageDriver, "swagger", a.Config.PublicBaseURL+"/docs")
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		a.Logger.Info("shutdown signal received")
	case err := <-errCh:
		cancel()
		return err
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = a.server.Shutdown(shutdownCtx)
	a.Close()
	a.Logger.Info("shutdown complete")
	return nil
}

func (a *App) hubSubscribe(ctx context.Context) {
	if a.Cache == nil || !a.Cache.Available() {
		a.Logger.Debug("websocket fanout is local-only (redis unavailable)")
		return
	}
	a.Hub.SubscribeRedis(ctx)
	a.Logger.Debug("websocket redis fanout enabled")
}
