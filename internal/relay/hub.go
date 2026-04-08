package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"syscall"
	"time"

	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

// Role identifies a connection role.
type Role string

// Connection roles for hub routing.
const (
	RoleHost   Role = "host"
	RoleClient Role = "client"
)

type connection interface {
	ID() string
	Role() Role
	Scope() ShareScope
	SessionID() string
	Send(ctx context.Context, frame *protocolpb.Frame) error
	Close(ctx context.Context, reason string) error
}

// Hub routes messages between host and clients.
type Hub struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
	logger   pslog.Logger
}

var errHostSessionClosed = errors.New("host session closed")
var errStaleHostConnection = errors.New("stale host connection")

type sessionState struct {
	id            string
	host          connection
	clients       map[string]connection
	clientIDs     map[string]string
	seenClientIDs map[string]struct{}
	controller    string
	cols          int
	rows          int
	seq           uint64
	history       []*protocolpb.Frame
	historyBytes  int
	replayMu      sync.RWMutex
}

const maxReplayHistoryBytes = 4 * 1024 * 1024

// NewHub constructs a Hub.
func NewHub(logger pslog.Logger) *Hub {
	if logger == nil {
		logger = logging.Default()
	}
	return &Hub{
		sessions: make(map[string]*sessionState),
		logger:   logger,
	}
}

// RegisterHost registers a host connection for a session.
func (h *Hub) RegisterHost(conn connection, sessionID string, cols, rows int) error {
	_ = h.registerHost(conn, sessionID, cols, rows)
	return nil
}

// registerHost registers a host connection for a session.
// If another host is already present for the session, it is replaced and returned.
func (h *Hub) registerHost(conn connection, sessionID string, cols, rows int) connection {
	h.mu.Lock()
	defer h.mu.Unlock()

	state := h.session(sessionID)
	var replaced connection
	if state.host != nil && state.host.ID() != conn.ID() {
		replaced = state.host
		h.logger.Info("relay.hub.host.takeover", "session", sessionID, "old_conn", state.host.ID(), "new_conn", conn.ID())
	}
	state.host = conn
	state.cols = cols
	state.rows = rows
	h.logger.Debug("relay.hub.host.register", "session", sessionID, "conn", conn.ID(), "cols", cols, "rows", rows)
	return replaced
}

// RegisterClient registers a client for a session.
func (h *Hub) RegisterClient(conn connection, sessionID, clientID string, wantsControl bool) (bool, string, int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	state := h.session(sessionID)
	state.clients[conn.ID()] = conn
	if clientID == "" {
		clientID = conn.ID()
	}
	state.clientIDs[conn.ID()] = clientID
	state.seenClientIDs[clientID] = struct{}{}
	granted := false
	if wantsControl && conn.Scope() == ShareScopeControl {
		state.controller = conn.ID()
		granted = true
	}
	holderID := state.clientIDs[state.controller]
	if holderID == "" {
		holderID = state.controller
	}
	h.logger.Debug(
		"relay.hub.client.register",
		"session", sessionID,
		"conn", conn.ID(),
		"client", clientID,
		"wants_control", wantsControl,
		"granted", granted,
	)
	return granted, holderID, state.cols, state.rows
}

// HasClientID reports whether a client ID is already registered for a session.
func (h *Hub) HasClientID(sessionID, clientID string) bool {
	if clientID == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.sessions[sessionID]
	if state == nil {
		return false
	}
	_, ok := state.seenClientIDs[clientID]
	return ok
}

// ClientCount reports how many clients are registered for a session.
func (h *Hub) ClientCount(sessionID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.sessions[sessionID]
	if state == nil {
		return 0
	}
	return len(state.clients)
}

