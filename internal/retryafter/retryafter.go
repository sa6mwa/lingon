package retryafter

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Error wraps an underlying error with a retry-after delay.
type Error struct {
	Err        error
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "retry after"
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// FromError extracts a retry-after duration from an error.
func FromError(err error) (time.Duration, bool) {
	var ra *Error
	if errors.As(err, &ra) && ra.RetryAfter > 0 {
		return ra.RetryAfter, true
	}
	return 0, false
}

// ParseHeader parses Retry-After header values (seconds only).
func ParseHeader(h http.Header) (time.Duration, bool) {
	if h == nil {
		return 0, false
	}
	raw := strings.TrimSpace(h.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
}
