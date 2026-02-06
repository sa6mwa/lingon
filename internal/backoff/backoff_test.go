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