// Unregister removes a connection from the hub.
func (h *Hub) Unregister(conn connection) {
	var notify []connection
	var notifyReason string

	h.mu.Lock()

	state := h.sessions[conn.SessionID()]
	if state == nil {
		h.mu.Unlock()
		return
	}

	if conn.Role() == RoleHost {
		if state.host != nil && state.host.ID() == conn.ID() {
			state.host = nil
			state.controller = ""
			h.logger.Info("relay.host.disconnect.done", "session", conn.SessionID())
			notifyReason = "host disconnected"
			notify = make([]connection, 0, len(state.clients))
			for _, client := range state.clients {
				notify = append(notify, client)
			}
		}
	} else {
		delete(state.clients, conn.ID())
		delete(state.clientIDs, conn.ID())
		h.logger.Debug("relay.hub.client.unregister", "session", conn.SessionID(), "conn", conn.ID())
	}
	if state.controller == conn.ID() {
		state.controller = ""
	}

	h.mu.Unlock()

	if len(notify) == 0 {
		return
	}
	frame := frameError(notifyReason)
	for _, client := range notify {
		if ws, ok := client.(*wsConn); ok {
			_ = ws.SendImmediate(context.Background(), frame)
		} else {
			_ = client.Send(context.Background(), frame)
		}
		_ = client.Close(context.Background(), notifyReason)
	}
}

// CloseAll disconnects all sessions and clients.
func (h *Hub) CloseAll(reason string) {
	h.mu.Lock()
	var conns []connection
	for _, state := range h.sessions {
		if state.host != nil {
			conns = append(conns, state.host)
		}
		for _, client := range state.clients {
			conns = append(conns, client)
		}
	}
	h.sessions = make(map[string]*sessionState)
	h.mu.Unlock()

	if reason == "" {
		reason = "shutdown"
	}
	for _, conn := range conns {
		_ = conn.Close(context.Background(), reason)
	}
}

// HandleHostFrame routes host frames to clients.
func (h *Hub) HandleHostFrame(ctx context.Context, conn connection, frame *protocolpb.Frame) error {
	h.mu.Lock()
	state := h.sessions[conn.SessionID()]
	if state == nil {
		h.mu.Unlock()
		return fmt.Errorf("unknown session")
	}
	if state.host == nil || state.host.ID() != conn.ID() {
		h.mu.Unlock()
		return errStaleHostConnection
	}
	if frame.GetSessionClosed() != nil {
		h.mu.Unlock()
		return errHostSessionClosed
	}
	if ctrl := frame.GetControl(); ctrl != nil {
		state.controller = ctrl.HolderClientId
	}
	state.seq++
	frame.Seq = state.seq
	h.recordFrameLocked(state, frame)
	clients := make([]connection, 0, len(state.clients))
	for _, client := range state.clients {
		clients = append(clients, client)
	}
	h.mu.Unlock()

	state.replayMu.RLock()
	defer state.replayMu.RUnlock()
	h.logger.Trace("relay.hub.host.frame", "session", conn.SessionID(), "seq", frame.Seq, "clients", len(clients))

	for _, client := range clients {
		if err := client.Send(ctx, frame); err != nil {
			if isExpectedSendError(err) {
				continue
			}
			h.logger.Debug("relay.client.send.failed", "err", err)
		}
	}
	return nil
}

// BroadcastSessionFrame sends a server-originated frame to session participants.
// Sequence numbering is owned by the hub to keep frame ordering monotonic.
func (h *Hub) BroadcastSessionFrame(ctx context.Context, sessionID string, frame *protocolpb.Frame, includeHost bool) bool {
	if frame == nil || sessionID == "" {
		return false
	}
	h.mu.Lock()
	state := h.sessions[sessionID]
	if state == nil {
		h.mu.Unlock()
		return false
	}
	state.seq++
	frame.SessionId = sessionID
	frame.Seq = state.seq
	h.recordFrameLocked(state, frame)
	host := state.host
	clients := make([]connection, 0, len(state.clients))
	for _, client := range state.clients {
		clients = append(clients, client)
	}
	h.mu.Unlock()

	state.replayMu.RLock()
	defer state.replayMu.RUnlock()
	sent := false
	for _, client := range clients {
		if err := client.Send(ctx, frame); err != nil {
			if isExpectedSendError(err) {
				continue
			}
			h.logger.Debug("relay.client.send.failed", "err", err)
			continue
		}
		sent = true
	}
	if includeHost && host != nil {
		if err := host.Send(ctx, frame); err != nil {
			if !isExpectedSendError(err) {
				h.logger.Debug("relay.host.send.failed", "err", err)
			}
		} else {
			sent = true
		}
	}
	return sent
}

