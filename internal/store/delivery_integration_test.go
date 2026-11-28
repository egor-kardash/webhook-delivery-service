//go:build integration

package store_test

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/store"
	"github.com/egor-kardash/webhook-delivery-service/internal/testdb"
)

const (
	deliveryTestEventType = "user.created"
	deliveryTestKey       = "delivery-test-key"
	deliveryTestSecretKey = "SRJHT7YRQUTAAHEZJQMSKGHCY4"
)

func seedPendingDelivery(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	_, err := pool.Exec(
		t.Context(),
		`INSERT INTO webhook.subscriptions (target_url, secret_key, event_types, active)
		 VALUES ($1, $2, $3, true)`,
		"https://example.com",
		deliveryTestSecretKey,
		[]string{deliveryTestEventType},
	)
	if err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	eventStore := store.NewEventStore(pool)
	result, err := eventStore.PublishEventWithDeliveries(t.Context(), &domain.Event{
		EventType:      deliveryTestEventType,
		Payload:        json.RawMessage(`{"id":1}`),
		IdempotencyKey: deliveryTestKey,
	})
	if err != nil {
		t.Fatalf("publish event: %v", err)
	}
	if len(result.DeliveryIDs) != 1 {
		t.Fatalf("len(DeliveryIDs) = %d, want 1", len(result.DeliveryIDs))
	}

	return result.DeliveryIDs[0]
}

func TestDeliveryStoreClaimOneDue(t *testing.T) {
	t.Run("claims one due delivery and leases it", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		job, err := deliveryStore.ClaimOneDue(t.Context())
		if err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if job == nil {
			t.Fatal("store.ClaimOneDue returned nil, want job")
		}
		if job.ID != deliveryID {
			t.Errorf("job.ID = %d, want %d", job.ID, deliveryID)
		}
		if string(job.Payload) != `{"id": 1}` {
			t.Errorf("job.Payload = %s, want %s", job.Payload, `{"id": 1}`)
		}

		var (
			status string
			locked *time.Time
		)
		err = pool.QueryRow(
			t.Context(),
			`SELECT status, locked_until FROM webhook.deliveries WHERE id = $1`,
			deliveryID,
		).Scan(&status, &locked)
		if err != nil {
			t.Fatalf("select delivery: %v", err)
		}
		if status != "in_progress" {
			t.Errorf("status = %q, want %q", status, "in_progress")
		}
		if locked == nil || locked.IsZero() {
			t.Error("locked_until is nil/zero, want lease")
		}

		second, err := deliveryStore.ClaimOneDue(t.Context())
		if err != nil {
			t.Fatalf("second store.ClaimOneDue: %v", err)
		}
		if second != nil {
			t.Errorf("second claim = %+v, want nil", second)
		}
	})

	t.Run("two workers get at most one delivery", func(t *testing.T) {
		pool := testdb.New(t)
		seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		start := make(chan struct{})
		jobs := make(chan *domain.DeliveryJob, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup

		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				job, err := deliveryStore.ClaimOneDue(t.Context())
				jobs <- job
				errs <- err
			}()
		}

		close(start)
		wg.Wait()
		close(jobs)
		close(errs)

		for err := range errs {
			if err != nil {
				t.Fatalf("store.ClaimOneDue: %v", err)
			}
		}

		var claimed int
		for job := range jobs {
			if job != nil {
				claimed++
			}
		}
		if claimed != 1 {
			t.Errorf("claimed = %d, want 1", claimed)
		}
	})

	t.Run("does not claim a future delivery", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		if _, err := pool.Exec(
			t.Context(),
			`UPDATE webhook.deliveries SET next_attempt_at = now() + interval '1 hour' WHERE id = $1`,
			deliveryID,
		); err != nil {
			t.Fatalf("move next_attempt_at: %v", err)
		}

		job, err := deliveryStore.ClaimOneDue(t.Context())
		if err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if job != nil {
			t.Errorf("job = %+v, want nil for future delivery", job)
		}
	})

	t.Run("does not claim a dead delivery", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		if _, err := pool.Exec(
			t.Context(),
			`UPDATE webhook.deliveries SET status = 'dead', locked_until = NULL WHERE id = $1`,
			deliveryID,
		); err != nil {
			t.Fatalf("mark dead: %v", err)
		}

		job, err := deliveryStore.ClaimOneDue(t.Context())
		if err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if job != nil {
			t.Errorf("job = %+v, want nil for dead delivery", job)
		}
	})
}

