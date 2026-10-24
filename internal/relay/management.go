package relay

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/sessionmanage"
)

type managementWaiter struct {
	hostID    string
	sessionID string
	reply     chan *protocolpb.SessionManagement
}

func (s *HTTPServer) handleSessionManagement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if wantsShareSession(r) {
		writeJSON(w, http.StatusForbidden, sessionmanage.Result{Error: "session management requires the owner account"})
		return
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, sessionmanage.Result{Error: "application/json required"})
		return
	}
	access, err := s.accessTokenFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, sessionmanage.Result{Error: "session management requires the owner account"})
		return
	}
	var req sessionmanage.Request
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, sessionmanage.Result{Error: "invalid request"})
		return
	}
	if err := sessionmanage.Validate(req); err != nil {
		writeJSON(w, http.StatusBadRequest, sessionmanage.Result{Error: err.Error()})
		return
	}
	session, ok := s.Store.GetSession(req.SessionID)
	if !ok || session.Username != access.Username {
		writeJSON(w, http.StatusForbidden, sessionmanage.Result{Error: "session belongs to another owner or is unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	result, err := s.Hub.manage(ctx, req)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, sessionmanage.Result{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Hub) manage(ctx context.Context, req sessionmanage.Request) (sessionmanage.Result, error) {
	h.mu.Lock()
	state := h.sessions[req.SessionID]
	if state == nil || state.host == nil {
		h.mu.Unlock()
		return sessionmanage.Result{}, fmt.Errorf("no host connected; connect the session's machine first")
	}
	if len(h.management) >= 256 {
		h.mu.Unlock()
		return sessionmanage.Result{}, fmt.Errorf("session management busy")
	}
	host := state.host
	key := newConnID()
	waiter := managementWaiter{host.ID(), req.SessionID, make(chan *protocolpb.SessionManagement, 1)}
	h.management[key] = waiter
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.management, key); h.mu.Unlock() }()
	frame := &protocolpb.Frame{SessionId: req.SessionID, Payload: &protocolpb.Frame_Management{Management: &protocolpb.SessionManagement{CorrelationId: key, RequestId: req.RequestID, Kind: req.Kind, Name: req.Name}}}
	if err := host.Send(ctx, frame); err != nil {
		return sessionmanage.Result{}, err
	}
	select {
	case reply := <-waiter.reply:
		return sessionmanage.Result{SessionID: reply.ResultSessionId, Name: reply.Name, Error: reply.Error}, nil
	case <-ctx.Done():
		return sessionmanage.Result{}, fmt.Errorf("session operation outcome unknown; refresh sessions before retrying: %w", ctx.Err())
	}
}

func (s *HTTPServer) handleManagementResult(_ context.Context, host connection, reply *protocolpb.SessionManagement) {
	if !reply.Completed {
		return
	}
	s.Hub.mu.Lock()
	state := s.Hub.sessions[host.SessionID()]
	valid := state != nil && state.host != nil && state.host.ID() == host.ID()
	waiter, waiting := s.Hub.management[reply.CorrelationId]
	var renamedSession Session
	var renamed bool
	if valid && reply.Kind == "rename" && reply.Error == "" && reply.ResultSessionId == host.SessionID() && sessionmanage.ValidateName(reply.Name) == nil && s.Store != nil {
		renamedSession, renamed = s.Store.GetSession(host.SessionID())
		// Hold host identity stable through the metadata mutation, not disk I/O.
		if renamed && reply.CorrelationId == "" {
			renamedSession, renamed = s.Store.SetSessionName(host.SessionID(), reply.Name)
		}
	}
	s.Hub.mu.Unlock()
	if !valid {
		return
	}
	// A host-local rename has no correlation ID; its metadata still updates every client.
	// Only uncorrelated runtime announcements change names; retried acknowledgements
	// may describe older completed renames. Both responses confirm persistence.
	if renamed {
		if err := s.persist(); err != nil {
			reply.Error = "failed to persist renamed session"
		} else {
			s.notifySessions(renamedSession.Username)
		}
	}
	if waiting && waiter.hostID == host.ID() && waiter.sessionID == host.SessionID() {
		select {
		case waiter.reply <- reply:
		default:
		}
	}
}

func (h *Hub) failManagementLocked(hostID string) {
	for _, waiter := range h.management {
		if waiter.hostID == hostID {
			select {
			case waiter.reply <- &protocolpb.SessionManagement{Error: "host disconnected; operation outcome unknown, refresh sessions before retrying"}:
			default:
			}
		}
	}
}
