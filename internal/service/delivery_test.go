package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
	"github.com/egor-kardash/webhook-delivery-service/internal/service"
)

type stubDeliveryStore struct {
	getCalls     int
	gotGetID     int64
	getResult    *domain.Delivery
	getErr       error
	jobs         []*domain.DeliveryJob
	claimCalls   int
	releaseCalls int
	recordCalls  int
	records      []domain.AttemptResult
	recordErrAt  int
	recordErr    error
	resetCalls   int
	gotResetID   int64
	resetResult  *domain.Delivery
	resetErr     error
}

func (s *stubDeliveryStore) GetByID(_ context.Context, id int64) (*domain.Delivery, error) {
	s.getCalls++
	s.gotGetID = id

	return s.getResult, s.getErr
}

func (s *stubDeliveryStore) ClaimOneDue(_ context.Context) (*domain.DeliveryJob, error) {
	s.claimCalls++
	if len(s.jobs) == 0 {
		return nil, nil
	}

	job := s.jobs[0]
	s.jobs = s.jobs[1:]

	return job, nil
}

func (s *stubDeliveryStore) ReleaseExpiredLeases(_ context.Context, _ int) (int, error) {
	s.releaseCalls++

	return 0, nil
}

func (s *stubDeliveryStore) ResetForRetry(_ context.Context, id int64) (*domain.Delivery, error) {
	s.resetCalls++
	s.gotResetID = id

	return s.resetResult, s.resetErr
}

func (s *stubDeliveryStore) RecordAttempt(_ context.Context, result domain.AttemptResult) error {
	s.recordCalls++
	s.records = append(s.records, result)

	if s.recordErrAt == s.recordCalls {
		return s.recordErr
	}

	return nil
}

type stubSender struct {
	send func(context.Context, domain.DeliveryJob) (int, error)
}

func (s *stubSender) Send(ctx context.Context, job domain.DeliveryJob) (int, error) {
	return s.send(ctx, job)
}

func newTestDeliveryService(t *testing.T, store service.DeliveryStore, sender service.Sender) *service.DeliveryService {
	t.Helper()

	svc, err := service.NewDeliveryService(
		store,
		sender,
		domain.RetryPolicy{
			MaxAttempts: 5,
			BaseDelay:   time.Second,
			MaxDelay:    time.Minute,
		},
	)
	if err != nil {
		t.Fatalf("service.NewDeliveryService: %v", err)
	}

	return svc
}

func deliveryJob(id int64) *domain.DeliveryJob {
	return &domain.DeliveryJob{
		ID:        id,
		EventID:   id + 100,
		EventType: "user.created",
		Payload:   []byte(`{}`),
		TargetURL: "https://example.com",
	}
}

func TestDeliveryServiceGetDelivery(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		want := &domain.Delivery{
			ID:             42,
			EventID:        7,
			SubscriptionID: 3,
			Status:         domain.DeliveryPending,
		}
		store := &stubDeliveryStore{getResult: want}
		svc := newTestDeliveryService(t, store, &stubSender{})

		got, err := svc.GetDelivery(t.Context(), want.ID)
		if err != nil {
			t.Fatalf("service.GetDelivery: %v", err)
		}
		if got != want {
			t.Error("service.GetDelivery must return the delivery from the store")
		}
		if store.getCalls != 1 {
			t.Errorf("store.GetByID calls = %d, want 1", store.getCalls)
		}
		if store.gotGetID != want.ID {
			t.Errorf("store.GetByID id = %d, want %d", store.gotGetID, want.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		store := &stubDeliveryStore{getErr: domain.ErrDeliveryNotFound}
		svc := newTestDeliveryService(t, store, &stubSender{})

		_, err := svc.GetDelivery(t.Context(), 42)
		if !errors.Is(err, domain.ErrDeliveryNotFound) {
			t.Errorf("service.GetDelivery error = %v, want %v", err, domain.ErrDeliveryNotFound)
		}
	})

	t.Run("store error", func(t *testing.T) {
		storeErr := errors.New("store failure")
		store := &stubDeliveryStore{getErr: storeErr}
		svc := newTestDeliveryService(t, store, &stubSender{})

		_, err := svc.GetDelivery(t.Context(), 42)
		if !errors.Is(err, storeErr) {
			t.Errorf("service.GetDelivery error = %v, want %v", err, storeErr)
		}
	})
}

func TestDeliveryServiceRetryDelivery(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		want := &domain.Delivery{ID: 42, Status: domain.DeliveryPending}
		store := &stubDeliveryStore{resetResult: want}
		svc := newTestDeliveryService(t, store, &stubSender{})

		got, err := svc.RetryDelivery(t.Context(), want.ID)
		if err != nil {
			t.Fatalf("service.RetryDelivery: %v", err)
		}
		if got != want {
			t.Error("service.RetryDelivery must return the delivery from the store")
		}
		if store.resetCalls != 1 {
			t.Errorf("store.ResetForRetry calls = %d, want 1", store.resetCalls)
		}
		if store.gotResetID != want.ID {
			t.Errorf("store.ResetForRetry id = %d, want %d", store.gotResetID, want.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		store := &stubDeliveryStore{resetErr: domain.ErrDeliveryNotFound}
		svc := newTestDeliveryService(t, store, &stubSender{})

		_, err := svc.RetryDelivery(t.Context(), 42)
		if !errors.Is(err, domain.ErrDeliveryNotFound) {
			t.Errorf("service.RetryDelivery error = %v, want %v", err, domain.ErrDeliveryNotFound)
		}
	})

	t.Run("not retryable", func(t *testing.T) {
		store := &stubDeliveryStore{resetErr: domain.ErrDeliveryNotRetryable}
		svc := newTestDeliveryService(t, store, &stubSender{})

		_, err := svc.RetryDelivery(t.Context(), 42)
		if !errors.Is(err, domain.ErrDeliveryNotRetryable) {
			t.Errorf("service.RetryDelivery error = %v, want %v", err, domain.ErrDeliveryNotRetryable)
		}
	})
}

