package mvu

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// DisconnectedOverlayInput describes connectivity state for disconnect overlays.
type DisconnectedOverlayInput struct {
	Connected          bool
	ConnectedOnce      bool
	ReconnectAt        time.Time
	WaitingForSessions bool
	WaitUntil          time.Time
	Now                time.Time
}

// DisconnectedOverlayResult reports the resulting overlay state.
type DisconnectedOverlayResult struct {
	Changed           bool
	DisconnectVisible bool
	DisconnectTitle   string
	DisconnectDetail  string
	ConnectionMessage string
}

// AttachConnectivityInput describes attach connectivity state for MVU sync.
type AttachConnectivityInput struct {
	Connected          bool
	ConnectedOnce      bool
	ReconnectAt        time.Time
	WaitingForSessions bool
	WaitUntil          time.Time
	Endpoint           string
	Now                time.Time
}

// AttachConnectivityResult reports synchronized attach connectivity UI state.
type AttachConnectivityResult struct {
	Changed bool
	Overlay DisconnectedOverlayResult
}

// ReconnectDetail returns a reconnect countdown detail label.
func ReconnectDetail(now, reconnectAt time.Time) string {
	if reconnectAt.IsZero() {
		return "reconnecting in 1s"
	}
	remaining := int(math.Ceil(reconnectAt.Sub(now).Seconds()))
	if remaining < 1 {
		remaining = 1
	}
	return fmt.Sprintf("reconnecting in %ds", remaining)
}

// WaitingForSessionsDetail returns a waiting countdown detail label.
func WaitingForSessionsDetail(now, until time.Time) string {
	remaining := int(math.Ceil(until.Sub(now).Seconds()))
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Sprintf("waiting for sessions in %ds", remaining)
}

func (r *Runtime) applyDisconnectedOverlay(in DisconnectedOverlayInput) DisconnectedOverlayResult {
	if r == nil {
		return DisconnectedOverlayResult{}
	}
	now := in.Now
	if now.IsZero() {
		now = r.now()
	}
	before := r.State()
	switch {
	case in.WaitingForSessions:
		if before.ConnectionMessage != "" {
			r.hideConnection()
		}
		title := "Waiting for sessions"
		detail := WaitingForSessionsDetail(now, in.WaitUntil)
		if !before.DisconnectVisible || before.DisconnectTitle != title || before.DisconnectDetail != detail {
			r.showDisconnected(title, detail)
		}
	case in.Connected:
		if strings.Contains(strings.ToLower(before.ConnectionMessage), "connection lost") {
			r.hideConnection()
		}
		if before.DisconnectVisible {
			r.hideDisconnected()
		}
	default:
		if !in.ConnectedOnce && !before.DisconnectVisible {
			break
		}
		if before.ConnectionMessage != "" && !strings.Contains(strings.ToLower(before.ConnectionMessage), "connection lost") {
			r.hideConnection()
		}
		title := "Not connected"
		detail := ReconnectDetail(now, in.ReconnectAt)
		if !before.DisconnectVisible || before.DisconnectTitle != title || before.DisconnectDetail != detail {
			r.showDisconnected(title, detail)
		}
	}
	after := r.State()
	changed := before.DisconnectVisible != after.DisconnectVisible ||
		before.DisconnectTitle != after.DisconnectTitle ||
		before.DisconnectDetail != after.DisconnectDetail ||
		before.ConnectionMessage != after.ConnectionMessage
	return DisconnectedOverlayResult{
		Changed:           changed,
		DisconnectVisible: after.DisconnectVisible && after.DisconnectTitle != "",
		DisconnectTitle:   after.DisconnectTitle,
		DisconnectDetail:  after.DisconnectDetail,
		ConnectionMessage: after.ConnectionMessage,
	}
}

func (r *Runtime) applyAttachConnectivity(in AttachConnectivityInput) AttachConnectivityResult {
	if r == nil {
		return AttachConnectivityResult{}
	}
	now := in.Now
	if now.IsZero() {
		now = r.now()
	}
	changed := false
	if in.Connected {
		state := r.State()
		if strings.Contains(strings.ToLower(state.ConnectionMessage), "connection lost") {
			r.hideConnection()
			changed = true
		}
		overlay := r.applyDisconnectedOverlay(DisconnectedOverlayInput{
			Connected: true,
			Now:       now,
		})
		return AttachConnectivityResult{Changed: changed || overlay.Changed, Overlay: overlay}
	}
	if in.WaitingForSessions {
		overlay := r.applyDisconnectedOverlay(DisconnectedOverlayInput{
			WaitingForSessions: true,
			WaitUntil:          in.WaitUntil,
			Now:                now,
		})
		return AttachConnectivityResult{Changed: overlay.Changed, Overlay: overlay}
	}
	state := r.State()
	msg := ConnectionLostMessage(in.Endpoint)
	if state.ConnectionMessage != msg || state.ConnectionStyle != BannerRed || !state.ConnectionExpiresAt.IsZero() {
		r.showConnectionLost(msg)
		changed = true
	}
	overlay := r.applyDisconnectedOverlay(DisconnectedOverlayInput{
		Connected:     false,
		ConnectedOnce: in.ConnectedOnce,
		ReconnectAt:   in.ReconnectAt,
		Now:           now,
	})
	return AttachConnectivityResult{Changed: changed || overlay.Changed, Overlay: overlay}
}
