package mvu

import (
	"strings"
	"testing"
	"time"
)

func TestReconnectDetail(t *testing.T) {
	now := time.Unix(1000, 0)
	if got := ReconnectDetail(now, time.Time{}); got != "reconnecting in 1s" {
		t.Fatalf("ReconnectDetail(zero) = %q", got)
	}
	if got := ReconnectDetail(now, now.Add(3*time.Second)); got != "reconnecting in 3s" {
		t.Fatalf("ReconnectDetail(+3s) = %q", got)
	}
	if got := ReconnectDetail(now, now.Add(-time.Second)); got != "reconnecting in 1s" {
		t.Fatalf("ReconnectDetail(-1s) = %q", got)
	}
}

func TestWaitingForSessionsDetail(t *testing.T) {
	now := time.Unix(2000, 0)
	if got := WaitingForSessionsDetail(now, now.Add(4*time.Second)); got != "waiting for sessions in 4s" {
		t.Fatalf("WaitingForSessionsDetail(+4s) = %q", got)
	}
	if got := WaitingForSessionsDetail(now, now.Add(-time.Second)); got != "waiting for sessions in 0s" {
		t.Fatalf("WaitingForSessionsDetail(-1s) = %q", got)
	}
}

func TestDisconnectedOverlayActionWaitingHidesConnection(t *testing.T) {
	r := NewRuntime()
	now := time.Unix(3000, 0)
	r.ApplyAction(StatusAction{Input: StatusInput{Kind: StatusConnected, Message: "connected", Duration: 2 * time.Second}})

	res := r.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		WaitingForSessions: true,
		WaitUntil:          now.Add(5 * time.Second),
		Now:                now,
	}})
	if !res.Changed {
		t.Fatalf("expected changed result")
	}
	if !res.Overlay.DisconnectVisible {
		t.Fatalf("expected disconnect overlay visible")
	}
	if res.Overlay.DisconnectTitle != "Waiting for sessions" {
		t.Fatalf("disconnect title = %q", res.Overlay.DisconnectTitle)
	}
	if !strings.Contains(res.Overlay.DisconnectDetail, "5s") {
		t.Fatalf("disconnect detail = %q", res.Overlay.DisconnectDetail)
	}
	if res.Overlay.ConnectionMessage != "" {
		t.Fatalf("expected connection message cleared, got %q", res.Overlay.ConnectionMessage)
	}
}

func TestDisconnectedOverlayActionDisconnectedShowsReconnectDetail(t *testing.T) {
	r := NewRuntime()
	now := time.Unix(4000, 0)
	res := r.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		Connected:     false,
		ConnectedOnce: true,
		ReconnectAt:   now.Add(2 * time.Second),
		Now:           now,
	}})
	if !res.Changed {
		t.Fatalf("expected changed result")
	}
	if !res.Overlay.DisconnectVisible {
		t.Fatalf("expected disconnect overlay visible")
	}
	if res.Overlay.DisconnectTitle != "Not connected" {
		t.Fatalf("disconnect title = %q", res.Overlay.DisconnectTitle)
	}
	if res.Overlay.DisconnectDetail != "reconnecting in 2s" {
		t.Fatalf("disconnect detail = %q", res.Overlay.DisconnectDetail)
	}
}

func TestDisconnectedOverlayActionPreservesConnectionLostBanner(t *testing.T) {
	r := NewRuntime()
	now := time.Unix(4500, 0)
	r.ApplyAction(StatusAction{Input: StatusInput{Kind: StatusConnectionLost, Message: "connection lost to relay, reconnecting"}})

	res := r.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		Connected:     false,
		ConnectedOnce: true,
		ReconnectAt:   now.Add(2 * time.Second),
		Now:           now,
	}})
	if !res.Overlay.DisconnectVisible {
		t.Fatalf("expected disconnect overlay visible")
	}
	if res.Overlay.ConnectionMessage == "" {
		t.Fatalf("expected connection-lost banner preserved")
	}
}

func TestDisconnectedOverlayActionConnectedClearsDisconnect(t *testing.T) {
	r := NewRuntime()
	now := time.Unix(5000, 0)
	r.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		Connected:     false,
		ConnectedOnce: true,
		ReconnectAt:   now.Add(time.Second),
		Now:           now,
	}})

	res := r.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		Connected: true,
		Now:       now,
	}})
	if !res.Changed {
		t.Fatalf("expected changed result")
	}
	if res.Overlay.DisconnectVisible {
		t.Fatalf("expected disconnect overlay hidden")
	}
}

func TestDisconnectedOverlayActionSkipsFirstDisconnectWhenNeverConnected(t *testing.T) {
	r := NewRuntime()
	now := time.Unix(6000, 0)
	res := r.ApplyAction(DisconnectedOverlayAction{Input: DisconnectedOverlayInput{
		Connected:     false,
		ConnectedOnce: false,
		ReconnectAt:   now.Add(2 * time.Second),
		Now:           now,
	}})
	if res.Changed {
		t.Fatalf("expected no change before first successful connect")
	}
	if res.Overlay.DisconnectVisible {
		t.Fatalf("expected disconnect overlay hidden")
	}
}

func TestAttachConnectivityActionDisconnectedSetsBannerAndOverlay(t *testing.T) {
	r := NewRuntime()
	now := time.Unix(7000, 0)
	res := r.ApplyAction(AttachConnectivityAction{Input: AttachConnectivityInput{
		Connected:     false,
		ConnectedOnce: true,
		ReconnectAt:   now.Add(2 * time.Second),
		Endpoint:      "https://localhost:1234/v1",
		Now:           now,
	}})
	if !res.Changed {
		t.Fatalf("expected changed result")
	}
	if !res.Overlay.DisconnectVisible {
		t.Fatalf("expected disconnect overlay visible")
	}
	state := r.State()
	if !strings.Contains(state.ConnectionMessage, "connection lost to https://localhost:1234/v1") {
		t.Fatalf("expected connection-lost banner, got %q", state.ConnectionMessage)
	}
}

func TestAttachConnectivityActionConnectedClearsLostBanner(t *testing.T) {
	r := NewRuntime()
	now := time.Unix(7100, 0)
	r.ApplyAction(AttachConnectivityAction{Input: AttachConnectivityInput{
		Connected:     false,
		ConnectedOnce: true,
		ReconnectAt:   now.Add(time.Second),
		Endpoint:      "https://localhost:1234/v1",
		Now:           now,
	}})

	res := r.ApplyAction(AttachConnectivityAction{Input: AttachConnectivityInput{
		Connected: true,
		Now:       now.Add(time.Second),
	}})
	if !res.Changed {
		t.Fatalf("expected changed result")
	}
	state := r.State()
	if state.ConnectionMessage != "" {
		t.Fatalf("expected connection banner cleared, got %q", state.ConnectionMessage)
	}
	if state.DisconnectVisible {
		t.Fatalf("expected disconnect overlay cleared")
	}
}
