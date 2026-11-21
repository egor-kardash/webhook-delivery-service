package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

type EventStore struct {
	pool *pgxpool.Pool
}

func NewEventStore(pool *pgxpool.Pool) *EventStore {
	return &EventStore{pool: pool}
}

func (s *EventStore) PublishEventWithDeliveries(
	ctx context.Context,
	event *domain.Event,
) (*domain.PublishResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	eventID, created, err := insertEvent(ctx, tx, event)
	if err != nil {
		return nil, err
	}

	var deliveries []int64
	if created {
		deliveries, err = insertDeliveries(ctx, tx, eventID, event.EventType)
	} else {
		deliveries, err = selectDeliveries(ctx, tx, eventID)
	}
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &domain.PublishResult{
		EventID:     eventID,
		DeliveryIDs: deliveries,
		Created:     created,
	}, nil
}

func insertEvent(ctx context.Context, tx pgx.Tx, event *domain.Event) (int64, bool, error) {
	const (
		insertEventQuery = `
			INSERT INTO webhook.events (event_type, payload, idempotency_key)
			VALUES ($1, $2, $3)
			ON CONFLICT (idempotency_key) DO NOTHING
			RETURNING id`

		selectEventIDQuery = `
			SELECT id
			FROM webhook.events
			WHERE idempotency_key = $1`
	)

	const maxAttempts = 3

	for range maxAttempts {
		var eventID int64

		err := tx.QueryRow(
			ctx,
			insertEventQuery,
			event.EventType,
			event.Payload,
			event.IdempotencyKey,
		).Scan(&eventID)
		switch {
		case err == nil:
			return eventID, true, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return 0, false, fmt.Errorf("insert event: %w", err)
		}

		err = tx.QueryRow(ctx, selectEventIDQuery, event.IdempotencyKey).Scan(&eventID)
		switch {
		case err == nil:
			return eventID, false, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return 0, false, fmt.Errorf("find existing event: %w", err)
		}
	}

	return 0, false, errors.New("insert event: idempotency conflict did not settle")
}

func insertDeliveries(ctx context.Context, tx pgx.Tx, eventID int64, eventType string) ([]int64, error) {
	const query = `
		INSERT INTO webhook.deliveries (event_id, subscription_id)
		SELECT $1, s.id
		FROM webhook.subscriptions s
		WHERE s.active
			AND s.event_types @> $2
		RETURNING id`

	rows, err := tx.Query(ctx, query, eventID, []string{eventType})
	if err != nil {
		return nil, fmt.Errorf("insert deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan deliveries: %w", err)
		}
		deliveries = append(deliveries, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}

	slices.Sort(deliveries)

	return deliveries, nil
}

func selectDeliveries(ctx context.Context, tx pgx.Tx, eventID int64) ([]int64, error) {
	const query = `
		SELECT id
		FROM webhook.deliveries
		WHERE event_id = $1
		ORDER BY id`

	rows, err := tx.Query(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("select existing deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan existing deliveries: %w", err)
		}
		deliveries = append(deliveries, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate existing deliveries: %w", err)
	}

	return deliveries, nil
}
