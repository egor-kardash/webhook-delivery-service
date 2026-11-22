//go:build integration

package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/store"
	"github.com/egor-kardash/webhook-delivery-service/internal/testdb"
)

const (
	eventTestType          = "user.created"
	eventTestKey           = "event-idem-key-1"
	eventTestSecretKey     = "SRJHT7YRQUTAAHEZJQMSKGHCY4"
	failDeliveryConstraint = "test_event_fail_deliveries"
)

func seedEventSubscription(t *testing.T, pool *pgxpool.Pool, targetURL string, eventTypes []string, active bool) {
	t.Helper()

	_, err := pool.Exec(
		t.Context(),
		`INSERT INTO webhook.subscriptions (target_url, secret_key, event_types, active)
		 VALUES ($1, $2, $3, $4)`,
		targetURL,
		eventTestSecretKey,
		eventTypes,
		active,
	)
	if err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
}

func TestEventStorePublishEventWithDeliveries(t *testing.T) {
	t.Run("fans out only to active matching subscriptions", func(t *testing.T) {
		pool := testdb.New(t)
		eventStore := store.NewEventStore(pool)

		seedEventSubscription(t, pool, "https://active-one.example", []string{eventTestType}, true)
		seedEventSubscription(t, pool, "https://active-two.example", []string{eventTestType, "order.paid"}, true)
		seedEventSubscription(t, pool, "https://active-other.example", []string{"order.paid"}, true)
		seedEventSubscription(t, pool, "https://inactive.example", []string{eventTestType}, false)

		event := &domain.Event{
			EventType:      eventTestType,
			Payload:        json.RawMessage(`{"id":1}`),
			IdempotencyKey: eventTestKey,
		}

		result, err := eventStore.PublishEventWithDeliveries(t.Context(), event)
		if err != nil {
			t.Fatalf("PublishEventWithDeliveries: %v", err)
		}

		if !result.Created {
			t.Error("Created = false, want true")
		}
		if result.EventID <= 0 {
			t.Errorf("EventID = %d, want > 0", result.EventID)
		}
		if len(result.DeliveryIDs) != 2 {
			t.Fatalf("DeliveryIDs = %v, want 2 deliveries", result.DeliveryIDs)
		}

		rows, err := pool.Query(
			t.Context(),
			`SELECT s.target_url, d.status, d.attempts, d.next_attempt_at
			 FROM webhook.deliveries d
			 JOIN webhook.subscriptions s ON s.id = d.subscription_id
			 WHERE d.event_id = $1
			 ORDER BY s.target_url`,
			result.EventID,
		)
		if err != nil {
			t.Fatalf("select deliveries: %v", err)
		}
		defer rows.Close()

		var gotURLs []string
		for rows.Next() {
			var (
				targetURL     string
				status        string
				attempts      int
				nextAttemptAt time.Time
			)
			if err := rows.Scan(&targetURL, &status, &attempts, &nextAttemptAt); err != nil {
				t.Fatalf("scan delivery: %v", err)
			}

			if status != "pending" {
				t.Errorf("delivery status for %s = %q, want %q", targetURL, status, "pending")
			}
			if attempts != 0 {
				t.Errorf("delivery attempts for %s = %d, want 0", targetURL, attempts)
			}
			if nextAttemptAt.IsZero() {
				t.Errorf("delivery next_attempt_at for %s is zero, want non-zero", targetURL)
			}

			gotURLs = append(gotURLs, targetURL)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate deliveries: %v", err)
		}

		wantURLs := []string{"https://active-one.example", "https://active-two.example"}
		if !slices.Equal(gotURLs, wantURLs) {
			t.Errorf("delivery targets = %v, want %v", gotURLs, wantURLs)
		}

		var (
			gotType        string
			gotKey         string
			gotPayload     string
			payloadMatches bool
		)
		err = pool.QueryRow(
			t.Context(),
			`SELECT event_type, idempotency_key, payload::text, payload = $2::jsonb
			 FROM webhook.events
			 WHERE id = $1`,
			result.EventID,
			string(event.Payload),
		).Scan(&gotType, &gotKey, &gotPayload, &payloadMatches)
		if err != nil {
			t.Fatalf("select event: %v", err)
		}
		if gotType != eventTestType {
			t.Errorf("event_type = %q, want %q", gotType, eventTestType)
		}
		if gotKey != eventTestKey {
			t.Errorf("idempotency_key = %q, want %q", gotKey, eventTestKey)
		}
		if !payloadMatches {
			t.Errorf("payload = %s, want %s", gotPayload, event.Payload)
		}
	})

	t.Run("no matching subscriptions gives zero deliveries", func(t *testing.T) {
		pool := testdb.New(t)
		eventStore := store.NewEventStore(pool)

		seedEventSubscription(t, pool, "https://other.example", []string{"order.paid"}, true)

		event := &domain.Event{
			EventType:      eventTestType,
			Payload:        json.RawMessage(`{}`),
			IdempotencyKey: eventTestKey,
		}

		result, err := eventStore.PublishEventWithDeliveries(t.Context(), event)
		if err != nil {
			t.Fatalf("PublishEventWithDeliveries: %v", err)
		}

		if !result.Created {
			t.Error("Created = false, want true")
		}
		if result.EventID <= 0 {
			t.Errorf("EventID = %d, want > 0", result.EventID)
		}
		if result.DeliveryIDs == nil {
			t.Error("DeliveryIDs = nil, want empty non-nil slice")
		}
		if len(result.DeliveryIDs) != 0 {
			t.Errorf("DeliveryIDs = %v, want empty", result.DeliveryIDs)
		}

		var count int
		if err := pool.QueryRow(
			t.Context(),
			`SELECT count(*) FROM webhook.deliveries WHERE event_id = $1`,
			result.EventID,
		).Scan(&count); err != nil {
			t.Fatalf("count deliveries: %v", err)
		}
		if count != 0 {
			t.Errorf("delivery count = %d, want 0", count)
		}
	})

	t.Run("duplicate idempotency key returns same event without new deliveries", func(t *testing.T) {
		pool := testdb.New(t)
		eventStore := store.NewEventStore(pool)

		seedEventSubscription(t, pool, "https://active.example", []string{eventTestType}, true)

		event := &domain.Event{
			EventType:      eventTestType,
			Payload:        json.RawMessage(`{"id":1}`),
			IdempotencyKey: eventTestKey,
		}

		first, err := eventStore.PublishEventWithDeliveries(t.Context(), event)
		if err != nil {
			t.Fatalf("first publish: %v", err)
		}
		second, err := eventStore.PublishEventWithDeliveries(t.Context(), event)
		if err != nil {
			t.Fatalf("second publish: %v", err)
		}

		if !first.Created {
			t.Error("first.Created = false, want true")
		}
		if second.Created {
			t.Error("second.Created = true, want false")
		}
		if second.EventID != first.EventID {
			t.Errorf("second EventID = %d, want %d", second.EventID, first.EventID)
		}
		if !slices.Equal(second.DeliveryIDs, first.DeliveryIDs) {
			t.Errorf("second DeliveryIDs = %v, want %v", second.DeliveryIDs, first.DeliveryIDs)
		}

		var eventCount, deliveryCount int
		if err := pool.QueryRow(
			t.Context(),
			`SELECT count(*) FROM webhook.events WHERE idempotency_key = $1`,
			eventTestKey,
		).Scan(&eventCount); err != nil {
			t.Fatalf("count events: %v", err)
		}
		if err := pool.QueryRow(
			t.Context(),
			`SELECT count(*) FROM webhook.deliveries WHERE event_id = $1`,
			first.EventID,
		).Scan(&deliveryCount); err != nil {
			t.Fatalf("count deliveries: %v", err)
		}
		if eventCount != 1 {
			t.Errorf("event count = %d, want 1", eventCount)
		}
		if deliveryCount != len(first.DeliveryIDs) {
			t.Errorf("delivery count = %d, want %d", deliveryCount, len(first.DeliveryIDs))
		}
	})

	t.Run("event insert is rolled back when delivery insert fails", func(t *testing.T) {
		pool := testdb.New(t)
		eventStore := store.NewEventStore(pool)

		seedEventSubscription(t, pool, "https://active.example", []string{eventTestType}, true)

		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := pool.Exec(
				ctx,
				`ALTER TABLE webhook.deliveries DROP CONSTRAINT IF EXISTS `+failDeliveryConstraint,
			); err != nil {
				t.Errorf("drop test constraint: %v", err)
			}
		})

		if _, err := pool.Exec(
			t.Context(),
			`ALTER TABLE webhook.deliveries DROP CONSTRAINT IF EXISTS `+failDeliveryConstraint,
		); err != nil {
			t.Fatalf("drop previous constraint: %v", err)
		}
		if _, err := pool.Exec(
			t.Context(),
			`ALTER TABLE webhook.deliveries
			 ADD CONSTRAINT `+failDeliveryConstraint+` CHECK (false) NOT VALID`,
		); err != nil {
			t.Fatalf("add failing constraint: %v", err)
		}

		event := &domain.Event{
			EventType:      eventTestType,
			Payload:        json.RawMessage(`{"id":1}`),
			IdempotencyKey: eventTestKey,
		}

		_, err := eventStore.PublishEventWithDeliveries(t.Context(), event)
		if err == nil {
			t.Fatal("expected delivery insert error, got nil")
		}

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
		}
		if pgErr.Code != "23514" {
			t.Errorf("SQLSTATE = %q, want %q", pgErr.Code, "23514")
		}

		var eventCount, deliveryCount int
		if err := pool.QueryRow(
			t.Context(),
			`SELECT count(*) FROM webhook.events WHERE idempotency_key = $1`,
			eventTestKey,
		).Scan(&eventCount); err != nil {
			t.Fatalf("count events after rollback: %v", err)
		}
		if err := pool.QueryRow(
			t.Context(),
			`SELECT count(*) FROM webhook.deliveries`,
		).Scan(&deliveryCount); err != nil {
			t.Fatalf("count deliveries after rollback: %v", err)
		}
		if eventCount != 0 {
			t.Errorf("event count after rollback = %d, want 0", eventCount)
		}
		if deliveryCount != 0 {
			t.Errorf("delivery count after rollback = %d, want 0", deliveryCount)
		}
	})
}
