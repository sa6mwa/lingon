package control

import "testing"

func TestPrefixCommands(t *testing.T) {
	var p Prefix
	if action, out := p.Feed(CtrlL); action != ActionNone || len(out) != 0 {
		t.Fatalf("expected pending only, got action=%v out=%v", action, out)
	}
	if action, out := p.Feed('Q'); action != ActionQuit || len(out) != 0 {
		t.Fatalf("expected quit action, got action=%v out=%v", action, out)
	}
	var p2 Prefix
	if action, out := p2.Feed(CtrlL); action != ActionNone || len(out) != 0 {
		t.Fatalf("expected pending only, got action=%v out=%v", action, out)
	}
	if action, out := p2.Feed('q'); action != ActionNone || len(out) != 0 {
		t.Fatalf("expected q to be ignored for prefix commands, got action=%v out=%v", action, out)
	}
}

func TestPrefixTabActions(t *testing.T) {
	var p Prefix
	_, _ = p.Feed(CtrlL)
	if action, out := p.Feed('n'); action != ActionNextTab || len(out) != 0 {
		t.Fatalf("expected next tab action, got action=%v out=%v", action, out)
	}
	_, _ = p.Feed(CtrlL)
	if action, out := p.Feed('p'); action != ActionPrevTab || len(out) != 0 {
		t.Fatalf("expected prev tab action, got action=%v out=%v", action, out)
	}
	_, _ = p.Feed(CtrlL)
	if action, out := p.Feed('b'); action != ActionToggleTabBar || len(out) != 0 {
		t.Fatalf("expected toggle tab bar action, got action=%v out=%v", action, out)
	}
	_, _ = p.Feed(CtrlL)
	if action, out := p.Feed('t'); action != ActionNextTheme || len(out) != 0 {
		t.Fatalf("expected next theme action, got action=%v out=%v", action, out)
	}
}

func TestPrefixSessionActions(t *testing.T) {
	var p Prefix
	_, _ = p.Feed(CtrlL)
	if action, out := p.Feed('c'); action != ActionNewPTY || len(out) != 0 {
		t.Fatalf("expected new pty action, got action=%v out=%v", action, out)
	}
	_, _ = p.Feed(CtrlL)
	if action, out := p.Feed('r'); action != ActionToggleRespawn || len(out) != 0 {
		t.Fatalf("expected toggle respawn action, got action=%v out=%v", action, out)
	}
	_, _ = p.Feed(CtrlL)
	if action, out := p.Feed('w'); action != ActionToggleWallInactivity || len(out) != 0 {
		t.Fatalf("expected toggle wall inactivity action, got action=%v out=%v", action, out)
	}
	_, _ = p.Feed(CtrlL)
	if action, out := p.Feed('o'); action != ActionToggleOffline || len(out) != 0 {
		t.Fatalf("expected toggle offline action, got action=%v out=%v", action, out)
	}
}

func TestPrefixPassthroughUnknown(t *testing.T) {
	var p Prefix
	_, _ = p.Feed(CtrlL)
	action, out := p.Feed('x')
	if action != ActionNone {
		t.Fatalf("expected no action, got %v", action)
	}
	if len(out) != 0 {
		t.Fatalf("expected no passthrough output, got %v", out)
	}
}

func TestPrefixLiteralCtrlL(t *testing.T) {
	var p Prefix
	_, _ = p.Feed(CtrlL)
	action, out := p.Feed(CtrlL)
	if action != ActionNone {
		t.Fatalf("expected no action, got %v", action)
	}
	if len(out) != 1 || out[0] != CtrlL {
		t.Fatalf("expected literal ctrl+l, got %v", out)
	}
}

func TestPrefixNonControl(t *testing.T) {
	var p Prefix
	action, out := p.Feed('a')
	if action != ActionNone {
		t.Fatalf("expected no action, got %v", action)
	}
	if len(out) != 1 || out[0] != 'a' {
		t.Fatalf("expected passthrough, got %v", out)
	}
}
