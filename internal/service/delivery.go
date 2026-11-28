package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

type DeliveryStore interface {
	GetByID(ctx context.Context, id int64) (*domain.Delivery, error)
	ClaimOneDue(ctx context.Context) (*domain.DeliveryJob, error)
	ReleaseExpiredLeases(ctx context.Context, maxAttempts int) (int, error)
	RecordAttempt(ctx context.Context, result domain.AttemptResult) error
	ResetForRetry(ctx context.Context, id int64) (*domain.Delivery, error)
}

type Sender interface {
	Send(ctx context.Context, job domain.DeliveryJob) (statusCode int, err error)
}

type DeliveryService struct {
	store  DeliveryStore
	sender Sender
	policy domain.RetryPolicy
}

func NewDeliveryService(store DeliveryStore, sender Sender, policy domain.RetryPolicy) (*DeliveryService, error) {
	if store == nil {
		return nil, errors.New("delivery store is required")
	}
	if sender == nil {
		return nil, errors.New("sender is required")
	}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("retry policy: %w", err)
	}

	return &DeliveryService{
		store:  store,
		sender: sender,
		policy: policy,
	}, nil
}

func (s *DeliveryService) GetDelivery(ctx context.Context, id int64) (*domain.Delivery, error) {
	delivery, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get delivery %d: %w", id, err)
	}

	return delivery, nil
}

func (s *DeliveryService) RetryDelivery(ctx context.Context, id int64) (*domain.Delivery, error) {
	delivery, err := s.store.ResetForRetry(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("retry delivery %d: %w", id, err)
	}

	return delivery, nil
}

func (s *DeliveryService) ProcessDue(ctx context.Context, maxPerTick int) error {
	if maxPerTick <= 0 {
		return fmt.Errorf("process due limit = %d, must be greater than 0", maxPerTick)
	}

	if _, err := s.store.ReleaseExpiredLeases(ctx, s.policy.MaxAttempts); err != nil {
		return fmt.Errorf("release expired leases: %w", err)
	}

	var errs []error

	for processed := 0; processed < maxPerTick; processed++ {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}

		job, err := s.store.ClaimOneDue(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("claim due delivery: %w", err))
			break
		}
		if job == nil {
			break
		}

		statusCode, sendErr := s.sender.Send(ctx, *job)
		result := s.evaluateAttempt(*job, statusCode, sendErr)

		if err := s.store.RecordAttempt(ctx, result); err != nil {
			errs = append(errs, fmt.Errorf("record attempt for delivery %d: %w", job.ID, err))
		}
	}

	return errors.Join(errs...)
}

func (s *DeliveryService) evaluateAttempt(job domain.DeliveryJob, statusCode int, sendErr error) domain.AttemptResult {
	result := domain.AttemptResult{DeliveryID: job.ID}

	if sendErr != nil {
		result.LastError = sendErr.Error()

		if errors.Is(sendErr, domain.ErrPermanentDelivery) {
			result.Status = domain.DeliveryFailed
			return result
		}

		return s.retryOrDead(job, result)
	}

	if statusCode < 100 || statusCode > 599 {
		result.LastError = fmt.Sprintf("unexpected status code %d", statusCode)
		return s.retryOrDead(job, result)
	}

	code := statusCode
	result.LastStatusCode = &code

	switch {
	case statusCode >= 200 && statusCode < 300:
		result.Status = domain.DeliveryDelivered
	case statusCode == 408 || statusCode == 429:
		return s.retryOrDead(job, result)
	case statusCode >= 500 && statusCode <= 599:
		return s.retryOrDead(job, result)
	default:
		result.Status = domain.DeliveryFailed
	}

	return result
}

func (s *DeliveryService) retryOrDead(job domain.DeliveryJob, result domain.AttemptResult) domain.AttemptResult {
	attemptsUsed := job.Attempts + 1

	if attemptsUsed >= s.policy.MaxAttempts {
		result.Status = domain.DeliveryDead
		return result
	}

	result.Status = domain.DeliveryPending
	result.RetryAfter = s.policy.NextDelay(attemptsUsed)

	return result
}
