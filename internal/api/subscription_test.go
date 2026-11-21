package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/internal/api"
	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
)

type stubSubscriptionService struct {
	calls    int
	gotInput service.CreateSubscriptionInput
	result   *domain.Subscription
	err      error
}

func (s *stubSubscriptionService) Create(
	_ context.Context,
	in service.CreateSubscriptionInput,
) (*domain.Subscription, error) {
	s.calls++
	s.gotInput = in

	return s.result, s.err
}

func TestSubscriptionCreate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Date(2000, 11, 17, 7, 30, 0, 0, time.UTC)
		wantSub := &domain.Subscription{
			ID:         123,
			TargetURL:  "https://example.com",
			SecretKey:  "SRJHT7YRQUTAAHEZJQMSKGHCY4",
			EventTypes: []string{"user.created", "order.paid"},
			Active:     true,
			CreatedAt:  now,
		}

		svc := &stubSubscriptionService{result: wantSub}
		handler := api.NewSubscriptionHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"target_url":"https://example.com","event_types":["user.created","order.paid"]}`
		req := newJSONPost(t, "/subscriptions", reqBody)
		rr := serve(t, router, req)

		requireStatus(t, rr, http.StatusCreated)

		if got := rr.Header().Get("Location"); got != "/subscriptions/123" {
			t.Errorf("Location = %q, want %q", got, "/subscriptions/123")
		}
		if got := rr.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want %q", got, "no-store")
		}
		if got := rr.Header().Get("Pragma"); got != "no-cache" {
			t.Errorf("Pragma = %q, want %q", got, "no-cache")
		}

		type subscriptionResponse struct {
			ID         int64     `json:"id"`
			TargetURL  string    `json:"target_url"`
			SecretKey  string    `json:"secret_key"`
			EventTypes []string  `json:"event_types"`
			Active     bool      `json:"active"`
			CreatedAt  time.Time `json:"created_at"`
		}

		resp := decodeJSONBody[subscriptionResponse](t, rr)
		if resp.ID != wantSub.ID {
			t.Errorf("resp.ID = %d, want %d", resp.ID, wantSub.ID)
		}
		if resp.TargetURL != wantSub.TargetURL {
			t.Errorf("resp.TargetURL = %q, want %q", resp.TargetURL, wantSub.TargetURL)
		}
		if resp.SecretKey != wantSub.SecretKey {
			t.Errorf("resp.SecretKey does not match the store result")
		}
		if !slices.Equal(resp.EventTypes, wantSub.EventTypes) {
			t.Errorf("resp.EventTypes = %v, want %v", resp.EventTypes, wantSub.EventTypes)
		}
		if !resp.Active {
			t.Error("resp.Active = false, want true")
		}
		if !resp.CreatedAt.Equal(wantSub.CreatedAt) {
			t.Errorf("resp.CreatedAt = %v, want %v", resp.CreatedAt, wantSub.CreatedAt)
		}

		if svc.calls != 1 {
			t.Fatalf("svc.Create calls = %d, want 1", svc.calls)
		}

		if svc.gotInput.TargetURL != wantSub.TargetURL {
			t.Errorf("service received TargetURL = %q, want %q", svc.gotInput.TargetURL, wantSub.TargetURL)
		}
		if !slices.Equal(svc.gotInput.EventTypes, wantSub.EventTypes) {
			t.Errorf("service received EventTypes = %v, want %v", svc.gotInput.EventTypes, wantSub.EventTypes)
		}
	})

	t.Run("validation error invalid target_url", func(t *testing.T) {
		svc := &stubSubscriptionService{
			err: &domain.ValidationError{
				Fields: map[string]string{"target_url": "must be a valid http or https URL"},
			},
		}
		handler := api.NewSubscriptionHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"target_url":"ftp://example.com","event_types":["user.created"]}`
		req := newJSONPost(t, "/subscriptions", reqBody)
		rr := serve(t, router, req)

		resp := requireError(t, rr, http.StatusBadRequest, "validation_failed")

		if _, ok := resp.Error.Fields["target_url"]; !ok {
			t.Errorf("expected fields.target_url in response, got %v", resp.Error.Fields)
		}

		if svc.calls != 1 {
			t.Errorf("svc.Create calls = %d, want 1", svc.calls)
		}
	})

	t.Run("validation error empty event types", func(t *testing.T) {
		svc := &stubSubscriptionService{
			err: &domain.ValidationError{
				Fields: map[string]string{"event_types": "must contain at least one event type"},
			},
		}
		handler := api.NewSubscriptionHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"target_url":"https://example.com","event_types":[]}`
		req := newJSONPost(t, "/subscriptions", reqBody)
		rr := serve(t, router, req)

		resp := requireError(t, rr, http.StatusBadRequest, "validation_failed")

		if _, ok := resp.Error.Fields["event_types"]; !ok {
			t.Errorf("expected fields.event_types in response, got %v", resp.Error.Fields)
		}

		if svc.calls != 1 {
			t.Errorf("svc.Create calls = %d, want 1", svc.calls)
		}
	})

	t.Run("payload too large", func(t *testing.T) {
		svc := &stubSubscriptionService{}
		handler := api.NewSubscriptionHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		bigURL := "https://example.com" + strings.Repeat("a", 1<<20)
		reqBody := `{"target_url":"` + bigURL + `","event_types":["user.created"]}`
		req := newJSONPost(t, "/subscriptions", reqBody)
		rr := serve(t, router, req)

		requireError(t, rr, http.StatusRequestEntityTooLarge, "request_too_large")

		if svc.calls != 0 {
			t.Errorf("svc.Create calls = %d, want 0", svc.calls)
		}
	})

	t.Run("unsupported media type", func(t *testing.T) {
		svc := &stubSubscriptionService{}
		handler := api.NewSubscriptionHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := "plain text"
		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/subscriptions",
			strings.NewReader(reqBody),
		)
		req.Header.Set("Content-Type", "text/plain")
		rr := serve(t, router, req)

		requireError(t, rr, http.StatusUnsupportedMediaType, "unsupported_media_type")

		if svc.calls != 0 {
			t.Errorf("svc.Create calls = %d, want 0", svc.calls)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		cases := []struct {
			name    string
			reqBody string
		}{
			{name: "syntax error", reqBody: `{"target_url":}`},
			{
				name:    "unknown field",
				reqBody: `{"target_url":"https://example.com","event_types":["user.created"],"unknown":1}`,
			},
			{
				name:    "multiple values",
				reqBody: `{"target_url":"https://example.com","event_types":["user.created"]} {"target_url":"https://example.com","event_types":["user.created"]}`,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &stubSubscriptionService{}
				handler := api.NewSubscriptionHandler(svc)
				router := newTestRouter(&stubPinger{}, handler)

				req := newJSONPost(t, "/subscriptions", tc.reqBody)
				rr := serve(t, router, req)

				requireError(t, rr, http.StatusBadRequest, "invalid_json")

				if svc.calls != 0 {
					t.Errorf("svc.Create calls = %d, want 0", svc.calls)
				}
			})
		}
	})

	t.Run("service internal error", func(t *testing.T) {
		svc := &stubSubscriptionService{
			err: errors.New("unexpected database failure"),
		}
		handler := api.NewSubscriptionHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"target_url":"https://example.com","event_types":["user.created"]}`
		req := newJSONPost(t, "/subscriptions", reqBody)
		rr := serve(t, router, req)

		resp := requireError(t, rr, http.StatusInternalServerError, "internal_error")
		if strings.Contains(resp.Error.Message, "unexpected database failure") {
			t.Errorf("response leaks internal error: %q", resp.Error.Message)
		}

		if svc.calls != 1 {
			t.Errorf("svc.Create calls = %d, want 1", svc.calls)
		}
	})

	t.Run("service deadline", func(t *testing.T) {
		cases := []struct {
			name string
			err  error
		}{
			{name: "canceled", err: fmt.Errorf("create subscription: %w", context.Canceled)},
			{name: "deadline", err: fmt.Errorf("create subscription: %w", context.DeadlineExceeded)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &stubSubscriptionService{
					err: tc.err,
				}
				handler := api.NewSubscriptionHandler(svc)
				router := newTestRouter(&stubPinger{}, handler)

				reqBody := `{"target_url":"https://example.com","event_types":["user.created"]}`
				req := newJSONPost(t, "/subscriptions", reqBody)
				rr := serve(t, router, req)

				requireError(t, rr, http.StatusServiceUnavailable, "service_unavailable")

				if svc.calls != 1 {
					t.Errorf("svc.Create calls = %d, want 1", svc.calls)
				}
			})
		}
	})
}
