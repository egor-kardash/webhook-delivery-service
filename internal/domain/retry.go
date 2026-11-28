package domain

import (
	"fmt"
	"math/rand/v2"
	"time"
)

const jitterFactor = 0.2

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 1 {
		return fmt.Errorf("retry max attempts = %d, must be at least 1", p.MaxAttempts)
	}
	if p.BaseDelay <= 0 {
		return fmt.Errorf("retry base delay = %s, must be greater than 0", p.BaseDelay)
	}
	if p.MaxDelay <= 0 {
		return fmt.Errorf("retry max delay = %s, must be greater than 0", p.MaxDelay)
	}
	if p.MaxDelay < p.BaseDelay {
		return fmt.Errorf(
			"retry max delay = %s, must be greater than or equal to base delay %s",
			p.MaxDelay,
			p.BaseDelay,
		)
	}

	return nil
}

func (p RetryPolicy) NextDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := p.BaseDelay

	for i := 1; i < attempt; i++ {
		if delay >= p.MaxDelay {
			break
		}

		delay *= 2
		if delay > p.MaxDelay {
			delay = p.MaxDelay
			break
		}
	}

	if delay <= 0 {
		return 0
	}

	spread := time.Duration(float64(delay) * jitterFactor)
	if spread <= 0 {
		return delay
	}

	delta := time.Duration(rand.Int64N(int64(2*spread)+1)) - spread
	delay += delta

	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}

	return delay
}
