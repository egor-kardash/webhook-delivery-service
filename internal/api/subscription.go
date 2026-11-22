package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
)

type SubscriptionService interface {
	Create(ctx context.Context, in service.CreateSubscriptionInput) (*domain.Subscription, error)
}

type SubscriptionHandler struct {
	svc SubscriptionService
}

func NewSubscriptionHandler(svc SubscriptionService) *SubscriptionHandler {
	return &SubscriptionHandler{svc: svc}
}

func (h *SubscriptionHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /subscriptions", h.Create)
}

type createSubscriptionRequest struct {
	TargetURL  string   `json:"target_url"`
	EventTypes []string `json:"event_types"`
}

type createSubscriptionResponse struct {
	ID         int64     `json:"id"`
	TargetURL  string    `json:"target_url"`
	SecretKey  string    `json:"secret_key"`
	EventTypes []string  `json:"event_types"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
}

func (h *SubscriptionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createSubscriptionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(r.Context(), w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	sub, err := h.svc.Create(ctx, service.CreateSubscriptionInput{
		TargetURL:  req.TargetURL,
		EventTypes: req.EventTypes,
	})
	if err != nil {
		writeServiceError(r.Context(), w, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("%s/%d", r.URL.Path, sub.ID))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	WriteJSON(r.Context(), w, http.StatusCreated, createSubscriptionResponse{
		ID:         sub.ID,
		TargetURL:  sub.TargetURL,
		SecretKey:  sub.SecretKey,
		EventTypes: sub.EventTypes,
		Active:     sub.Active,
		CreatedAt:  sub.CreatedAt,
	})
}
