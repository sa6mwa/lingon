package session

import (
	"context"
	"strings"

	"pkt.systems/lingon/internal/protocolpb"
)

// HandleSessionInput writes raw input to the target local PTY session.
func (r *Runner) HandleSessionInput(sessionID string, data []byte) {
	if r == nil || len(data) == 0 {
		return
	}
	targetID := r.resolveLocalSessionID(sessionID)
	if targetID == "" {
		return
	}
	local := r.localSession(targetID)
	if local == nil {
		return
	}
	r.handleRemoteInput(local, data, r.logger != nil)
}

func (r *Runner) resolveLocalSessionID(sessionID string) string {
	targetID := strings.TrimSpace(sessionID)
	if targetID == "" {
		activeID, activeLocal := r.activeSession()
		if activeLocal {
			targetID = strings.TrimSpace(activeID)
		}
	}
	if targetID == "" {
		targetID = r.firstLocalID()
	}
	return targetID
}

func (r *Runner) sendCtrlDSession(sessionID string) {
	if r == nil {
		return
	}
	targetID := r.resolveLocalSessionID(sessionID)
	if targetID == "" {
		return
	}
	local := r.localSession(targetID)
	if local == nil {
		return
	}
	if err := local.sendRemoteEOF(); err != nil && r.logger != nil {
		r.logger.Debug("session.local.ctrl_d.failed", "err", err, "session", local.ID())
	}
}

// SendCtrlDActive delivers an explicit EOF to the active local PTY session.
func (r *Runner) SendCtrlDActive() {
	r.sendCtrlDSession("")
}

// SessionOffline reports the offline flag for a local session.
func (r *Runner) SessionOffline(sessionID string) (bool, bool) {
	if r == nil {
		return false, false
	}
	targetID := r.resolveLocalSessionID(sessionID)
	if targetID == "" {
		return false, false
	}
	local := r.localSession(targetID)
	if local == nil {
		return false, false
	}
	return local.Offline(), true
}

// HandleSessionCommand applies a protocol command to the target local session.
func (r *Runner) HandleSessionCommand(ctx context.Context, sessionID string, kind protocolpb.CommandKind) {
	if r == nil {
		return
	}
	targetID := r.resolveLocalSessionID(sessionID)
	if targetID == "" {
		return
	}
	stdout := r.stdout()
	switch kind {
	case protocolpb.CommandKind_COMMAND_KIND_SEND_EOF:
		r.sendCtrlDSession(targetID)
	case protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE:
		r.toggleOffline(targetID, stdout)
	case protocolpb.CommandKind_COMMAND_KIND_TOGGLE_RESPAWN:
		r.toggleRespawn(targetID, stdout)
	case protocolpb.CommandKind_COMMAND_KIND_CYCLE_WALL_INACTIVITY:
		commandCtx := ctx
		if commandCtx == nil {
			commandCtx = context.Background()
		}
		r.toggleWallInactivity(commandCtx, targetID, r.tokenRefresher, stdout)
	}
}
