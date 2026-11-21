package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

type SubscriptionStore struct {
	pool *pgxpool.Pool
}

func NewSubscriptionStore(pool *pgxpool.Pool) *SubscriptionStore {
	return &SubscriptionStore{pool: pool}
}

func (s *SubscriptionStore) Create(
	ctx context.Context,
	sub *domain.Subscription,
) (*domain.Subscription, error) {
	const query = `
		INSERT INTO webhook.subscriptions (target_url, secret_key, event_types, active)
		VALUES ($1, $2, $3, $4)
		RETURNING id, target_url, secret_key, event_types, active, created_at`

	var created domain.Subscription
	err := s.pool.QueryRow(ctx, query, sub.TargetURL, sub.SecretKey, sub.EventTypes, sub.Active).
		Scan(
			&created.ID,
			&created.TargetURL,
			&created.SecretKey,
			&created.EventTypes,
			&created.Active,
			&created.CreatedAt,
		)
	if err != nil {
		return nil, fmt.Errorf("insert subscription: %w", err)
	}

	return &created, nil
}
