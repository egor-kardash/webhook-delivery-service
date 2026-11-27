package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

type DeliveryStore struct {
	pool  *pgxpool.Pool
	lease time.Duration
}

func NewDeliveryStore(pool *pgxpool.Pool, lease time.Duration) (*DeliveryStore, error) {
	if lease < time.Millisecond {
		return nil, fmt.Errorf("lease = %s, must be at least 1ms", lease)
	}

	return &DeliveryStore{
		pool:  pool,
		lease: lease,
	}, nil
}

func (s *DeliveryStore) GetByID(ctx context.Context, id int64) (*domain.Delivery, error) {
	if id <= 0 {
		return nil, errors.New("delivery id must be positive")
	}

	const query = `
		SELECT id, event_id, subscription_id, status, attempts,
			   next_attempt_at, COALESCE(last_status_code, 0),
			   COALESCE(last_error, ''), created_at, updated_at
		FROM webhook.deliveries
		WHERE id = $1`

	var delivery domain.Delivery
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&delivery.ID,
		&delivery.EventID,
		&delivery.SubscriptionID,
		&delivery.Status,
		&delivery.Attempts,
		&delivery.NextAttemptAt,
		&delivery.LastStatusCode,
		&delivery.LastError,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("select delivery: %w", domain.ErrDeliveryNotFound)
		}
		return nil, fmt.Errorf("select delivery: %w", err)
	}

	return &delivery, nil
}

func (s *DeliveryStore) ClaimOneDue(ctx context.Context) (*domain.DeliveryJob, error) {
	const query = `
		WITH due AS (
			SELECT id
			FROM webhook.deliveries
			WHERE status = 'pending'
				AND next_attempt_at <= now()
			ORDER BY next_attempt_at, id
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		),
		claimed AS (
			UPDATE webhook.deliveries d
			SET status = 'in_progress',
				locked_until = now() + ($1 * interval '1 millisecond'),
				updated_at = now()
			FROM due
			WHERE d.id = due.id
			RETURNING d.id, d.event_id, d.subscription_id, d.attempts
		)
		SELECT c.id, c.event_id, c.subscription_id, c.attempts,
			   e.event_type, e.payload,
			   s.target_url
		FROM claimed c
		JOIN webhook.events e ON e.id = c.event_id
		JOIN webhook.subscriptions s ON s.id = c.subscription_id`

	var job domain.DeliveryJob
	err := s.pool.QueryRow(ctx, query, s.lease.Milliseconds()).Scan(
		&job.ID,
		&job.EventID,
		&job.SubscriptionID,
		&job.Attempts,
		&job.EventType,
		&job.Payload,
		&job.TargetURL,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("claim one due delivery: %w", err)
	}

	return &job, nil
}

func (s *DeliveryStore) ReleaseExpiredLeases(ctx context.Context) (int, error) {
	const query = `
		UPDATE webhook.deliveries
		SET status = 'pending',
			locked_until = NULL,
			next_attempt_at = now(),
			updated_at = now()
		WHERE status = 'in_progress'
			AND locked_until <= now()`

	tag, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("release expired delivery leases: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

func (s *DeliveryStore) RecordAttempt(ctx context.Context, result domain.AttemptResult) error {
	if result.DeliveryID <= 0 {
		return errors.New("delivery id must be positive")
	}
	if !result.Status.IsAttemptOutcome() {
		return fmt.Errorf("invalid attempt status %q", result.Status)
	}

	const query = `
		UPDATE webhook.deliveries
		SET status = $2,
			attempts = attempts + 1,
			locked_until = NULL,
			next_attempt_at = now(),
			last_status_code = $3,
			last_error = NULLIF($4, ''),
			updated_at = now()
		WHERE id = $1
			AND status = 'in_progress'
			AND locked_until > now()`

	var lastStatusCode any
	if result.LastStatusCode != nil {
		lastStatusCode = *result.LastStatusCode
	}

	tag, err := s.pool.Exec(
		ctx,
		query,
		result.DeliveryID,
		string(result.Status),
		lastStatusCode,
		result.LastError,
	)
	if err != nil {
		return fmt.Errorf("record delivery attempt: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("record delivery attempt: %w", domain.ErrLeaseLost)
	}

	return nil
}
