package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
)

const (
	testEventType      = "user.created"
	testIdempotencyKey = "idem-key-1"
)

type stubEventStore struct {
	calls    int
	gotInput *domain.Event
	result   *domain.PublishResult
	err      error
}

func (s *stubEventStore) PublishEventWithDeliveries(
	_ context.Context,
	event *domain.Event,
) (*domain.PublishResult, error) {
	s.calls++
	s.gotInput = event

	return s.result, s.err
}

func TestEventServiceCreate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		want := &domain.PublishResult{
			EventID:     42,
			DeliveryIDs: []int64{100, 101},
			Created:     true,
		}
		store := &stubEventStore{result: want}
		svc := service.NewEventService(store)

		in := service.CreateEventInput{
			EventType:      "  " + testEventType + " \n",
			IdempotencyKey: "  " + testIdempotencyKey + "  ",
			Payload:        json.RawMessage(` {"id":1} `),
		}

		result, err := svc.Create(t.Context(), in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if store.calls != 1 {
			t.Fatalf("store calls = %d, want 1", store.calls)
		}

		if store.gotInput.EventType != testEventType {
			t.Errorf("store received EventType = %q, want %q", store.gotInput.EventType, testEventType)
		}
		if store.gotInput.IdempotencyKey != testIdempotencyKey {
			t.Errorf("store received IdempotencyKey = %q, want %q", store.gotInput.IdempotencyKey, testIdempotencyKey)
		}
		if got := string(store.gotInput.Payload); got != `{"id":1}` {
			t.Errorf("store received Payload = %s, want %s", got, `{"id":1}`)
		}

		if result != want {
			t.Error("service must return the result from the store")
		}
	})

	t.Run("validation error", func(t *testing.T) {
		store := &stubEventStore{}
		svc := service.NewEventService(store)

		in := service.CreateEventInput{
			EventType:      "",
			IdempotencyKey: testIdempotencyKey,
			Payload:        json.RawMessage(`{"id":1}`),
		}

		_, err := svc.Create(t.Context(), in)
		if err == nil {
			t.Fatal("expected validation error, got nil")
		}

		var valErr *domain.ValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("expected *domain.ValidationError, got %T: %v", err, err)
		}

		if store.calls != 0 {
			t.Errorf("store calls = %d, want 0 on validation error", store.calls)
		}
	})

	t.Run("store error", func(t *testing.T) {
		storeErr := errors.New("store failure")
		store := &stubEventStore{err: storeErr}
		svc := service.NewEventService(store)

		in := service.CreateEventInput{
			EventType:      testEventType,
			IdempotencyKey: testIdempotencyKey,
			Payload:        json.RawMessage(`{"id":1}`),
		}

		_, err := svc.Create(t.Context(), in)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if store.calls != 1 {
			t.Errorf("store calls = %d, want 1", store.calls)
		}

		if !errors.Is(err, storeErr) {
			t.Errorf("expected error to wrap %v, got %v", storeErr, err)
		}
	})
}
