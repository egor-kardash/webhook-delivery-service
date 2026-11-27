package domain

import (
	"errors"
	"fmt"
)

var (
	ErrDeliveryNotFound  = errors.New("delivery not found")
	ErrLeaseLost         = errors.New("delivery lease lost")
	ErrPermanentDelivery = errors.New("permanent delivery failure")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %d field(s)", len(e.Fields))
}
