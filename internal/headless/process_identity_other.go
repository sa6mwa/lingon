//go:build !linux

package headless

import "time"

// ProcessMatchesStartTime reports whether pid still appears to be the process
// recorded at startedAt. Platforms without a reliable process start-time check
// return false rather than signaling a potentially reused PID.
func ProcessMatchesStartTime(pid int, startedAt time.Time) bool {
	return false
}

// RecordedProcessMayMatch falls back to PID liveness on platforms without
// process start-time identity, preserving forced detach behavior there.
func RecordedProcessMayMatch(pid int, startedAt time.Time) bool {
	return pid > 0
}
