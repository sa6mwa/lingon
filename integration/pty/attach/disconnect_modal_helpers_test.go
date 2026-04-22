//go:build integration
// +build integration

package integrationptyattach_test

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func collectCountdownSamples(t *testing.T, sess *ptytest.PTYSession, re *regexp.Regexp, d time.Duration) []int {
	t.Helper()
	deadline := ptytest.Now(sess.Clock()).Add(d)
	values := make([]int, 0, 8)
	for ptytest.Now(sess.Clock()).Before(deadline) {
		if exited, err := sess.WaitErr(0); exited {
			if err == nil || errorsIsCanceledOrNoSessions(err) {
				break
			}
			t.Fatalf("attach exited unexpectedly while sampling countdown: %v", err)
		}
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

func errorsIsCanceledOrNoSessions(err error) bool {
	return errorsIsCanceled(err) || strings.Contains(err.Error(), "no sessions available")
}

func errorsIsCanceled(err error) bool {
	return err == context.Canceled || strings.Contains(err.Error(), context.Canceled.Error())
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
