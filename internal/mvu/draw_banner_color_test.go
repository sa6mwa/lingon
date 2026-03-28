package mvu

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/theme"
)

func TestDrawBannerAtRowGreenUsesBlackTextOnGreenBackground(t *testing.T) {
	t.Parallel()
	ui := theme.TUI("default")
	var out bytes.Buffer
	DrawBannerAtRow(&out, 80, 1, "connected", BannerGreen, ui)
	raw := out.String()
	if !strings.Contains(raw, "\x1b[38;2;0;0;0;42mconnected") {
		t.Fatalf("expected black-on-green sequence in banner output, got %q", raw)
	}
	if strings.Contains(raw, "\x1b[97;42mconnected") {
		t.Fatalf("unexpected white-on-green sequence in banner output, got %q", raw)
	}
}

func TestDrawIndicatorAtRowGreenUsesBlackTextOnGreenBackground(t *testing.T) {
	t.Parallel()
	ui := theme.TUI("default")
	var out bytes.Buffer
	DrawIndicatorAtRow(&out, 80, 1, "wall inactivity 10s", BannerGreen, ui)
	raw := out.String()
	if !strings.Contains(raw, "\x1b[38;2;0;0;0;42mwall inactivity 10s") {
		t.Fatalf("expected black-on-green sequence in indicator output, got %q", raw)
	}
	if strings.Contains(raw, "\x1b[97;42mwall inactivity 10s") {
		t.Fatalf("unexpected white-on-green sequence in indicator output, got %q", raw)
	}
}
