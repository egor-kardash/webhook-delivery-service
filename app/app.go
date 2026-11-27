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
	"github.com/egor-kardash/webhook-delivery-service/internal/client"
	"github.com/egor-kardash/webhook-delivery-service/internal/config"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
	"github.com/egor-kardash/webhook-delivery-service/internal/store"
	"github.com/egor-kardash/webhook-delivery-service/internal/worker"
)

type App struct {
	pool            *pgxpool.Pool
	log             *slog.Logger
	server          *http.Server
	deliveryWorker  *worker.Worker
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

	eventStore := store.NewEventStore(pool)
	eventService := service.NewEventService(eventStore)
	eventHandler := api.NewEventHandler(eventService)

	webhookClient, err := client.New(client.Options{
		Timeout: cfg.Delivery.HTTPTimeout,
	})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("init webhook client: %w", err)
	}

	deliveryStore, err := store.NewDeliveryStore(pool, cfg.Delivery.Lease)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("init delivery store: %w", err)
	}

	deliveryService := service.NewDeliveryService(deliveryStore, webhookClient)

	deliveryHandler := api.NewDeliveryHandler(deliveryService)

	deliveryWorker, err := worker.New(deliveryService, worker.Options{
		PollInterval: cfg.Delivery.PollInterval,
		MaxPerTick:   cfg.Delivery.MaxPerTick,
	}, log)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("init delivery worker: %w", err)
	}

	router := api.NewRouter(log, pool, subHandler, eventHandler, deliveryHandler)

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
		deliveryWorker:  deliveryWorker,
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

	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})

	go func() {
		defer close(workerDone)

		if err := a.deliveryWorker.Run(workerCtx); err != nil {
			a.log.Error("delivery worker stopped", "error", err)
		}
	}()

	select {
	case <-ctx.Done():
		shutdownErr := a.shutdownHTTPServer()

		stopWorker()
		<-workerDone

		return shutdownErr

	case err := <-serveErr:
		stopWorker()
		<-workerDone

		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve http: %w", err)
	}
}

func (a *App) shutdownHTTPServer() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		_ = a.server.Close()
		return fmt.Errorf("shutdown http server: %w", err)
	}

	a.log.Info("http server stopped")

	return nil
}

func (a *App) Close() error {
	a.pool.Close()
	return nil
}
