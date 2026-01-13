package relay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
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

type sessionState struct {
	id         string
	host       connection
	clients    map[string]connection
	clientIDs  map[string]string
	controller string
	cols       int
	rows       int
	seq        uint64
}

// NewHub constructs a Hub.
func NewHub(logger pslog.Logger) *Hub {
	if logger == nil {
		logger = pslog.LoggerFromEnv()
	}
	return &Hub{
		sessions: make(map[string]*sessionState),
		logger:   logger,
	}
}

// RegisterHost registers a host connection for a session.
func (h *Hub) RegisterHost(conn connection, sessionID string, cols, rows int) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	state := h.session(sessionID)
	if state.host != nil {
		_ = state.host.Close(context.Background(), "replaced by new host")
	}
	state.host = conn
	state.cols = cols
	state.rows = rows
	return nil
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
	granted := false
	if wantsControl && conn.Scope() == ShareScopeControl {
		state.controller = conn.ID()
		granted = true
	}
	holderID := state.clientIDs[state.controller]
	if holderID == "" {
		holderID = state.controller
	}
	return granted, holderID, state.cols, state.rows
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
			notifyReason = "host disconnected"
			notify = make([]connection, 0, len(state.clients))
			for _, client := range state.clients {
				notify = append(notify, client)
			}
		}
	} else {
		delete(state.clients, conn.ID())
		delete(state.clientIDs, conn.ID())
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
		_ = client.Send(context.Background(), frame)
		_ = client.Close(context.Background(), notifyReason)
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
	if ctrl := frame.GetControl(); ctrl != nil {
		state.controller = ctrl.HolderClientId
	}
	state.seq++
	frame.Seq = state.seq
	clients := make([]connection, 0, len(state.clients))
	for _, client := range state.clients {
		clients = append(clients, client)
	}
	h.mu.Unlock()

	for _, client := range clients {
		if err := client.Send(ctx, frame); err != nil {
			h.logger.Debug("failed to send to client", "err", err)
		}
	}
	return nil
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
		host := state.host
		h.mu.Unlock()
		return host.Send(ctx, frame)
	}

	// Control policy: any client input/resize can take control if scope allows.
	controlChanged := false
	if conn.Scope() == ShareScopeControl {
		if frame.GetIn() != nil || frame.GetResize() != nil {
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

	if conn.Scope() != ShareScopeControl && (frame.GetIn() != nil || frame.GetResize() != nil) {
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
	}

	return host.Send(ctx, frame)
}

func (h *Hub) session(sessionID string) *sessionState {
	state := h.sessions[sessionID]
	if state != nil {
		return state
	}
	state = &sessionState{
		id:        sessionID,
		clients:   make(map[string]connection),
		clientIDs: make(map[string]string),
	}
	h.sessions[sessionID] = state
	return state
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
	ctrl := frameControl(sessionID, holderID)
	for _, client := range clients {
		_ = client.Send(ctx, ctrl)
	}
	if host != nil {
		_ = host.Send(ctx, ctrl)
	}
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