func TestDeliveryStoreRecordAttempt(t *testing.T) {
	t.Run("updates attempts and last response fields", func(t *testing.T) {
		pool := testdb.New(t)
		seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		job, err := deliveryStore.ClaimOneDue(t.Context())
		if err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if job == nil {
			t.Fatal("store.ClaimOneDue returned nil, want job")
		}

		code := 500
		err = deliveryStore.RecordAttempt(t.Context(), domain.AttemptResult{
			DeliveryID:     job.ID,
			Status:         domain.DeliveryPending,
			RetryAfter:     time.Minute,
			LastStatusCode: &code,
			LastError:      "server error",
		})
		if err != nil {
			t.Fatalf("store.RecordAttempt: %v", err)
		}

		var (
			status        string
			attempts      int
			locked        *time.Time
			nextAttemptAt time.Time
			lastCode      *int
			lastError     *string
		)
		err = pool.QueryRow(t.Context(), `
			SELECT status, attempts, locked_until, next_attempt_at, last_status_code, last_error
			FROM webhook.deliveries WHERE id = $1`, job.ID).
			Scan(&status, &attempts, &locked, &nextAttemptAt, &lastCode, &lastError)
		if err != nil {
			t.Fatalf("select delivery: %v", err)
		}
		if status != "pending" {
			t.Errorf("status = %q, want %q", status, "pending")
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1", attempts)
		}
		if locked != nil {
			t.Errorf("locked_until = %v, want nil", locked)
		}
		if nextAttemptAt.IsZero() {
			t.Error("next_attempt_at is zero, want future time")
		}
		if !nextAttemptAt.After(time.Now()) {
			t.Errorf("next_attempt_at = %v, want after now", nextAttemptAt)
		}
		if lastCode == nil || *lastCode != code {
			t.Errorf("last_status_code = %v, want %d", lastCode, code)
		}
		if lastError == nil || *lastError != "server error" {
			t.Errorf("last_error = %v, want %q", lastError, "server error")
		}
	})

	t.Run("rejects stale lease", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		job, err := deliveryStore.ClaimOneDue(t.Context())
		if err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if job == nil {
			t.Fatal("store.ClaimOneDue returned nil, want job")
		}

		_, err = pool.Exec(
			t.Context(),
			`UPDATE webhook.deliveries SET locked_until = now() - interval '1 second' WHERE id = $1`,
			deliveryID,
		)
		if err != nil {
			t.Fatalf("expire lease: %v", err)
		}

		err = deliveryStore.RecordAttempt(t.Context(), domain.AttemptResult{
			DeliveryID: deliveryID,
			Status:     domain.DeliveryDelivered,
		})
		if !errors.Is(err, domain.ErrLeaseLost) {
			t.Errorf("store.RecordAttempt error = %v, want %v", err, domain.ErrLeaseLost)
		}
	})
}

func TestDeliveryStoreGetByID(t *testing.T) {
	t.Run("returns existing delivery", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		delivery, err := deliveryStore.GetByID(t.Context(), deliveryID)
		if err != nil {
			t.Fatalf("store.GetByID: %v", err)
		}

		if delivery.ID != deliveryID {
			t.Errorf("ID = %d, want %d", delivery.ID, deliveryID)
		}
		if delivery.EventID <= 0 {
			t.Errorf("EventID = %d, want > 0", delivery.EventID)
		}
		if delivery.SubscriptionID <= 0 {
			t.Errorf("SubscriptionID = %d, want > 0", delivery.SubscriptionID)
		}
		if delivery.Status != domain.DeliveryPending {
			t.Errorf("Status = %q, want %q", delivery.Status, domain.DeliveryPending)
		}
		if delivery.Attempts != 0 {
			t.Errorf("Attempts = %d, want 0", delivery.Attempts)
		}
		if delivery.NextAttemptAt.IsZero() {
			t.Error("NextAttemptAt is zero, want non-zero")
		}
		if delivery.LastStatusCode != 0 {
			t.Errorf("LastStatusCode = %d, want 0", delivery.LastStatusCode)
		}
		if delivery.LastError != "" {
			t.Errorf("LastError = %q, want empty", delivery.LastError)
		}
		if delivery.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero, want non-zero")
		}
		if delivery.UpdatedAt.IsZero() {
			t.Error("UpdatedAt is zero, want non-zero")
		}
	})

	t.Run("not found", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		_, err = deliveryStore.GetByID(t.Context(), 999999)
		if !errors.Is(err, domain.ErrDeliveryNotFound) {
			t.Errorf("store.GetByID error = %v, want %v", err, domain.ErrDeliveryNotFound)
		}
	})
}

