//go:build integration

package store_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/store"
	"github.com/egor-kardash/webhook-delivery-service/internal/testdb"
)

const (
	validTargetURL         = "https://example.com"
	testSecretKey          = "SRJHT7YRQUTAAHEZJQMSKGHCY4"
	sqlstateCheckViolation = "23514"
)

func TestSubscriptionStoreCreate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		pool := testdb.New(t)
		subStore := store.NewSubscriptionStore(pool)

		in := &domain.Subscription{
			TargetURL:  validTargetURL,
			SecretKey:  testSecretKey,
			EventTypes: []string{"user.created", "order.paid"},
			Active:     true,
		}

		created, err := subStore.Create(t.Context(), in)
		if err != nil {
			t.Fatalf("store.Create: %v", err)
		}

		if created.ID <= 0 {
			t.Errorf("created.ID = %d, want > 0", created.ID)
		}
		if created.CreatedAt.IsZero() {
			t.Error("created.CreatedAt is zero, want non-zero timestamp")
		}

		if created.TargetURL != in.TargetURL {
			t.Errorf("created.TargetURL = %q, want %q", created.TargetURL, in.TargetURL)
		}
		if created.SecretKey != in.SecretKey {
			t.Error("created.SecretKey does not match the input")
		}
		if !slices.Equal(created.EventTypes, in.EventTypes) {
			t.Errorf("created.EventTypes = %v, want %v", created.EventTypes, in.EventTypes)
		}
		if !created.Active {
			t.Error("created.Active = false, want true")
		}

		var (
			dbEventTypes []string
			dbEventType  string
		)
		err = pool.QueryRow(
			t.Context(),
			`SELECT pg_typeof(event_types)::text, event_types
			 FROM webhook.subscriptions
			 WHERE id = $1`,
			created.ID,
		).Scan(&dbEventType, &dbEventTypes)
		if err != nil {
			t.Fatalf("select event_types directly: %v", err)
		}

		if dbEventType != "text[]" {
			t.Errorf("event_types column type = %q, want %q", dbEventType, "text[]")
		}
		if !slices.Equal(dbEventTypes, in.EventTypes) {
			t.Errorf("db event_types = %v, want %v", dbEventTypes, in.EventTypes)
		}
	})

	t.Run("db constraints backstop", func(t *testing.T) {
		pool := testdb.New(t)
		subStore := store.NewSubscriptionStore(pool)

		badSub := &domain.Subscription{
			TargetURL:  validTargetURL,
			SecretKey:  "too-short",
			EventTypes: []string{"user.created"},
			Active:     true,
		}

		_, err := subStore.Create(t.Context(), badSub)
		if err == nil {
			t.Fatal("expected database check constraint error, got nil")
		}

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
		}
		if pgErr.Code != sqlstateCheckViolation {
			t.Errorf("SQLSTATE = %q, want %q", pgErr.Code, sqlstateCheckViolation)
		}
	})
}
