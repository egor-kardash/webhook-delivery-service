package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
)

type stubSubscriptionStore struct {
	calls    int
	gotInput *domain.Subscription
	result   *domain.Subscription
	err      error
}

func (f *stubSubscriptionStore) Create(_ context.Context, sub *domain.Subscription) (*domain.Subscription, error) {
	f.calls++
	f.gotInput = sub

	return f.result, f.err
}

func TestSubscriptionServiceCreate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		want := &domain.Subscription{ID: 123}
		store := &stubSubscriptionStore{result: want}
		svc := service.NewSubscriptionService(store)

		in := service.CreateSubscriptionInput{
			TargetURL:  " https://example.com ",
			EventTypes: []string{" user.created ", "user.created", "order.paid"},
		}

		created, err := svc.Create(t.Context(), in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if store.calls != 1 {
			t.Fatalf("store.Create calls = %d, want 1", store.calls)
		}

		if store.gotInput.TargetURL != "https://example.com" {
			t.Errorf("store received TargetURL = %q, want %q", store.gotInput.TargetURL, "https://example.com")
		}

		wantEvents := []string{"user.created", "order.paid"}
		if !slices.Equal(store.gotInput.EventTypes, wantEvents) {
			t.Errorf("store received EventTypes = %v, want %v", store.gotInput.EventTypes, wantEvents)
		}

		if store.gotInput.SecretKey == "" {
			t.Error("expected generated SecretKey to be non-empty")
		}
		if len(store.gotInput.SecretKey) < 26 {
			t.Errorf("SecretKey length = %d, want >= 26", len(store.gotInput.SecretKey))
		}

		if created != want {
			t.Error("service must return the subscription created by the store")
		}
	})

	t.Run("validation error", func(t *testing.T) {
		store := &stubSubscriptionStore{}
		svc := service.NewSubscriptionService(store)

		in := service.CreateSubscriptionInput{
			TargetURL:  "ftp://example.com",
			EventTypes: []string{"user.created"},
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
			t.Errorf("store.Create calls = %d, want 0 on validation error", store.calls)
		}
	})

	t.Run("store error", func(t *testing.T) {
		storeErr := errors.New("store failure")
		store := &stubSubscriptionStore{err: storeErr}
		svc := service.NewSubscriptionService(store)

		in := service.CreateSubscriptionInput{
			TargetURL:  "https://example.com",
			EventTypes: []string{"user.created"},
		}

		_, err := svc.Create(t.Context(), in)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if store.calls != 1 {
			t.Errorf("store.Create calls = %d, want 1", store.calls)
		}

		if !errors.Is(err, storeErr) {
			t.Errorf("expected error to wrap %v, got %v", storeErr, err)
		}
	})
}
