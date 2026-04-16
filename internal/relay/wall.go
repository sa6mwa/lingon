package relay

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

const (
	defaultWallTimeout    = 5 * time.Second
	defaultWallEventTTL   = 6 * time.Hour
	defaultWallEventLimit = 100
	maxWallEventLimit     = 500
)

var defaultWallInactiveAfterLevels = []time.Duration{
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

type inactivityMonitor struct {
	username      string
	sessionID     string
	sender        string
	lastActivity  time.Time
	level         int
	inactiveAfter time.Duration
	timer         *time.Timer
}

type wallEvent struct {
	ID             uint64
	Username       string
	SessionID      string
	Sender         string
	Message        string
	TimeoutSeconds uint32
	Kind           protocolpb.WallKind
	CreatedAt      time.Time
}

// WallService manages relay-driven wall message fanout and inactivity monitors.
type WallService struct {
	mu                  sync.Mutex
	store               *Store
	hub                 *Hub
	logger              pslog.Logger
	timeout             time.Duration
	inactiveAfterLevels []time.Duration
	monitors            map[string]*inactivityMonitor
	bySession           map[string]map[string]struct{}
	eventTTL            time.Duration
	nextEventID         uint64
	eventsByUser        map[string][]wallEvent
}

func newWallService(store *Store, hub *Hub, logger pslog.Logger, timeout time.Duration, inactiveAfterLevels []time.Duration) *WallService {
	if logger == nil {
		logger = logging.Default()
	}
	s := &WallService{
		store:        store,
		hub:          hub,
		logger:       logger,
		monitors:     make(map[string]*inactivityMonitor),
		bySession:    make(map[string]map[string]struct{}),
		eventTTL:     defaultWallEventTTL,
		eventsByUser: make(map[string][]wallEvent),
	}
	s.setConfig(timeout, inactiveAfterLevels)
	return s
}

func (s *WallService) setConfig(timeout time.Duration, inactiveAfterLevels []time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setConfigLocked(timeout, inactiveAfterLevels)
}

func cloneWallInactiveAfterLevels(levels []time.Duration) []time.Duration {
	cloned := make([]time.Duration, len(levels))
	copy(cloned, levels)
	return cloned
}

func normalizeWallInactiveAfterLevels(levels []time.Duration) []time.Duration {
	if len(levels) == 0 {
		return cloneWallInactiveAfterLevels(defaultWallInactiveAfterLevels)
	}
	normalized := make([]time.Duration, 0, len(levels))
	seen := make(map[time.Duration]struct{}, len(levels))
	for _, level := range levels {
		if level <= 0 {
			continue
		}
		if _, exists := seen[level]; exists {
			continue
		}
		seen[level] = struct{}{}
		normalized = append(normalized, level)
	}
	if len(normalized) == 0 {
		return cloneWallInactiveAfterLevels(defaultWallInactiveAfterLevels)
	}
	return normalized
}

func durationSlicesEqual(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *WallService) setConfigLocked(timeout time.Duration, inactiveAfterLevels []time.Duration) {
	if timeout <= 0 {
		timeout = defaultWallTimeout
	}
	inactiveAfterLevels = normalizeWallInactiveAfterLevels(inactiveAfterLevels)
	levelsChanged := !durationSlicesEqual(s.inactiveAfterLevels, inactiveAfterLevels)
	s.timeout = timeout
	s.inactiveAfterLevels = cloneWallInactiveAfterLevels(inactiveAfterLevels)
	if !levelsChanged {
		return
	}
	now := time.Now().UTC()
	for key, monitor := range s.monitors {
		if monitor == nil {
			continue
		}
		if monitor.level < 0 {
			monitor.level = 0
		}
		if monitor.level >= len(s.inactiveAfterLevels) {
			monitor.level = len(s.inactiveAfterLevels) - 1
		}
		monitor.inactiveAfter = s.inactiveAfterLevels[monitor.level]
		s.scheduleMonitorLocked(key, monitor, now)
	}
}

func (s *WallService) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, monitor := range s.monitors {
		if monitor.timer != nil {
			monitor.timer.Stop()
			monitor.timer = nil
		}
	}
	s.monitors = make(map[string]*inactivityMonitor)
	s.bySession = make(map[string]map[string]struct{})
	s.eventsByUser = make(map[string][]wallEvent)
	s.nextEventID = 0
}

