package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
)

type EventService interface {
	Create(ctx context.Context, in service.CreateEventInput) (*domain.PublishResult, error)
}

type EventHandler struct {
	svc EventService
}

func NewEventHandler(svc EventService) *EventHandler {
	return &EventHandler{svc: svc}
}

func (h *EventHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /events", h.Create)
}

type createEventRequest struct {
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type createEventResponse struct {
	EventID    int64 `json:"event_id"`
	Deliveries int   `json:"deliveries"`
}

func (h *EventHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createEventRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(r.Context(), w, err)
		return
	}

	idempotencyKey, ok := requireHeader(w, r, "Idempotency-Key", "idempotency_key")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	result, err := h.svc.Create(ctx, service.CreateEventInput{
		EventType:      req.EventType,
		IdempotencyKey: idempotencyKey,
		Payload:        req.Payload,
	})
	if err != nil {
		writeServiceError(r.Context(), w, err)
		return
	}

	status := http.StatusOK
	if result.Created {
		status = http.StatusAccepted
	}

	WriteJSON(r.Context(), w, status, createEventResponse{
		EventID:    result.EventID,
		Deliveries: len(result.DeliveryIDs),
	})
}