func TestDeliveryStoreReleaseExpiredLeases(t *testing.T) {
	t.Run("returns pending and increments attempts", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		if _, err := deliveryStore.ClaimOneDue(t.Context()); err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if _, err := pool.Exec(
			t.Context(),
			`UPDATE webhook.deliveries SET locked_until = now() - interval '1 second' WHERE id = $1`,
			deliveryID,
		); err != nil {
			t.Fatalf("expire lease: %v", err)
		}

		released, err := deliveryStore.ReleaseExpiredLeases(t.Context(), 5)
		if err != nil {
			t.Fatalf("store.ReleaseExpiredLeases: %v", err)
		}
		if released != 1 {
			t.Errorf("released = %d, want 1", released)
		}

		var (
			status    string
			attempts  int
			locked    *time.Time
			lastError *string
		)
		if err := pool.QueryRow(
			t.Context(),
			`SELECT status, attempts, locked_until, last_error
			 FROM webhook.deliveries WHERE id = $1`,
			deliveryID,
		).Scan(&status, &attempts, &locked, &lastError); err != nil {
			t.Fatalf("select delivery: %v", err)
		}
		if status != "pending" {
			t.Errorf("status = %q, want %q", status, "pending")
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1", attempts)
		}
		if locked != nil {
			t.Errorf("locked_until = %v, want nil", locked)
		}
		if lastError == nil || *lastError != "lease expired" {
			t.Errorf("last_error = %v, want %q", lastError, "lease expired")
		}
	})

	t.Run("marks dead when max attempts reached", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		if _, err := deliveryStore.ClaimOneDue(t.Context()); err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if _, err := pool.Exec(
			t.Context(),
			`UPDATE webhook.deliveries SET locked_until = now() - interval '1 second' WHERE id = $1`,
			deliveryID,
		); err != nil {
			t.Fatalf("expire lease: %v", err)
		}

		released, err := deliveryStore.ReleaseExpiredLeases(t.Context(), 1)
		if err != nil {
			t.Fatalf("store.ReleaseExpiredLeases: %v", err)
		}
		if released != 1 {
			t.Errorf("released = %d, want 1", released)
		}

		var (
			status   string
			attempts int
		)
		if err := pool.QueryRow(
			t.Context(),
			`SELECT status, attempts FROM webhook.deliveries WHERE id = $1`,
			deliveryID,
		).Scan(&status, &attempts); err != nil {
			t.Fatalf("select delivery: %v", err)
		}
		if status != "dead" {
			t.Errorf("status = %q, want %q", status, "dead")
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1", attempts)
		}
	})
}

func TestDeliveryStoreResetForRetry(t *testing.T) {
	t.Run("failed delivery returns to pending", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		job, err := deliveryStore.ClaimOneDue(t.Context())
		if err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if job == nil {
			t.Fatal("store.ClaimOneDue returned nil, want job")
		}

		code := 400
		if err := deliveryStore.RecordAttempt(t.Context(), domain.AttemptResult{
			DeliveryID:     deliveryID,
			Status:         domain.DeliveryFailed,
			LastStatusCode: &code,
			LastError:      "bad request",
		}); err != nil {
			t.Fatalf("store.RecordAttempt: %v", err)
		}

		delivery, err := deliveryStore.ResetForRetry(t.Context(), deliveryID)
		if err != nil {
			t.Fatalf("store.ResetForRetry: %v", err)
		}
		if delivery.Status != domain.DeliveryPending {
			t.Errorf("status = %q, want %q", delivery.Status, domain.DeliveryPending)
		}
		if delivery.Attempts != 0 {
			t.Errorf("attempts = %d, want 0", delivery.Attempts)
		}
		if delivery.LastStatusCode != 0 {
			t.Errorf("last_status_code = %d, want 0", delivery.LastStatusCode)
		}
		if delivery.LastError != "" {
			t.Errorf("last_error = %q, want empty", delivery.LastError)
		}
		if delivery.NextAttemptAt.IsZero() {
			t.Error("next_attempt_at is zero, want non-zero")
		}
	})

	t.Run("dead delivery returns to pending", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		job, err := deliveryStore.ClaimOneDue(t.Context())
		if err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}
		if job == nil {
			t.Fatal("store.ClaimOneDue returned nil, want job")
		}

		if err := deliveryStore.RecordAttempt(t.Context(), domain.AttemptResult{
			DeliveryID: deliveryID,
			Status:     domain.DeliveryDead,
		}); err != nil {
			t.Fatalf("store.RecordAttempt: %v", err)
		}

		delivery, err := deliveryStore.ResetForRetry(t.Context(), deliveryID)
		if err != nil {
			t.Fatalf("store.ResetForRetry: %v", err)
		}
		if delivery.Status != domain.DeliveryPending {
			t.Errorf("status = %q, want %q", delivery.Status, domain.DeliveryPending)
		}
	})

	t.Run("not retryable", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryID := seedPendingDelivery(t, pool)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		if _, err := deliveryStore.ClaimOneDue(t.Context()); err != nil {
			t.Fatalf("store.ClaimOneDue: %v", err)
		}

		_, err = deliveryStore.ResetForRetry(t.Context(), deliveryID)
		if !errors.Is(err, domain.ErrDeliveryNotRetryable) {
			t.Errorf("store.ResetForRetry error = %v, want %v", err, domain.ErrDeliveryNotRetryable)
		}
	})

	t.Run("not found", func(t *testing.T) {
		pool := testdb.New(t)
		deliveryStore, err := store.NewDeliveryStore(pool, time.Minute)
		if err != nil {
			t.Fatalf("new delivery store: %v", err)
		}

		_, err = deliveryStore.ResetForRetry(t.Context(), 999999)
		if !errors.Is(err, domain.ErrDeliveryNotFound) {
			t.Errorf("store.ResetForRetry error = %v, want %v", err, domain.ErrDeliveryNotFound)
		}
	})
}
