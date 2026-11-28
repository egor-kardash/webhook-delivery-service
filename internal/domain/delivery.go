package domain

import (
	"encoding/json"
	"time"
)

type DeliveryStatus string

const (
	DeliveryPending    DeliveryStatus = "pending"
	DeliveryInProgress DeliveryStatus = "in_progress"
	DeliveryDelivered  DeliveryStatus = "delivered"
	DeliveryFailed     DeliveryStatus = "failed"
	DeliveryDead       DeliveryStatus = "dead"
)

func (s DeliveryStatus) IsAttemptOutcome() bool {
	switch s {
	case DeliveryPending, DeliveryDelivered, DeliveryFailed, DeliveryDead:
		return true
	default:
		return false
	}
}

type Delivery struct {
	ID             int64
	EventID        int64
	SubscriptionID int64
	Status         DeliveryStatus
	Attempts       int
	NextAttemptAt  time.Time
	LastStatusCode int
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type DeliveryJob struct {
	ID             int64
	EventID        int64
	SubscriptionID int64
	Attempts       int
	EventType      string
	Payload        json.RawMessage
	TargetURL      string
}

type AttemptResult struct {
	DeliveryID     int64
	Status         DeliveryStatus
	RetryAfter     time.Duration
	LastStatusCode *int
	LastError      string
}
