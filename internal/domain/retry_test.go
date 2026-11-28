package domain_test

import (
	"testing"
	"time"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

func TestRetryPolicyNextDelay(t *testing.T) {
	p := domain.RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   time.Second,
		MaxDelay:    time.Minute,
	}

	first := p.NextDelay(1)
	if first < 800*time.Millisecond || first > 1200*time.Millisecond {
		t.Fatalf("first delay = %s, want 0.8s..1.2s", first)
	}

	second := p.NextDelay(2)
	if second < 1600*time.Millisecond || second > 2400*time.Millisecond {
		t.Fatalf("second delay = %s, want 1.6s..2.4s", second)
	}

	if got := p.NextDelay(10); got > time.Minute {
		t.Fatalf("capped delay = %s, want <= 1m", got)
	}
}
