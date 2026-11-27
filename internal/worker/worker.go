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

type Config struct {
	PollInterval time.Duration
	MaxPerTick   int
}

type Worker struct {
	processor  Processor
	interval   time.Duration
	maxPerTick int
	log        *slog.Logger
}

func New(processor Processor, cfg Config, log *slog.Logger) (*Worker, error) {
	if cfg.PollInterval <= 0 {
		return nil, fmt.Errorf("poll interval = %s, must be greater than 0", cfg.PollInterval)
	}
	if cfg.MaxPerTick <= 0 {
		return nil, fmt.Errorf("max per tick = %d, must be greater than 0", cfg.MaxPerTick)
	}
	if log == nil {
		log = slog.Default()
	}

	return &Worker{
		processor:  processor,
		interval:   cfg.PollInterval,
		maxPerTick: cfg.MaxPerTick,
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
