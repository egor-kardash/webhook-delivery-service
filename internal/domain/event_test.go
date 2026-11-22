package domain_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

const (
	testEventType      = "user.created"
	testIdempotencyKey = "idem-key-1"
)

func TestNewEvent(t *testing.T) {
	cases := []struct {
		name               string
		eventType          string
		idempotencyKey     string
		payload            json.RawMessage
		wantEventType      string
		wantIdempotencyKey string
		wantPayload        string
	}{
		{
			name:               "valid event",
			eventType:          testEventType,
			idempotencyKey:     testIdempotencyKey,
			payload:            json.RawMessage(`{"id":1}`),
			wantEventType:      testEventType,
			wantIdempotencyKey: testIdempotencyKey,
			wantPayload:        `{"id":1}`,
		},
		{
			name:               "event type and idempotency key are trimmed",
			eventType:          "  \t" + testEventType + " \n",
			idempotencyKey:     "  " + testIdempotencyKey + "\r\n",
			payload:            json.RawMessage(`{"id":1}`),
			wantEventType:      testEventType,
			wantIdempotencyKey: testIdempotencyKey,
			wantPayload:        `{"id":1}`,
		},
		{
			name:               "payload is trimmed",
			eventType:          testEventType,
			idempotencyKey:     testIdempotencyKey,
			payload:            json.RawMessage(" \n\t {\"id\":1} \r\n"),
			wantEventType:      testEventType,
			wantIdempotencyKey: testIdempotencyKey,
			wantPayload:        `{"id":1}`,
		},
		{
			name:               "empty object payload is valid",
			eventType:          testEventType,
			idempotencyKey:     testIdempotencyKey,
			payload:            json.RawMessage(`{}`),
			wantEventType:      testEventType,
			wantIdempotencyKey: testIdempotencyKey,
			wantPayload:        `{}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := domain.NewEvent(tc.eventType, tc.idempotencyKey, tc.payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if event.EventType != tc.wantEventType {
				t.Errorf("EventType = %q, want %q", event.EventType, tc.wantEventType)
			}

			if event.IdempotencyKey != tc.wantIdempotencyKey {
				t.Errorf("IdempotencyKey = %q, want %q", event.IdempotencyKey, tc.wantIdempotencyKey)
			}

			if string(event.Payload) != tc.wantPayload {
				t.Errorf("Payload = %s, want %s", event.Payload, tc.wantPayload)
			}
		})
	}
}

func TestNewEventValidation(t *testing.T) {
	cases := []struct {
		name           string
		eventType      string
		idempotencyKey string
		payload        json.RawMessage
		wantField      string
	}{
		{
			name:           "empty event type",
			eventType:      "",
			idempotencyKey: testIdempotencyKey,
			payload:        json.RawMessage(`{"id":1}`),
			wantField:      "event_type",
		},
		{
			name:           "whitespace only event type",
			eventType:      "   \t\n  ",
			idempotencyKey: testIdempotencyKey,
			payload:        json.RawMessage(`{"id":1}`),
			wantField:      "event_type",
		},
		{
			name:           "empty idempotency key",
			eventType:      testEventType,
			idempotencyKey: "",
			payload:        json.RawMessage(`{"id":1}`),
			wantField:      "idempotency_key",
		},
		{
			name:           "whitespace only idempotency key",
			eventType:      testEventType,
			idempotencyKey: "  \r\n ",
			payload:        json.RawMessage(`{"id":1}`),
			wantField:      "idempotency_key",
		},
		{
			name:           "payload array",
			eventType:      testEventType,
			idempotencyKey: testIdempotencyKey,
			payload:        json.RawMessage(`[1,2]`),
			wantField:      "payload",
		},
		{
			name:           "payload string",
			eventType:      testEventType,
			idempotencyKey: testIdempotencyKey,
			payload:        json.RawMessage(`"text"`),
			wantField:      "payload",
		},
		{
			name:           "payload number",
			eventType:      testEventType,
			idempotencyKey: testIdempotencyKey,
			payload:        json.RawMessage(`42`),
			wantField:      "payload",
		},
		{
			name:           "payload null",
			eventType:      testEventType,
			idempotencyKey: testIdempotencyKey,
			payload:        json.RawMessage(`null`),
			wantField:      "payload",
		},
		{
			name:           "payload missing",
			eventType:      testEventType,
			idempotencyKey: testIdempotencyKey,
			payload:        nil,
			wantField:      "payload",
		},
		{
			name:           "payload invalid json",
			eventType:      testEventType,
			idempotencyKey: testIdempotencyKey,
			payload:        json.RawMessage(`{"id":`),
			wantField:      "payload",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.NewEvent(tc.eventType, tc.idempotencyKey, tc.payload)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}

			var valErr *domain.ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected *domain.ValidationError, got %T: %v", err, err)
			}

			if _, ok := valErr.Fields[tc.wantField]; !ok {
				t.Errorf("expected error on field %q, got fields: %v", tc.wantField, valErr.Fields)
			}
		})
	}
}
