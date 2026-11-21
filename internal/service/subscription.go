package service

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

type SubscriptionStore interface {
	Create(ctx context.Context, sub *domain.Subscription) (*domain.Subscription, error)
}

type SubscriptionService struct {
	store SubscriptionStore
}

func NewSubscriptionService(store SubscriptionStore) *SubscriptionService {
	return &SubscriptionService{store: store}
}

type CreateSubscriptionInput struct {
	TargetURL  string
	EventTypes []string
}

func (s *SubscriptionService) Create(
	ctx context.Context,
	in CreateSubscriptionInput,
) (*domain.Subscription, error) {
	sub, err := domain.NewSubscription(in.TargetURL, in.EventTypes, rand.Text())
	if err != nil {
		return nil, err
	}

	created, err := s.store.Create(ctx, sub)
	if err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}

	return created, nil
}
