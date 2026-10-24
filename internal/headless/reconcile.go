package headless

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SocketExists reports whether path exists and is a unix socket.
func SocketExists(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSocket != 0
}

// Reconcile prunes stale state entries and returns remaining sessions sorted by id.
func (s *Store) Reconcile() ([]SessionRecord, error) {
	if s == nil {
		return nil, os.ErrInvalid
	}
	var out []SessionRecord
	err := s.WithLock(func(state *State) error {
		for id, rec := range state.Sessions {
			if !sessionRecordAlive(rec) {
				s.removeOwnedSocket(rec.SocketPath)
				delete(state.Sessions, id)
				continue
			}
			out = append(out, rec)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SessionID < out[j].SessionID
	})
	return out, nil
}

func sessionRecordAlive(rec SessionRecord) bool {
	if rec.PID > 0 {
		if PIDAlive(rec.PID) && RecordedProcessMayMatch(rec.PID, rec.StartedAt) {
			return true
		}
		return SocketReachable(rec.SocketPath)
	}
	return SocketReachable(rec.SocketPath)
}

// SocketReachable reports whether path is a live unix socket accepting connections.
func SocketReachable(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || !SocketExists(trimmed) {
		return false
	}
	conn, err := net.DialTimeout("unix", trimmed, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (s *Store) removeOwnedSocket(socketPath string) {
	trimmed := strings.TrimSpace(socketPath)
	if trimmed == "" || !SocketExists(trimmed) {
		return
	}
	base := filepath.Clean(filepath.Dir(s.path))
	target := filepath.Clean(trimmed)
	if target == base || !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return
	}
	_ = os.Remove(target)
}