func TestProcessDue(t *testing.T) {
	t.Run("claims, sends and records jobs", func(t *testing.T) {
		store := &stubDeliveryStore{jobs: []*domain.DeliveryJob{deliveryJob(1), deliveryJob(2)}}
		codes := []int{200, 500}
		var sent int
		sender := &stubSender{send: func(context.Context, domain.DeliveryJob) (int, error) {
			code := codes[sent]
			sent++
			return code, nil
		}}
		svc := newTestDeliveryService(t, store, sender)

		if err := svc.ProcessDue(t.Context(), 5); err != nil {
			t.Fatalf("service.ProcessDue: %v", err)
		}

		if store.releaseCalls != 1 {
			t.Errorf("release calls = %d, want 1", store.releaseCalls)
		}
		if store.claimCalls != 3 {
			t.Errorf("claim calls = %d, want 3", store.claimCalls)
		}
		if sent != 2 {
			t.Errorf("send calls = %d, want 2", sent)
		}
		if len(store.records) != 2 {
			t.Fatalf("records = %d, want 2", len(store.records))
		}
		if store.records[0].Status != domain.DeliveryDelivered {
			t.Errorf("first status = %q, want %q", store.records[0].Status, domain.DeliveryDelivered)
		}
		if store.records[1].Status != domain.DeliveryPending {
			t.Errorf("second status = %q, want %q", store.records[1].Status, domain.DeliveryPending)
		}
		if store.records[0].RetryAfter != 0 {
			t.Errorf("delivered attempt RetryAfter = %s, want 0", store.records[0].RetryAfter)
		}
		if store.records[1].RetryAfter <= 0 {
			t.Errorf("pending attempt RetryAfter = %s, want positive", store.records[1].RetryAfter)
		}
	})

	t.Run("sender error does not abort the batch", func(t *testing.T) {
		store := &stubDeliveryStore{jobs: []*domain.DeliveryJob{deliveryJob(1), deliveryJob(2)}}
		var sent int
		sender := &stubSender{send: func(context.Context, domain.DeliveryJob) (int, error) {
			sent++
			if sent == 1 {
				return 0, errors.New("connection refused")
			}
			return 204, nil
		}}
		svc := newTestDeliveryService(t, store, sender)

		if err := svc.ProcessDue(t.Context(), 5); err != nil {
			t.Fatalf("service.ProcessDue: %v", err)
		}

		if store.claimCalls != 3 {
			t.Errorf("claim calls = %d, want 3", store.claimCalls)
		}
		if len(store.records) != 2 {
			t.Fatalf("records = %d, want 2", len(store.records))
		}
		if store.records[0].Status != domain.DeliveryPending {
			t.Errorf("first status = %q, want %q", store.records[0].Status, domain.DeliveryPending)
		}
		if store.records[1].Status != domain.DeliveryDelivered {
			t.Errorf("second status = %q, want %q", store.records[1].Status, domain.DeliveryDelivered)
		}
	})

	t.Run("record error is returned", func(t *testing.T) {
		recordErr := errors.New("database unavailable")
		store := &stubDeliveryStore{
			jobs:        []*domain.DeliveryJob{deliveryJob(1), deliveryJob(2)},
			recordErrAt: 1,
			recordErr:   recordErr,
		}
		sender := &stubSender{send: func(context.Context, domain.DeliveryJob) (int, error) {
			return 200, nil
		}}
		svc := newTestDeliveryService(t, store, sender)

		err := svc.ProcessDue(t.Context(), 5)
		if !errors.Is(err, recordErr) {
			t.Fatalf("service.ProcessDue error = %v, want %v", err, recordErr)
		}
		if store.recordCalls != 2 {
			t.Errorf("record calls = %d, want 2", store.recordCalls)
		}
	})

	t.Run("does not exceed max per tick", func(t *testing.T) {
		store := &stubDeliveryStore{
			jobs: []*domain.DeliveryJob{deliveryJob(1), deliveryJob(2), deliveryJob(3)},
		}
		sender := &stubSender{send: func(context.Context, domain.DeliveryJob) (int, error) {
			return 200, nil
		}}
		svc := newTestDeliveryService(t, store, sender)

		if err := svc.ProcessDue(t.Context(), 2); err != nil {
			t.Fatalf("service.ProcessDue: %v", err)
		}
		if store.claimCalls != 2 {
			t.Errorf("claim calls = %d, want 2", store.claimCalls)
		}
		if len(store.records) != 2 {
			t.Errorf("records = %d, want 2", len(store.records))
		}
	})
}
