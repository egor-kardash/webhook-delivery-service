package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

type EventStore interface {
	PublishEventWithDeliveries(ctx context.Context, event *domain.Event) (*domain.PublishResult, error)
}

type EventService struct {
	store EventStore
}

func NewEventService(store EventStore) *EventService {
	return &EventService{store: store}
}

type CreateEventInput struct {
	EventType      string
	Payload        json.RawMessage
	IdempotencyKey string
}

func (s *EventService) Create(ctx context.Context, in CreateEventInput) (*domain.PublishResult, error) {
	event, err := domain.NewEvent(in.EventType, in.IdempotencyKey, in.Payload)
	if err != nil {
		return nil, err
	}

	result, err := s.store.PublishEventWithDeliveries(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("create event: %w", err)
	}

	return result, nil
}
