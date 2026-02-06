package session

import (
	"time"

	"pkt.systems/lingon/internal/clock"
)

func advanceClock(clk clock.Clock, d time.Duration) {
	if mock, ok := clk.(*clock.MockClock); ok {
		mock.Add(d)
		return
	}
	clk.Sleep(d)
}
