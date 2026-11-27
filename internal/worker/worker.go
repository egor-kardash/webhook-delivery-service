package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Processor interface {
	ProcessDue(ctx context.Context, maxPerTick int) error
}

type Options struct {
	PollInterval time.Duration
	MaxPerTick   int
}

type Worker struct {
	processor  Processor
	interval   time.Duration
	maxPerTick int
	log        *slog.Logger
}

func New(processor Processor, opts Options, log *slog.Logger) (*Worker, error) {
	if opts.PollInterval <= 0 {
		return nil, fmt.Errorf("poll interval = %s, must be greater than 0", opts.PollInterval)
	}
	if opts.MaxPerTick <= 0 {
		return nil, fmt.Errorf("max per tick = %d, must be greater than 0", opts.MaxPerTick)
	}
	if log == nil {
		log = slog.Default()
	}

	return &Worker{
		processor:  processor,
		interval:   opts.PollInterval,
		maxPerTick: opts.MaxPerTick,
		log:        log,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return nil
		}

		if err := w.processor.ProcessDue(ctx, w.maxPerTick); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.log.Error("process due deliveries", "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
