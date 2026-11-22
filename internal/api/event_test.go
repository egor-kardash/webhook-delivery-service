package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egor-kardash/webhook-delivery-service/internal/api"
	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
)

const (
	testEventType      = "user.created"
	testIdempotencyKey = "idem-key-1"
)

type stubEventService struct {
	calls    int
	gotInput service.CreateEventInput
	result   *domain.PublishResult
	err      error
}

func (s *stubEventService) Create(_ context.Context, in service.CreateEventInput) (*domain.PublishResult, error) {
	s.calls++
	s.gotInput = in

	return s.result, s.err
}

func TestEventCreate(t *testing.T) {
	t.Run("new event returns 202", func(t *testing.T) {
		want := &domain.PublishResult{
			EventID:     42,
			DeliveryIDs: []int64{100, 101},
			Created:     true,
		}
		svc := &stubEventService{result: want}
		handler := api.NewEventHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"event_type":"user.created","payload":{"id":1}}`
		req := newJSONPost(t, "/events", reqBody)
		req.Header.Set("Idempotency-Key", "  "+testIdempotencyKey+"  ")
		rr := serve(t, router, req)

		requireStatus(t, rr, http.StatusAccepted)

		type eventResponse struct {
			EventID    int64 `json:"event_id"`
			Deliveries int   `json:"deliveries"`
		}

		resp := decodeJSONBody[eventResponse](t, rr)
		if resp.EventID != want.EventID {
			t.Errorf("resp.EventID = %d, want %d", resp.EventID, want.EventID)
		}
		if resp.Deliveries != len(want.DeliveryIDs) {
			t.Errorf("resp.Deliveries = %d, want %d", resp.Deliveries, len(want.DeliveryIDs))
		}

		if svc.calls != 1 {
			t.Fatalf("svc.Create calls = %d, want 1", svc.calls)
		}
		if svc.gotInput.EventType != testEventType {
			t.Errorf("service received EventType = %q, want %q", svc.gotInput.EventType, testEventType)
		}
		if svc.gotInput.IdempotencyKey != testIdempotencyKey {
			t.Errorf("service received IdempotencyKey = %q, want %q", svc.gotInput.IdempotencyKey, testIdempotencyKey)
		}
		if got := string(svc.gotInput.Payload); got != `{"id":1}` {
			t.Errorf("service received Payload = %s, want %s", got, `{"id":1}`)
		}
	})

	t.Run("idempotent replay returns 200", func(t *testing.T) {
		want := &domain.PublishResult{
			EventID:     42,
			DeliveryIDs: []int64{100, 101},
			Created:     false,
		}
		svc := &stubEventService{result: want}
		handler := api.NewEventHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"event_type":"user.created","payload":{"id":1}}`
		req := newJSONPost(t, "/events", reqBody)
		req.Header.Set("Idempotency-Key", testIdempotencyKey)
		rr := serve(t, router, req)

		requireStatus(t, rr, http.StatusOK)

		type eventResponse struct {
			EventID    int64 `json:"event_id"`
			Deliveries int   `json:"deliveries"`
		}

		resp := decodeJSONBody[eventResponse](t, rr)
		if resp.EventID != want.EventID {
			t.Errorf("resp.EventID = %d, want %d", resp.EventID, want.EventID)
		}
		if resp.Deliveries != len(want.DeliveryIDs) {
			t.Errorf("resp.Deliveries = %d, want %d", resp.Deliveries, len(want.DeliveryIDs))
		}

		if svc.calls != 1 {
			t.Fatalf("svc.Create calls = %d, want 1", svc.calls)
		}
	})

	t.Run("no matching subscriptions returns deliveries 0", func(t *testing.T) {
		want := &domain.PublishResult{
			EventID: 43,
			Created: true,
		}
		svc := &stubEventService{result: want}
		handler := api.NewEventHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"event_type":"user.created","payload":{}}`
		req := newJSONPost(t, "/events", reqBody)
		req.Header.Set("Idempotency-Key", testIdempotencyKey)
		rr := serve(t, router, req)

		requireStatus(t, rr, http.StatusAccepted)

		type eventResponse struct {
			EventID    int64 `json:"event_id"`
			Deliveries int   `json:"deliveries"`
		}
		resp := decodeJSONBody[eventResponse](t, rr)
		if resp.EventID != want.EventID {
			t.Errorf("resp.EventID = %d, want %d", resp.EventID, want.EventID)
		}
		if resp.Deliveries != 0 {
			t.Errorf("resp.Deliveries = %d, want 0", resp.Deliveries)
		}

		if svc.calls != 1 {
			t.Fatalf("svc.Create calls = %d, want 1", svc.calls)
		}
	})

	t.Run("missing Idempotency-Key", func(t *testing.T) {
		svc := &stubEventService{}
		handler := api.NewEventHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"event_type":"user.created","payload":{"id":1}}`
		req := newJSONPost(t, "/events", reqBody)
		rr := serve(t, router, req)

		resp := requireError(t, rr, http.StatusBadRequest, "validation_failed")
		if _, ok := resp.Error.Fields["idempotency_key"]; !ok {
			t.Errorf("expected fields.idempotency_key in response, got %v", resp.Error.Fields)
		}

		if svc.calls != 0 {
			t.Errorf("svc.Create calls = %d, want 0", svc.calls)
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		cases := []struct {
			name    string
			payload string
		}{
			{name: "array", payload: `[1,2]`},
			{name: "string", payload: `"text"`},
			{name: "number", payload: `42`},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &stubEventService{
					err: &domain.ValidationError{
						Fields: map[string]string{"payload": "must be a valid JSON object"},
					},
				}
				handler := api.NewEventHandler(svc)
				router := newTestRouter(&stubPinger{}, handler)

				reqBody := `{"event_type":"user.created","payload":` + tc.payload + `}`
				req := newJSONPost(t, "/events", reqBody)
				req.Header.Set("Idempotency-Key", testIdempotencyKey)
				rr := serve(t, router, req)

				resp := requireError(t, rr, http.StatusBadRequest, "validation_failed")
				if _, ok := resp.Error.Fields["payload"]; !ok {
					t.Errorf("expected fields.payload in response, got %v", resp.Error.Fields)
				}

				if svc.calls != 1 {
					t.Errorf("svc.Create calls = %d, want 1", svc.calls)
				}
			})
		}
	})

	t.Run("payload too large", func(t *testing.T) {
		svc := &stubEventService{}
		handler := api.NewEventHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		bigPayload := `{"data":"` + strings.Repeat("a", 1<<20) + `"}`
		reqBody := `{"event_type":"user.created","payload":` + bigPayload + `}`
		req := newJSONPost(t, "/events", reqBody)
		req.Header.Set("Idempotency-Key", testIdempotencyKey)
		rr := serve(t, router, req)

		requireError(t, rr, http.StatusRequestEntityTooLarge, "request_too_large")

		if svc.calls != 0 {
			t.Errorf("svc.Create calls = %d, want 0", svc.calls)
		}
	})

	t.Run("unsupported media type", func(t *testing.T) {
		svc := &stubEventService{}
		handler := api.NewEventHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/events",
			strings.NewReader("plain text"),
		)
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("Idempotency-Key", testIdempotencyKey)
		rr := serve(t, router, req)

		requireError(t, rr, http.StatusUnsupportedMediaType, "unsupported_media_type")

		if svc.calls != 0 {
			t.Errorf("svc.Create calls = %d, want 0", svc.calls)
		}
	})

	t.Run("service internal error", func(t *testing.T) {
		svc := &stubEventService{
			err: errors.New("unexpected database failure"),
		}
		handler := api.NewEventHandler(svc)
		router := newTestRouter(&stubPinger{}, handler)

		reqBody := `{"event_type":"user.created","payload":{"id":1}}`
		req := newJSONPost(t, "/events", reqBody)
		req.Header.Set("Idempotency-Key", testIdempotencyKey)
		rr := serve(t, router, req)

		resp := requireError(t, rr, http.StatusInternalServerError, "internal_error")
		if strings.Contains(resp.Error.Message, "unexpected database failure") {
			t.Errorf("response leaks internal error: %q", resp.Error.Message)
		}

		if svc.calls != 1 {
			t.Errorf("svc.Create calls = %d, want 1", svc.calls)
		}
	})

	t.Run("service unavailable", func(t *testing.T) {
		cases := []struct {
			name string
			err  error
		}{
			{name: "canceled", err: fmt.Errorf("create event: %w", context.Canceled)},
			{name: "deadline", err: fmt.Errorf("create event: %w", context.DeadlineExceeded)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				svc := &stubEventService{err: tc.err}
				handler := api.NewEventHandler(svc)
				router := newTestRouter(&stubPinger{}, handler)

				reqBody := `{"event_type":"user.created","payload":{"id":1}}`
				req := newJSONPost(t, "/events", reqBody)
				req.Header.Set("Idempotency-Key", testIdempotencyKey)
				rr := serve(t, router, req)

				requireError(t, rr, http.StatusServiceUnavailable, "service_unavailable")

				if svc.calls != 1 {
					t.Errorf("svc.Create calls = %d, want 1", svc.calls)
				}
			})
		}
	})
}
