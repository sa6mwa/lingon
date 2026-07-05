//go:build linux

package headless

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	linuxClockTicksPerSecond = 100
	processStartTolerance    = 10 * time.Second
)

// ProcessMatchesStartTime reports whether pid still appears to be the process
// recorded at startedAt. It rejects stale records after PID reuse.
func ProcessMatchesStartTime(pid int, startedAt time.Time) bool {
	if pid <= 0 || startedAt.IsZero() {
		return false
	}
	actual, ok := processStartTime(pid)
	if !ok {
		return false
	}
	delta := actual.Sub(startedAt)
	if delta < 0 {
		delta = -delta
	}
	return delta <= processStartTolerance
}

// RecordedProcessMayMatch reports whether pid is safe to treat as the recorded process.
func RecordedProcessMayMatch(pid int, startedAt time.Time) bool {
	return ProcessMatchesStartTime(pid, startedAt)
}

func processStartTime(pid int) (time.Time, bool) {
	boot, ok := linuxBootTime()
	if !ok {
		return time.Time{}, false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, false
	}
	raw := string(data)
	endComm := strings.LastIndex(raw, ") ")
	if endComm < 0 || endComm+2 >= len(raw) {
		return time.Time{}, false
	}
	fields := strings.Fields(raw[endComm+2:])
	if len(fields) <= 19 {
		return time.Time{}, false
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	sec := ticks / linuxClockTicksPerSecond
	nsec := (ticks % linuxClockTicksPerSecond) * int64(time.Second/linuxClockTicksPerSecond)
	return boot.Add(time.Duration(sec)*time.Second + time.Duration(nsec)), true
}

func linuxBootTime() (time.Time, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		value, ok := strings.CutPrefix(line, "btime ")
		if !ok {
			continue
		}
		sec, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return time.Time{}, false
		}
		return time.Unix(sec, 0), true
	}
	return time.Time{}, false
}
