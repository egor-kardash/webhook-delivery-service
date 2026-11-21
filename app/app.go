package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/egor-kardash/webhook-delivery-service/internal/api"
	"github.com/egor-kardash/webhook-delivery-service/internal/config"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
	"github.com/egor-kardash/webhook-delivery-service/internal/store"
)

type App struct {
	pool            *pgxpool.Pool
	log             *slog.Logger
	server          *http.Server
	shutdownTimeout time.Duration
}

func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	pool, err := store.NewPool(ctx, cfg.Database.DSN())
	if err != nil {
		return nil, fmt.Errorf("init postgres: %w", err)
	}

	subStore := store.NewSubscriptionStore(pool)
	subService := service.NewSubscriptionService(subStore)
	subHandler := api.NewSubscriptionHandler(subService)

	router := api.NewRouter(log, pool, subHandler)

	server := api.NewServer(
		api.ServerOptions{
			Address:           cfg.HTTPServer.Address,
			ReadHeaderTimeout: cfg.HTTPServer.ReadHeaderTimeout,
			ReadTimeout:       cfg.HTTPServer.ReadTimeout,
			WriteTimeout:      cfg.HTTPServer.WriteTimeout,
			IdleTimeout:       cfg.HTTPServer.IdleTimeout,
		},
		router,
		log,
	)

	return &App{
		pool:            pool,
		log:             log,
		server:          server,
		shutdownTimeout: cfg.HTTPServer.ShutdownTimeout,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.server.Addr, err)
	}

	a.log.Info("http server listening", "addr", ln.Addr().String())

	serveErr := make(chan error, 1)

	go func() {
		serveErr <- a.server.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			_ = a.server.Close()
			return fmt.Errorf("shutdown http server: %w", err)
		}

		a.log.Info("http server stopped")
		return nil

	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve http: %w", err)
	}
}

func (a *App) Close() error {
	a.pool.Close()
	return nil
}
