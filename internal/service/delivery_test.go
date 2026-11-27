package service_test

import (
	"context"
	"errors"
	"testing"

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
}

func (s *stubDeliveryStore) GetByID(_ context.Context, id int64) (*domain.Delivery, error) {
	s.getCalls++
	s.gotGetID = id

	return s.getResult, s.getErr
}

func (s *stubDeliveryStore) ClaimOneDue(context.Context) (*domain.DeliveryJob, error) {
	s.claimCalls++
	if len(s.jobs) == 0 {
		return nil, nil
	}

	job := s.jobs[0]
	s.jobs = s.jobs[1:]

	return job, nil
}

func (s *stubDeliveryStore) ReleaseExpiredLeases(context.Context) (int, error) {
	s.releaseCalls++

	return 0, nil
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
		svc := service.NewDeliveryService(store, &stubSender{})

		got, err := svc.GetDelivery(t.Context(), want.ID)
		if err != nil {
			t.Fatalf("service.GetDelivery: %v", err)
		}
		if got != want {
			t.Error("GetDelivery must return the delivery from the store")
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
		svc := service.NewDeliveryService(store, &stubSender{})

		_, err := svc.GetDelivery(t.Context(), 42)
		if !errors.Is(err, domain.ErrDeliveryNotFound) {
			t.Errorf("service.GetDelivery error = %v, want %v", err, domain.ErrDeliveryNotFound)
		}
	})

	t.Run("store error", func(t *testing.T) {
		storeErr := errors.New("store failure")
		store := &stubDeliveryStore{getErr: storeErr}
		svc := service.NewDeliveryService(store, &stubSender{})

		_, err := svc.GetDelivery(t.Context(), 42)
		if !errors.Is(err, storeErr) {
			t.Errorf("service.GetDelivery error = %v, want %v", err, storeErr)
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
		svc := service.NewDeliveryService(store, sender)

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
		svc := service.NewDeliveryService(store, sender)

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
		svc := service.NewDeliveryService(store, sender)

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
		svc := service.NewDeliveryService(store, sender)

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
