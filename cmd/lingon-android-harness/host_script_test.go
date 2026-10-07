package main

import (
	"os/exec"
	"testing"
)

func TestHostScriptShellOverridePreservesInitialLines(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines int
		want  string
	}{
		{"quiet populated fixture", 3, "LINE_quiet-fixture 000\r\nLINE_quiet-fixture 001\r\nLINE_quiet-fixture 002\r\n"},
		{"empty fixture", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// /bin/true exits without starting an interactive terminal. The generated
			// wrapper is non-interactive and all stdio is captured or disconnected.
			path, err := writeHostScript(t.TempDir(), "quiet-fixture", "unused", "/bin/true", tc.lines)
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command("/bin/sh", path).Output()
			if err != nil {
				t.Fatalf("fixture shell: %v", err)
			}
			if string(output) != tc.want {
				t.Fatalf("initial payload %q, want %q", output, tc.want)
			}
		})
	}
}
