//go:build !linux

package headless

import (
	"testing"
	"time"
)

func TestRecordedProcessMayMatchFallsBackOffLinux(t *testing.T) {
	if ProcessMatchesStartTime(12345, time.Now().UTC()) {
		t.Fatalf("ProcessMatchesStartTime should be unavailable off Linux")
	}
	if !RecordedProcessMayMatch(12345, time.Now().UTC()) {
		t.Fatalf("RecordedProcessMayMatch should preserve PID fallback off Linux")
	}
}
