package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/app"
	"github.com/egor-kardash/webhook-delivery-service/internal/config"
	"github.com/egor-kardash/webhook-delivery-service/internal/platform"
)

const startupTimeout = 3 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	appLog, err := platform.NewLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	slog.SetDefault(appLog)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	a, err := app.New(startCtx, cfg, appLog)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, a.Close())
	}()

	appLog.Info("connected to postgres",
		"host", cfg.Database.Host,
		"port", cfg.Database.Port,
		"db", cfg.Database.Name,
	)

	return a.Run(ctx)
}
