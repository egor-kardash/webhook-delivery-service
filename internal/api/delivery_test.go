package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/internal/api"
	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

type stubDeliveryService struct {
	calls  int
	gotID  int64
	result *domain.Delivery
	err    error
}

func (s *stubDeliveryService) GetDelivery(_ context.Context, id int64) (*domain.Delivery, error) {
	s.calls++
	s.gotID = id

	return s.result, s.err
}

func TestDeliveryGet(t *testing.T) {
	t.Run("returns delivery", func(t *testing.T) {
		now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
		want := &domain.Delivery{
			ID:             42,
			EventID:        7,
			SubscriptionID: 3,
			Status:         domain.DeliveryPending,
			Attempts:       2,
			NextAttemptAt:  now.Add(time.Minute),
			LastStatusCode: 500,
			LastError:      "server error",
			CreatedAt:      now,
			UpdatedAt:      now.Add(time.Second),
		}

		svc := &stubDeliveryService{result: want}
		handler := api.NewDeliveryHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/deliveries/42", nil)
		rr := serve(t, router, req)

		requireStatus(t, rr, http.StatusOK)

		type deliveryResponse struct {
			ID             int64                 `json:"id"`
			EventID        int64                 `json:"event_id"`
			SubscriptionID int64                 `json:"subscription_id"`
			Status         domain.DeliveryStatus `json:"status"`
			Attempts       int                   `json:"attempts"`
			NextAttemptAt  time.Time             `json:"next_attempt_at"`
			LastStatusCode int                   `json:"last_status_code"`
			LastError      string                `json:"last_error"`
			CreatedAt      time.Time             `json:"created_at"`
			UpdatedAt      time.Time             `json:"updated_at"`
		}

		resp := decodeJSONBody[deliveryResponse](t, rr)
		if resp.ID != want.ID {
			t.Errorf("resp.ID = %d, want %d", resp.ID, want.ID)
		}
		if resp.EventID != want.EventID {
			t.Errorf("resp.EventID = %d, want %d", resp.EventID, want.EventID)
		}
		if resp.SubscriptionID != want.SubscriptionID {
			t.Errorf("resp.SubscriptionID = %d, want %d", resp.SubscriptionID, want.SubscriptionID)
		}
		if resp.Status != want.Status {
			t.Errorf("resp.Status = %q, want %q", resp.Status, want.Status)
		}
		if resp.Attempts != want.Attempts {
			t.Errorf("resp.Attempts = %d, want %d", resp.Attempts, want.Attempts)
		}
		if !resp.NextAttemptAt.Equal(want.NextAttemptAt) {
			t.Errorf("resp.NextAttemptAt = %v, want %v", resp.NextAttemptAt, want.NextAttemptAt)
		}
		if resp.LastStatusCode != want.LastStatusCode {
			t.Errorf("resp.LastStatusCode = %d, want %d", resp.LastStatusCode, want.LastStatusCode)
		}
		if resp.LastError != want.LastError {
			t.Errorf("resp.LastError = %q, want %q", resp.LastError, want.LastError)
		}
		if !resp.CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("resp.CreatedAt = %v, want %v", resp.CreatedAt, want.CreatedAt)
		}
		if !resp.UpdatedAt.Equal(want.UpdatedAt) {
			t.Errorf("resp.UpdatedAt = %v, want %v", resp.UpdatedAt, want.UpdatedAt)
		}

		if svc.calls != 1 {
			t.Fatalf("svc.GetDelivery calls = %d, want 1", svc.calls)
		}
		if svc.gotID != want.ID {
			t.Errorf("svc.GetDelivery id = %d, want %d", svc.gotID, want.ID)
		}
	})

	t.Run("returns 404 when delivery does not exist", func(t *testing.T) {
		svc := &stubDeliveryService{err: domain.ErrDeliveryNotFound}
		handler := api.NewDeliveryHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/deliveries/42", nil)
		rr := serve(t, router, req)

		requireError(t, rr, http.StatusNotFound, "not_found")

		if svc.calls != 1 {
			t.Errorf("svc.GetDelivery calls = %d, want 1", svc.calls)
		}
		if svc.gotID != 42 {
			t.Errorf("svc.GetDelivery id = %d, want 42", svc.gotID)
		}
	})

	t.Run("invalid id", func(t *testing.T) {
		cases := []struct {
			name string
			path string
		}{
			{name: "not a number", path: "/deliveries/abc"},
			{name: "zero", path: "/deliveries/0"},
			{name: "negative", path: "/deliveries/-1"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &stubDeliveryService{}
				handler := api.NewDeliveryHandler(svc)
				router := newTestRouter(&stubPinger{}, handler)

				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
				rr := serve(t, router, req)

				resp := requireError(t, rr, http.StatusBadRequest, "validation_failed")
				if _, ok := resp.Error.Fields["id"]; !ok {
					t.Errorf("expected fields.id in response, got %v", resp.Error.Fields)
				}

				if svc.calls != 0 {
					t.Errorf("svc.GetDelivery calls = %d, want 0", svc.calls)
				}
			})
		}
	})
}
