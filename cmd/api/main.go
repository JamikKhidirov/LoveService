package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/loveservice/loveservice/internal/app"
	"github.com/loveservice/loveservice/internal/config"
	"github.com/loveservice/loveservice/internal/observability"
)

var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	log := observability.NewLogger(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, log, version)
	if err != nil {
		log.Error("startup failed", "error", err)
		os.Exit(1)
	}
	if err := application.Run(ctx); err != nil {
		log.Error("server failed", "error", err)
		os.Exit(1)
	}
}
