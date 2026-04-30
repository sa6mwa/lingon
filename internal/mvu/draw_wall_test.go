package mvu

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/theme"
)

func TestDrawWallBoxShowsThreeWrappedMessageLines(t *testing.T) {
	var buf bytes.Buffer
	DrawWallBox(
		&buf,
		80,
		24,
		theme.TUI("default"),
		"Broadcast:",
		"Fixed tarball root layout. make release passed, release_tarball_sdk_test passed, and the packaged archives now root under liblockdc-<version>-<target>/ with no ./ entries.",
	)

	out := buf.String()
	if !strings.Contains(out, "Fixed tarball root layout.") {
		t.Fatalf("expected first wrapped segment in wall box: %q", out)
	}
	if !strings.Contains(out, "release_tarball_sdk_test passed,") {
		t.Fatalf("expected second wrapped segment in wall box: %q", out)
	}
	if !strings.Contains(out, "archives now root under") {
		t.Fatalf("expected third wrapped segment in wall box: %q", out)
	}
}