// HasParticipants reports whether a session currently has a host and/or clients connected.
func (h *Hub) HasParticipants(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.sessions[sessionID]
	if state == nil {
		return false
	}
	return state.host != nil || len(state.clients) > 0
}

// HandleClientFrame processes client input/control frames and forwards to host.
func (h *Hub) HandleClientFrame(ctx context.Context, conn connection, frame *protocolpb.Frame) error {
	h.mu.Lock()
	state := h.sessions[conn.SessionID()]
	if state == nil {
		h.mu.Unlock()
		return fmt.Errorf("unknown session")
	}
	if state.host == nil {
		h.mu.Unlock()
		return fmt.Errorf("no host connected")
	}
	if frame.GetHello() != nil {
		lastSeq := frame.GetHello().GetLastSeq()
		if ok, replay := h.replaySinceLocked(state, lastSeq); ok {
			state.replayMu.Lock()
			h.mu.Unlock()
			defer state.replayMu.Unlock()
			for _, replayFrame := range replay {
				if err := conn.Send(ctx, replayFrame); err != nil {
					if !isExpectedSendError(err) {
						h.logger.Debug("relay.client.replay.failed", "err", err)
					}
					return nil
				}
			}
			return nil
		}
		host := state.host
		h.mu.Unlock()
		h.logger.Trace("relay.hub.client.hello", "session", conn.SessionID(), "conn", conn.ID())
		return host.Send(ctx, frame)
	}

	// Control policy: any client input/resize/command can take control if scope allows.
	controlChanged := false
	if conn.Scope() == ShareScopeControl {
		if frame.GetIn() != nil || frame.GetResize() != nil || frame.GetCommand() != nil {
			if state.controller != conn.ID() {
				state.controller = conn.ID()
				controlChanged = true
			}
		}
	}
	host := state.host
	clients := make([]connection, 0, len(state.clients))
	for _, client := range state.clients {
		clients = append(clients, client)
	}
	h.mu.Unlock()

	if conn.Scope() != ShareScopeControl && (frame.GetIn() != nil || frame.GetResize() != nil || frame.GetCommand() != nil) {
		return fmt.Errorf("control not permitted")
	}

	if controlChanged {
		holderID := state.clientIDs[conn.ID()]
		if holderID == "" {
			holderID = conn.ID()
		}
		ctrl := frameControl(frame.SessionId, holderID)
		for _, client := range clients {
			_ = client.Send(ctx, ctrl)
		}
		if host != nil {
			_ = host.Send(ctx, ctrl)
		}
		h.logger.Debug("relay.hub.control.change", "session", conn.SessionID(), "holder", holderID, "clients", len(clients))
	}

	h.logger.Trace("relay.hub.client.frame", "session", conn.SessionID(), "conn", conn.ID())
	return host.Send(ctx, frame)
}

func (h *Hub) session(sessionID string) *sessionState {
	state := h.sessions[sessionID]
	if state != nil {
		return state
	}
	state = &sessionState{
		id:            sessionID,
		clients:       make(map[string]connection),
		clientIDs:     make(map[string]string),
		seenClientIDs: make(map[string]struct{}),
	}
	h.sessions[sessionID] = state
	return state
}

