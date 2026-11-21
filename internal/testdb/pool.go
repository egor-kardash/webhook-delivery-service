//go:build integration

package testdb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load test db config: %v", err)
	}

	ctx := t.Context()

	pool, err := pgxpool.New(ctx, cfg.DSN())
	if err != nil {
		t.Fatalf("create test db pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test db: %v", err)
	}

	if err := RequireDatabase(ctx, pool); err != nil {
		t.Fatalf("guard test db: %v", err)
	}

	if err := RequireSchema(ctx, pool, "webhook.subscriptions"); err != nil {
		t.Fatalf("guard test schema: %v", err)
	}

	if err := Reset(ctx, pool); err != nil {
		t.Fatalf("reset test db: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := Reset(ctx, pool); err != nil {
			t.Errorf("reset test db after test: %v", err)
		}
	})

	return pool
}

func Reset(ctx context.Context, pool *pgxpool.Pool) error {
	const query = `
		TRUNCATE webhook.deliveries, webhook.events, webhook.subscriptions
		RESTART IDENTITY CASCADE`

	if _, err := pool.Exec(ctx, query); err != nil {
		return fmt.Errorf("truncate test tables: %w", err)
	}

	return nil
}
