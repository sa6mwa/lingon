package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

func TestHeadlessAliasEnabled(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "lingon", want: false},
		{name: "lingonx", want: true},
		{name: "/usr/local/bin/lingonx", want: true},
		{name: "/usr/local/bin/LINGONX", want: true},
	}
	for _, tc := range tests {
		if got := headlessAliasEnabled(tc.name); got != tc.want {
			t.Fatalf("headlessAliasEnabled(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWithEnvReplacesExistingKey(t *testing.T) {
	env := []string{"A=1", "X=old", "B=2"}
	out := withEnv(env, "X", "new")
	found := false
	for _, e := range out {
		if e == "X=new" {
			found = true
		}
		if e == "X=old" {
			t.Fatalf("old key retained: %v", out)
		}
	}
	if !found {
		t.Fatalf("new key not found: %v", out)
	}
}

func TestResolveHeadlessSizeDefaults(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("cols", lingon.DefaultTerminalCols, "")
	cmd.Flags().Int("rows", lingon.DefaultTerminalRows, "")

	cols, rows, err := resolveHeadlessSize(cmd)
	if err != nil {
		t.Fatalf("resolveHeadlessSize: %v", err)
	}
	if cols != lingon.DefaultTerminalCols || rows != lingon.DefaultTerminalRows {
		t.Fatalf("resolveHeadlessSize defaults = %dx%d, want %dx%d", cols, rows, lingon.DefaultTerminalCols, lingon.DefaultTerminalRows)
	}
}

func TestResolveHeadlessSizeFlagOverrides(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("cols", lingon.DefaultTerminalCols, "")
	cmd.Flags().Int("rows", lingon.DefaultTerminalRows, "")
	if err := cmd.Flags().Set("cols", "132"); err != nil {
		t.Fatalf("set cols: %v", err)
	}
	if err := cmd.Flags().Set("rows", "41"); err != nil {
		t.Fatalf("set rows: %v", err)
	}

	cols, rows, err := resolveHeadlessSize(cmd)
	if err != nil {
		t.Fatalf("resolveHeadlessSize: %v", err)
	}
	if cols != 132 || rows != 41 {
		t.Fatalf("resolveHeadlessSize overrides = %dx%d, want 132x41", cols, rows)
	}
}

func TestResolveHeadlessWallInactiveAfterLevelsUsesConfigDefault(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("wall-inactive-after", lingon.DefaultWallInactiveAfterCSV, "")
	cfg := lingon.Config{
		Terminal: lingon.TerminalConfig{
			WallInactiveAfter: "3m,7m",
		},
	}

	levels, err := resolveHeadlessWallInactiveAfterLevels(cmd, cfg)
	if err != nil {
		t.Fatalf("resolveHeadlessWallInactiveAfterLevels: %v", err)
	}
	want := []time.Duration{3 * time.Minute, 7 * time.Minute}
	if !reflect.DeepEqual(levels, want) {
		t.Fatalf("levels = %v, want %v", levels, want)
	}
}

func TestResolveHeadlessWallInactiveAfterLevelsFlagOverridesConfig(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("wall-inactive-after", lingon.DefaultWallInactiveAfterCSV, "")
	if err := cmd.Flags().Set("wall-inactive-after", "1m,2m,4m"); err != nil {
		t.Fatalf("set wall-inactive-after: %v", err)
	}
	cfg := lingon.Config{
		Terminal: lingon.TerminalConfig{
			WallInactiveAfter: "3m,7m",
		},
	}

	levels, err := resolveHeadlessWallInactiveAfterLevels(cmd, cfg)
	if err != nil {
		t.Fatalf("resolveHeadlessWallInactiveAfterLevels: %v", err)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute}
	if !reflect.DeepEqual(levels, want) {
		t.Fatalf("levels = %v, want %v", levels, want)
	}
}

func TestResolveHeadlessWallInactiveAfterLevelsRejectsInvalid(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("wall-inactive-after", lingon.DefaultWallInactiveAfterCSV, "")
	if err := cmd.Flags().Set("wall-inactive-after", "nope"); err != nil {
		t.Fatalf("set wall-inactive-after: %v", err)
	}
	cfg := lingon.Config{}

	if _, err := resolveHeadlessWallInactiveAfterLevels(cmd, cfg); err == nil {
		t.Fatalf("expected parse error for invalid wall-inactive-after")
	}
}
