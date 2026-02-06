package attach_test

import (
	"regexp"
	"strconv"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func collectCountdownSamples(sess *ptytest.PTYSession, re *regexp.Regexp, d time.Duration) []int {
	deadline := ptytest.Now(sess.Clock()).Add(d)
	values := make([]int, 0, 8)
	for ptytest.Now(sess.Clock()).Before(deadline) {
		screen := sess.Screen()
		match := re.FindStringSubmatch(screen.String())
		if len(match) == 2 {
			if val, err := strconv.Atoi(match[1]); err == nil {
				values = append(values, val)
			}
		}
		ptytest.Advance(sess.Clock(), 200*time.Millisecond)
	}
	return values
}

func hasInterleavedZero(values []int) bool {
	hasZero := false
	hasHigh := false
	for _, v := range values {
		if v == 0 {
			hasZero = true
		}
		if v >= 2 {
			hasHigh = true
		}
	}
	return hasZero && hasHigh
}