func (s *WallService) senderLabel(username, ip string) string {
	username = strings.TrimSpace(username)
	ip = strings.TrimSpace(ip)
	if username == "" && ip == "" {
		return "unknown"
	}
	if username == "" {
		return ip
	}
	if ip == "" {
		return username
	}
	return fmt.Sprintf("%s@%s", username, ip)
}

func (s *WallService) sendUserWall(username, sender, message string, now time.Time) (int, error) {
	return s.sendUserWallForSession(username, sender, message, "", protocolpb.WallKind_WALL_KIND_UNSPECIFIED, now)
}

func (s *WallService) sendUserWallForSession(username, sender, message, sourceSessionID string, kind protocolpb.WallKind, now time.Time) (int, error) {
	message = sanitizeWallMessage(message)
	if message == "" {
		return 0, fmt.Errorf("message is required")
	}
	if username == "" {
		return 0, fmt.Errorf("username is required")
	}
	if s.store == nil || s.hub == nil {
		return 0, fmt.Errorf("wall service unavailable")
	}
	sessions := s.store.ListSessions(username)
	if len(sessions) == 0 {
		// Keep backlog entries even when no live participants are currently connected.
		s.recordEvent(username, sourceSessionID, sender, message, s.timeoutSeconds(), kind, now)
		return 0, nil
	}
	timeoutSeconds := s.timeoutSeconds()
	eventID := s.recordEvent(username, sourceSessionID, sender, message, timeoutSeconds, kind, now)
	sent := 0
	for _, session := range sessions {
		if session.Status != "active" {
			continue
		}
		if !s.hub.HasParticipants(session.ID) {
			continue
		}
		frame := frameWall(session.ID, eventID, sender, message, timeoutSeconds, kind, sourceSessionID)
		if s.hub.BroadcastSessionFrame(context.Background(), session.ID, frame, true) {
			sent++
		}
	}
	if sent > 0 {
		s.logger.Info("relay.wall.sent", "user", username, "sessions", sent)
	}
	return sent, nil
}

