package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

func TestEvaluateAttempt(t *testing.T) {
	job := domain.DeliveryJob{ID: 1}
	permanentErr := fmt.Errorf("blocked target: %w", domain.ErrPermanentDelivery)

	cases := []struct {
		name           string
		statusCode     int
		sendErr        error
		wantStatus     domain.DeliveryStatus
		wantStatusCode *int
		wantLastError  string
	}{
		{
			name:           "2xx delivered",
			statusCode:     200,
			wantStatus:     domain.DeliveryDelivered,
			wantStatusCode: intPtr(200),
		},
		{
			name:           "3xx failed",
			statusCode:     302,
			wantStatus:     domain.DeliveryFailed,
			wantStatusCode: intPtr(302),
		},
		{
			name:           "4xx failed",
			statusCode:     400,
			wantStatus:     domain.DeliveryFailed,
			wantStatusCode: intPtr(400),
		},
		{
			name:           "408 pending",
			statusCode:     408,
			wantStatus:     domain.DeliveryPending,
			wantStatusCode: intPtr(408),
		},
		{
			name:           "429 pending",
			statusCode:     429,
			wantStatus:     domain.DeliveryPending,
			wantStatusCode: intPtr(429),
		},
		{
			name:           "5xx pending",
			statusCode:     500,
			wantStatus:     domain.DeliveryPending,
			wantStatusCode: intPtr(500),
		},
		{
			name:          "network error pending",
			sendErr:       errors.New("connection reset"),
			wantStatus:    domain.DeliveryPending,
			wantLastError: "connection reset",
		},
		{
			name:          "permanent error failed",
			sendErr:       permanentErr,
			wantStatus:    domain.DeliveryFailed,
			wantLastError: permanentErr.Error(),
		},
		{
			name:          "invalid status code pending",
			statusCode:    600,
			wantStatus:    domain.DeliveryPending,
			wantLastError: "unexpected status code 600",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := evaluateAttempt(job, tc.statusCode, tc.sendErr)

			if result.DeliveryID != job.ID {
				t.Errorf("DeliveryID = %d, want %d", result.DeliveryID, job.ID)
			}
			if result.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q", result.Status, tc.wantStatus)
			}
			if result.LastError != tc.wantLastError {
				t.Errorf("LastError = %q, want %q", result.LastError, tc.wantLastError)
			}

			if tc.wantStatusCode == nil {
				if result.LastStatusCode != nil {
					t.Errorf("LastStatusCode = %v, want nil", *result.LastStatusCode)
				}
				return
			}

			if result.LastStatusCode == nil {
				t.Fatal("LastStatusCode = nil, want non-nil")
			}
			if *result.LastStatusCode != *tc.wantStatusCode {
				t.Errorf("LastStatusCode = %d, want %d", *result.LastStatusCode, *tc.wantStatusCode)
			}
		})
	}
}

func intPtr(v int) *int {
	return &v
}
