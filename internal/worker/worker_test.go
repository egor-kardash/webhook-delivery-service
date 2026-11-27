package worker_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/internal/worker"
)

type stubProcessor struct {
	mu         sync.Mutex
	calls      int
	maxPerTick int
	started    chan int

	block bool
}

func (p *stubProcessor) ProcessDue(ctx context.Context, maxPerTick int) error {
	p.mu.Lock()
	p.calls++
	p.maxPerTick = maxPerTick
	p.mu.Unlock()

	if p.started != nil {
		select {
		case p.started <- maxPerTick:
		default:
		}
	}

	if p.block {
		<-ctx.Done()
	}

	return nil
}

func newTestWorker(
	t *testing.T,
	processor worker.Processor,
	pollInterval time.Duration,
	maxPerTick int,
) *worker.Worker {
	t.Helper()

	w, err := worker.New(processor, worker.Options{
		PollInterval: pollInterval,
		MaxPerTick:   maxPerTick,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("worker.New: %v", err)
	}

	return w
}

func TestWorkerRunPassesMaxPerTickAndStopsOnContext(t *testing.T) {
	processor := &stubProcessor{started: make(chan int, 1)}
	w := newTestWorker(t, processor, time.Hour, 5)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)

	go func() {
		done <- w.Run(ctx)
	}()

	select {
	case got := <-processor.started:
		if got != 5 {
			t.Errorf("maxPerTick = %d, want 5", got)
		}
	case <-time.After(time.Second):
		t.Fatal("process due was not called")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop after context cancellation")
	}
}

func TestWorkerRunStopsWhileProcessorIsBlocked(t *testing.T) {
	processor := &stubProcessor{started: make(chan int, 1), block: true}
	w := newTestWorker(t, processor, time.Hour, 1)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)

	go func() {
		done <- w.Run(ctx)
	}()

	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("process due was not called")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop after context cancellation")
	}
}
