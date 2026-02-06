package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pkt.systems/lingon/internal/config"
)

// Writer emits JSONL trace events.
type Writer struct {
	mu   sync.Mutex
	enc  *json.Encoder
	file *os.File
	path string
}

// DefaultPath returns the default trace file path under ~/.lingon/debug.
func DefaultPath() (string, error) {
	dir := filepath.Join(config.DefaultConfigDir(), "debug")
	ts := time.Now().UTC().Format("20060102-150405")
	return filepath.Join(dir, fmt.Sprintf("lingon-%s.jsonl", ts)), nil
}

// New creates a trace writer at the provided path.
func New(path string) (*Writer, error) {
	if path == "" {
		return nil, fmt.Errorf("trace path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(file)
	enc.SetEscapeHTML(false)
	return &Writer{enc: enc, file: file, path: path}, nil
}

// Path returns the trace file path.
func (w *Writer) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

// Close closes the underlying file.
func (w *Writer) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// Event writes a single JSONL event.
func (w *Writer) Event(name string, fields map[string]any) {
	if w == nil || w.enc == nil {
		return
	}
	entry := make(map[string]any, len(fields)+2)
	entry["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	entry["event"] = name
	for key, value := range fields {
		entry[key] = value
	}
	w.mu.Lock()
	_ = w.enc.Encode(entry)
	w.mu.Unlock()
}

// SummarizeBytes returns a compact summary of a byte slice.
func SummarizeBytes(data []byte, limit int) map[string]any {
	if limit <= 0 {
		limit = 200
	}
	hash := sha256.Sum256(data)
	preview := data
	if len(preview) > limit {
		preview = preview[:limit]
	}
	quoted := strconv.QuoteToASCII(string(preview))
	if len(quoted) >= 2 {
		quoted = strings.TrimSuffix(strings.TrimPrefix(quoted, `"`), `"`)
	}
	return map[string]any{
		"len":     len(data),
		"hash":    hex.EncodeToString(hash[:]),
		"preview": quoted,
		"trunc":   len(data) > limit,
	}
}
