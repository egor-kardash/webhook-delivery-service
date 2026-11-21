package domain

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

type Event struct {
	ID             int64
	EventType      string
	Payload        json.RawMessage
	IdempotencyKey string
	CreatedAt      time.Time
}

type PublishResult struct {
	EventID     int64
	DeliveryIDs []int64
	Created     bool
}

func NewEvent(eventType, idempotencyKey string, payload json.RawMessage) (*Event, error) {
	eventType = strings.TrimSpace(eventType)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	payload = bytes.TrimSpace(payload)

	fields := make(map[string]string, 3)

	if eventType == "" {
		fields["event_type"] = "must not be blank"
	}
	if idempotencyKey == "" {
		fields["idempotency_key"] = "must not be blank"
	}
	if !isValidJSONObject(payload) {
		fields["payload"] = "must be a valid JSON object"
	}

	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}

	return &Event{
		EventType:      eventType,
		Payload:        bytes.Clone(payload),
		IdempotencyKey: idempotencyKey,
	}, nil
}

func isValidJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}

	return json.Valid(raw)
}
