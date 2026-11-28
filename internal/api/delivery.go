package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

type DeliveryService interface {
	GetDelivery(ctx context.Context, id int64) (*domain.Delivery, error)
	RetryDelivery(ctx context.Context, id int64) (*domain.Delivery, error)
}

type DeliveryHandler struct {
	svc DeliveryService
}

func NewDeliveryHandler(svc DeliveryService) *DeliveryHandler {
	return &DeliveryHandler{svc: svc}
}

func (h *DeliveryHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /deliveries/{id}", h.GetDelivery)
	mux.HandleFunc("POST /deliveries/{id}/retry", h.RetryDelivery)
}

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

func toDeliveryResponse(delivery domain.Delivery) deliveryResponse {
	return deliveryResponse{
		ID:             delivery.ID,
		EventID:        delivery.EventID,
		SubscriptionID: delivery.SubscriptionID,
		Status:         delivery.Status,
		Attempts:       delivery.Attempts,
		NextAttemptAt:  delivery.NextAttemptAt,
		LastStatusCode: delivery.LastStatusCode,
		LastError:      delivery.LastError,
		CreatedAt:      delivery.CreatedAt,
		UpdatedAt:      delivery.UpdatedAt,
	}
}

func (h *DeliveryHandler) GetDelivery(w http.ResponseWriter, r *http.Request) {
	id, err := parseDeliveryID(r)
	if err != nil {
		writeServiceError(r.Context(), w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	delivery, err := h.svc.GetDelivery(ctx, id)
	if err != nil {
		writeServiceError(r.Context(), w, err)
		return
	}

	WriteJSON(ctx, w, http.StatusOK, toDeliveryResponse(*delivery))
}

func (h *DeliveryHandler) RetryDelivery(w http.ResponseWriter, r *http.Request) {
	id, err := parseDeliveryID(r)
	if err != nil {
		writeServiceError(r.Context(), w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	delivery, err := h.svc.RetryDelivery(ctx, id)
	if err != nil {
		writeServiceError(ctx, w, err)
		return
	}

	WriteJSON(ctx, w, http.StatusAccepted, toDeliveryResponse(*delivery))
}

func parseDeliveryID(r *http.Request) (int64, error) {
	fields := make(map[string]string, 1)

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fields["id"] = "must be integer"
	} else if id <= 0 {
		fields["id"] = "must be positive"
	}

	if len(fields) > 0 {
		return 0, &domain.ValidationError{Fields: fields}
	}

	return id, nil
}