func (h *Hub) recordFrameLocked(state *sessionState, frame *protocolpb.Frame) {
	if state == nil || frame == nil || frame.Seq == 0 {
		return
	}
	clone := proto.Clone(frame)
	next, ok := clone.(*protocolpb.Frame)
	if !ok || next == nil {
		return
	}
	state.history = append(state.history, next)
	state.historyBytes += proto.Size(next)
	for state.historyBytes > maxReplayHistoryBytes && len(state.history) > 1 {
		removed := state.history[0]
		state.history = state.history[1:]
		state.historyBytes -= proto.Size(removed)
	}
	if state.historyBytes < 0 {
		state.historyBytes = 0
	}
}

func (h *Hub) replaySinceLocked(state *sessionState, lastSeq uint64) (bool, []*protocolpb.Frame) {
	if state == nil || lastSeq == 0 || len(state.history) == 0 {
		return false, nil
	}
	if lastSeq > state.seq {
		return false, nil
	}
	firstSeq := state.history[0].Seq
	if lastSeq+1 < firstSeq {
		return false, nil
	}
	idx := sort.Search(len(state.history), func(i int) bool {
		return state.history[i].Seq > lastSeq
	})
	if idx >= len(state.history) {
		return true, nil
	}
	replay := make([]*protocolpb.Frame, 0, len(state.history)-idx)
	for _, frame := range state.history[idx:] {
		clone := proto.Clone(frame)
		next, ok := clone.(*protocolpb.Frame)
		if !ok || next == nil {
			continue
		}
		replay = append(replay, next)
	}
	return true, replay
}

func isExpectedSendError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	status := websocket.CloseStatus(err)
	return status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway
}

// ControllerID returns the current controller client ID for a session.
func (h *Hub) ControllerID(sessionID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.sessions[sessionID]
	if state == nil {
		return ""
	}
	holderID := state.clientIDs[state.controller]
	if holderID == "" {
		holderID = state.controller
	}
	return holderID
}

// BroadcastControl notifies host and clients about the current controller.
func (h *Hub) BroadcastControl(ctx context.Context, sessionID string) {
	h.mu.Lock()
	state := h.sessions[sessionID]
	if state == nil {
		h.mu.Unlock()
		return
	}
	holderID := state.clientIDs[state.controller]
	if holderID == "" {
		holderID = state.controller
	}
	host := state.host
	clients := make([]connection, 0, len(state.clients))
	for _, client := range state.clients {
		clients = append(clients, client)
	}
	h.mu.Unlock()

	if holderID == "" {
		return
	}
	h.logger.Debug("relay.hub.control.broadcast", "session", sessionID, "holder", holderID, "clients", len(clients))
	ctrl := frameControl(sessionID, holderID)
	for _, client := range clients {
		_ = client.Send(ctx, ctrl)
	}
	if host != nil {
		_ = host.Send(ctx, ctrl)
	}
}

// Seq reports the current frame sequence number for a session.
func (h *Hub) Seq(sessionID string) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.sessions[sessionID]
	if state == nil {
		return 0
	}
	return state.seq
}

// NextSeq reserves the next sequence number for a session.
func (h *Hub) NextSeq(sessionID string) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.session(sessionID)
	state.seq++
	return state.seq
}

// SessionState returns current state for tests.
func (h *Hub) SessionState(sessionID string) (string, int, int, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.sessions[sessionID]
	if state == nil {
		return "", 0, 0, 0
	}
	return state.controller, state.cols, state.rows, state.seq
}

// HasHost reports whether a host is registered for the session.
func (h *Hub) HasHost(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.sessions[sessionID]
	if state == nil {
		return false
	}
	return state.host != nil
}

// TouchSession updates session size.
func (h *Hub) TouchSession(sessionID string, cols, rows int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.session(sessionID)
	state.cols = cols
	state.rows = rows
}

// NowUTC provides time for tests.
func NowUTC() time.Time {
	return time.Now().UTC()
}
