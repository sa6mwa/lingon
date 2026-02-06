package session

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync/atomic"
)

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var defaultSessionSequence int64 = -2

func defaultSessionIdentity() (string, string) {
	name := defaultSessionName()
	seq := nextSessionSequence()
	return name + defaultSessionSequenceSuffix(seq), name + defaultSessionSequenceSuffix(seq)
}

func defaultSessionSequenceSuffix(seq int64) string {
	if seq >= 0 {
		return fmt.Sprintf("-%d", seq)
	}
	return strconv.FormatInt(seq, 10)
}

func defaultSessionName() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "host"
	}
	host := sanitizeLabel(hostname)
	if len(host) > 4 {
		host = host[:4]
	}
	return host + hostnameHash(hostname)
}

func sanitizeLabel(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "host"
	}
	var b strings.Builder
	b.Grow(len(value))
	lastDash := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		ok := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
		if ok {
			b.WriteByte(ch)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "host"
	}
	return out
}

func hostnameHash(hostname string) string {
	normalized := strings.ToLower(strings.TrimSpace(hostname))
	if normalized == "" {
		normalized = "host"
	}
	currentUser := os.Getenv("USER")
	if currentUser == "" {
		u, err := user.Current()
		if err == nil && u.Username != "" {
			currentUser = u.Username
		}
	}
	if currentUser == "" {
		currentUser = "unknown"
	}
	seed := fmt.Sprintf("%s|%s|%d", normalized, currentUser, os.Getpid())
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	return base62Encode(h.Sum64(), 4)
}

func base62Encode(value uint64, width int) string {
	if width <= 0 {
		return ""
	}
	out := make([]byte, width)
	base := uint64(len(base62Alphabet))
	for i := width - 1; i >= 0; i-- {
		out[i] = base62Alphabet[int(value%base)]
		value /= base
	}
	return string(out)
}

func nextSessionSequence() int64 {
	return atomic.AddInt64(&defaultSessionSequence, 1)
}