func sanitizeWallMessage(raw string) string {
	raw = stripANSIEscapeSequences(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	lastSpace := false
	for len(raw) > 0 {
		r, size := utf8.DecodeRuneInString(raw)
		raw = raw[size:]
		if r == utf8.RuneError && size == 1 {
			continue
		}
		if unicode.IsSpace(r) {
			if lastSpace {
				continue
			}
			b.WriteByte(' ')
			lastSpace = true
			continue
		}
		if !isAllowedWallRune(r) {
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

func isAllowedWallRune(r rune) bool {
	if r >= 0x20 && r <= 0x7e {
		return true
	}
	if r == '\u200d' {
		return true
	}
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsMark(r)
}

func stripANSIEscapeSequences(raw string) string {
	if strings.IndexByte(raw, 0x1b) < 0 {
		return raw
	}
	src := []byte(raw)
	out := make([]byte, 0, len(src))
	for i := 0; i < len(src); {
		if src[i] != 0x1b {
			out = append(out, src[i])
			i++
			continue
		}
		i++ // Skip ESC.
		if i >= len(src) {
			break
		}
		switch src[i] {
		case '[': // CSI
			i++
			for i < len(src) {
				if src[i] >= 0x40 && src[i] <= 0x7e {
					i++
					break
				}
				i++
			}
		case ']': // OSC ... BEL or ST (ESC \)
			i++
			for i < len(src) {
				if src[i] == 0x07 {
					i++
					break
				}
				if src[i] == 0x1b && i+1 < len(src) && src[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default:
			// Single-byte escape family.
			i++
		}
	}
	return string(out)
}

func (s *WallService) normalizeEventLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultWallEventLimit
	case limit > maxWallEventLimit:
		return maxWallEventLimit
	default:
		return limit
	}
}

func (s *WallService) pruneEventsLocked(username string, now time.Time) {
	if username == "" {
		return
	}
	events := s.eventsByUser[username]
	if len(events) == 0 {
		return
	}
	cutoff := now.Add(-s.eventTTL)
	prune := 0
	for prune < len(events) {
		if events[prune].CreatedAt.After(cutoff) || events[prune].CreatedAt.Equal(cutoff) {
			break
		}
		prune++
	}
	if prune <= 0 {
		return
	}
	if prune >= len(events) {
		delete(s.eventsByUser, username)
		return
	}
	next := make([]wallEvent, len(events)-prune)
	copy(next, events[prune:])
	s.eventsByUser[username] = next
}

func (s *WallService) recordEvent(username, sourceSessionID, sender, message string, timeoutSeconds uint32, kind protocolpb.WallKind, now time.Time) uint64 {
	if strings.TrimSpace(username) == "" {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneEventsLocked(username, now)
	s.nextEventID++
	event := wallEvent{
		ID:             s.nextEventID,
		Username:       username,
		SessionID:      strings.TrimSpace(sourceSessionID),
		Sender:         strings.TrimSpace(sender),
		Message:        strings.TrimSpace(message),
		TimeoutSeconds: timeoutSeconds,
		Kind:           kind,
		CreatedAt:      now,
	}
	s.eventsByUser[username] = append(s.eventsByUser[username], event)
	return event.ID
}

func (s *WallService) listEvents(username string, sinceID uint64, limit int, now time.Time) ([]wallEvent, uint64, bool) {
	if strings.TrimSpace(username) == "" {
		return []wallEvent{}, sinceID, false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	limit = s.normalizeEventLimit(limit)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneEventsLocked(username, now)
	events := s.eventsByUser[username]
	if len(events) == 0 {
		return []wallEvent{}, sinceID, false
	}
	out := make([]wallEvent, 0, limit)
	nextID := sinceID
	more := false
	for _, event := range events {
		if event.ID <= sinceID {
			continue
		}
		if !s.eventVisibleLocked(event) {
			nextID = event.ID
			continue
		}
		if len(out) < limit {
			out = append(out, event)
			nextID = event.ID
			continue
		}
		more = true
		break
	}
	return out, nextID, more
}

func (s *WallService) eventVisibleLocked(event wallEvent) bool {
	if event.Kind != protocolpb.WallKind_WALL_KIND_INACTIVITY {
		return true
	}
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" || s.store == nil {
		return true
	}
	session, ok := s.store.GetSession(sessionID)
	if !ok {
		return false
	}
	return session.Status == "active"
}

func (s *WallService) timeoutSeconds() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return wallTimeoutSeconds(s.timeout)
}

func wallTimeoutSeconds(timeout time.Duration) uint32 {
	if timeout <= 0 {
		timeout = defaultWallTimeout
	}
	seconds := uint32(timeout.Round(time.Second).Seconds())
	if seconds == 0 {
		return 1
	}
	return seconds
}

func monitorKey(username, sessionID string) string {
	return username + "\x00" + sessionID
}

func (s *WallService) monitorLevelAfterLocked(level int) (int, time.Duration) {
	levels := s.inactiveAfterLevels
	if len(levels) == 0 {
		levels = defaultWallInactiveAfterLevels
	}
	if level < 0 {
		level = 0
	}
	if level >= len(levels) {
		level = len(levels) - 1
	}
	return level, levels[level]
}

func (s *WallService) enableMonitorLocked(key, username, sessionID, sender string, level int, now time.Time) (bool, time.Duration) {
	monitor := s.monitors[key]
	if monitor == nil {
		monitor = &inactivityMonitor{
			username:     username,
			sessionID:    sessionID,
			lastActivity: now,
		}
		s.monitors[key] = monitor
		if s.bySession[sessionID] == nil {
			s.bySession[sessionID] = make(map[string]struct{})
		}
		s.bySession[sessionID][key] = struct{}{}
	}
	level, after := s.monitorLevelAfterLocked(level)
	monitor.sender = sender
	monitor.lastActivity = now
	monitor.level = level
	monitor.inactiveAfter = after
	s.scheduleMonitorLocked(key, monitor, now)
	return true, after
}

func (s *WallService) setInactivity(username, sessionID, sender string, enabled bool, now time.Time) (bool, time.Duration) {
	if username == "" || sessionID == "" {
		return false, 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	key := monitorKey(username, sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !enabled {
		s.removeMonitorLocked(key)
		return false, 0
	}
	return s.enableMonitorLocked(key, username, sessionID, sender, 0, now)
}

func (s *WallService) toggleInactivity(username, sessionID, sender string, now time.Time) (bool, time.Duration) {
	if username == "" || sessionID == "" {
		return false, 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	key := monitorKey(username, sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if monitor, exists := s.monitors[key]; exists {
		nextLevel := monitor.level + 1
		levels := s.inactiveAfterLevels
		if len(levels) == 0 {
			levels = defaultWallInactiveAfterLevels
		}
		if nextLevel >= len(levels) {
			s.removeMonitorLocked(key)
			return false, 0
		}
		monitor.sender = sender
		monitor.level = nextLevel
		monitor.inactiveAfter = levels[nextLevel]
		monitor.lastActivity = now
		s.scheduleMonitorLocked(key, monitor, now)
		return true, monitor.inactiveAfter
	}
	return s.enableMonitorLocked(key, username, sessionID, sender, 0, now)
}

func (s *WallService) removeMonitorLocked(key string) {
	monitor := s.monitors[key]
	if monitor == nil {
		return
	}
	if monitor.timer != nil {
		monitor.timer.Stop()
		monitor.timer = nil
	}
	delete(s.monitors, key)
	if keys := s.bySession[monitor.sessionID]; keys != nil {
		delete(keys, key)
		if len(keys) == 0 {
			delete(s.bySession, monitor.sessionID)
		}
	}
}

func (s *WallService) markActivity(sessionID string, now time.Time) {
	if sessionID == "" {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.bySession[sessionID]
	for key := range keys {
		monitor := s.monitors[key]
		if monitor == nil {
			continue
		}
		monitor.lastActivity = now
		s.scheduleMonitorLocked(key, monitor, now)
	}
}

func (s *WallService) scheduleMonitorLocked(key string, monitor *inactivityMonitor, now time.Time) {
	if monitor == nil {
		return
	}
	if monitor.timer != nil {
		monitor.timer.Stop()
	}
	after := monitor.inactiveAfter
	if after <= 0 {
		_, after = s.monitorLevelAfterLocked(monitor.level)
		monitor.inactiveAfter = after
	}
	delay := after - now.Sub(monitor.lastActivity)
	if delay < time.Second {
		delay = time.Second
	}
	monitor.timer = time.AfterFunc(delay, func() {
		s.fireMonitor(key)
	})
}

func (s *WallService) fireMonitor(key string) {
	now := time.Now().UTC()
	var sessionID string
	var username string
	var sender string
	s.mu.Lock()
	monitor := s.monitors[key]
	if monitor == nil {
		s.mu.Unlock()
		return
	}
	monitor.timer = nil
	idle := now.Sub(monitor.lastActivity)
	inactiveAfter := monitor.inactiveAfter
	if inactiveAfter <= 0 {
		_, inactiveAfter = s.monitorLevelAfterLocked(monitor.level)
		monitor.inactiveAfter = inactiveAfter
	}
	if idle < inactiveAfter {
		s.scheduleMonitorLocked(key, monitor, now)
		s.mu.Unlock()
		return
	}
	sessionID = monitor.sessionID
	username = monitor.username
	sender = monitor.sender
	s.mu.Unlock()

	message := fmt.Sprintf("%s inactive", s.sessionLabel(sessionID))
	sent, err := s.sendUserWallForSession(username, sender, message, sessionID, protocolpb.WallKind_WALL_KIND_INACTIVITY, time.Time{})
	if err != nil {
		s.logger.Warn("relay.wall.inactive.failed", "session", sessionID, "err", err)
		return
	}
	s.logger.Info("relay.wall.inactive.sent", "session", sessionID, "sessions", sent)
}

func (s *WallService) sessionLabel(sessionID string) string {
	if s.store == nil || sessionID == "" {
		return sessionID
	}
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	session, ok := s.store.Sessions[sessionID]
	if !ok {
		return sessionID
	}
	if session.Name != "" {
		return session.Name
	}
	return session.ID
}
