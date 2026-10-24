package backoff

import (
	"math"
	"testing"
	"time"
)

func TestPolicyNextClampsAttemptBeforePow(t *testing.T) {
	p := Policy{
		Base:   time.Second,
		Factor: 2,
		Max:    time.Minute,
	}

	got := p.Next(math.MaxInt)
	if got != p.Max {
		t.Fatalf("Next(max-int) = %v, want %v", got, p.Max)
	}
}

func TestPolicyNextHandlesNonFiniteFactor(t *testing.T) {
	p := Policy{
		Base:   time.Second,
		Factor: math.NaN(),
		Max:    10 * time.Second,
	}

	if got := p.Next(1); got != 2*time.Second {
		t.Fatalf("Next with NaN factor = %v, want %v", got, 2*time.Second)
	}

	p.Factor = math.Inf(1)
	if got := p.Next(1); got != 2*time.Second {
		t.Fatalf("Next with +Inf factor = %v, want %v", got, 2*time.Second)
	}
}

func TestPolicyNextNeverReturnsNonPositiveDelay(t *testing.T) {
	p := Policy{
		Base:   time.Second,
		Factor: 2,
		Max:    time.Minute,
	}

	for _, attempt := range []int{0, 1, 2, 5, 10, 1000, math.MaxInt} {
		if got := p.Next(attempt); got <= 0 {
			t.Fatalf("Next(%d) = %v, want >0", attempt, got)
		}
	}
}

func TestPolicyWithJitterUsesBoundedSample(t *testing.T) {
	p := Policy{Jitter: 10 * time.Second}
	got := p.WithJitter(time.Second, func(max time.Duration) time.Duration {
		if max != 10*time.Second {
			t.Fatalf("jitter max = %v, want 10s", max)
		}
		return 7 * time.Second
	})
	if got != 8*time.Second {
		t.Fatalf("WithJitter = %v, want 8s", got)
	}
}

func TestPolicyWithJitterClampsSample(t *testing.T) {
	p := Policy{Jitter: time.Second}
	if got := p.WithJitter(time.Second, func(time.Duration) time.Duration { return 2 * time.Second }); got != 2*time.Second {
		t.Fatalf("WithJitter high sample = %v, want 2s", got)
	}
	if got := p.WithJitter(time.Second, func(time.Duration) time.Duration { return -time.Second }); got != time.Second {
		t.Fatalf("WithJitter low sample = %v, want 1s", got)
	}
}
