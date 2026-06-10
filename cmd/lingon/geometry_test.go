package main

import (
	"testing"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

func TestParseGeometryUsesColumnsByRowsOrder(t *testing.T) {
	cols, rows, err := parseGeometry("132x41")
	if err != nil {
		t.Fatalf("parseGeometry: %v", err)
	}
	if cols != 132 || rows != 41 {
		t.Fatalf("parseGeometry = %dx%d, want 132x41", cols, rows)
	}
}

func TestParseGeometryAcceptsUppercaseSeparatorAndZeros(t *testing.T) {
	cols, rows, err := parseGeometry("0X50")
	if err != nil {
		t.Fatalf("parseGeometry: %v", err)
	}
	if cols != 0 || rows != 50 {
		t.Fatalf("parseGeometry = %dx%d, want 0x50", cols, rows)
	}
}

func TestParseGeometryRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "80", "80x24x1", "80x", "x24", "-1x24", "80x-1", "Ax24"} {
		if _, _, err := parseGeometry(raw); err == nil {
			t.Fatalf("parseGeometry(%q) succeeded, want error", raw)
		}
	}
}

func TestResolveRootHostSizeGeometryOverridesRowsAndCols(t *testing.T) {
	cmd := geometryTestCommand(t)
	if err := cmd.Flags().Set("cols", "132"); err != nil {
		t.Fatalf("set cols: %v", err)
	}
	if err := cmd.Flags().Set("rows", "41"); err != nil {
		t.Fatalf("set rows: %v", err)
	}
	if err := cmd.Flags().Set(geometryFlagName, "80x24"); err != nil {
		t.Fatalf("set geometry: %v", err)
	}

	cols, rows, err := resolveRootHostSize(cmd, 132, 41)
	if err != nil {
		t.Fatalf("resolveRootHostSize: %v", err)
	}
	if cols != 80 || rows != 24 {
		t.Fatalf("resolveRootHostSize = %dx%d, want 80x24", cols, rows)
	}
}

func TestResolveRootHostSizeGeometryZeroDimensionsUseAutoDetectSentinels(t *testing.T) {
	tests := []struct {
		raw      string
		wantCols int
		wantRows int
	}{
		{raw: "80x0", wantCols: 80, wantRows: 0},
		{raw: "0x50", wantCols: 0, wantRows: 50},
		{raw: "0x0", wantCols: 0, wantRows: 0},
	}
	for _, tc := range tests {
		cmd := geometryTestCommand(t)
		if err := cmd.Flags().Set(geometryFlagName, tc.raw); err != nil {
			t.Fatalf("set geometry: %v", err)
		}
		cols, rows, err := resolveRootHostSize(cmd, lingon.DefaultTerminalCols, lingon.DefaultTerminalRows)
		if err != nil {
			t.Fatalf("resolveRootHostSize(%q): %v", tc.raw, err)
		}
		if cols != tc.wantCols || rows != tc.wantRows {
			t.Fatalf("resolveRootHostSize(%q) = %dx%d, want %dx%d", tc.raw, cols, rows, tc.wantCols, tc.wantRows)
		}
	}
}

func TestResolveRootHostSizeWithoutSizeFlagsUsesAutoDetectSentinels(t *testing.T) {
	cmd := geometryTestCommand(t)

	cols, rows, err := resolveRootHostSize(cmd, lingon.DefaultTerminalCols, lingon.DefaultTerminalRows)
	if err != nil {
		t.Fatalf("resolveRootHostSize: %v", err)
	}
	if cols != 0 || rows != 0 {
		t.Fatalf("resolveRootHostSize without flags = %dx%d, want 0x0", cols, rows)
	}
}

func TestResolveHeadlessSizeGeometryOverridesRowsAndCols(t *testing.T) {
	cmd := geometryTestCommand(t)
	if err := cmd.Flags().Set("cols", "132"); err != nil {
		t.Fatalf("set cols: %v", err)
	}
	if err := cmd.Flags().Set("rows", "41"); err != nil {
		t.Fatalf("set rows: %v", err)
	}
	if err := cmd.Flags().Set(geometryFlagName, "80x24"); err != nil {
		t.Fatalf("set geometry: %v", err)
	}

	cols, rows, err := resolveHeadlessSize(cmd)
	if err != nil {
		t.Fatalf("resolveHeadlessSize: %v", err)
	}
	if cols != 80 || rows != 24 {
		t.Fatalf("resolveHeadlessSize = %dx%d, want 80x24", cols, rows)
	}
}

func TestResolveHeadlessSizeGeometryZeroDimensionsUseDefaults(t *testing.T) {
	tests := []struct {
		raw      string
		wantCols int
		wantRows int
	}{
		{raw: "80x0", wantCols: 80, wantRows: lingon.DefaultTerminalRows},
		{raw: "0x50", wantCols: lingon.DefaultTerminalCols, wantRows: 50},
		{raw: "0x0", wantCols: lingon.DefaultTerminalCols, wantRows: lingon.DefaultTerminalRows},
	}
	for _, tc := range tests {
		cmd := geometryTestCommand(t)
		if err := cmd.Flags().Set(geometryFlagName, tc.raw); err != nil {
			t.Fatalf("set geometry: %v", err)
		}
		cols, rows, err := resolveHeadlessSize(cmd)
		if err != nil {
			t.Fatalf("resolveHeadlessSize(%q): %v", tc.raw, err)
		}
		if cols != tc.wantCols || rows != tc.wantRows {
			t.Fatalf("resolveHeadlessSize(%q) = %dx%d, want %dx%d", tc.raw, cols, rows, tc.wantCols, tc.wantRows)
		}
	}
}

func TestValidateHeadlessReexecFlagsRejectsInvalidGeometry(t *testing.T) {
	cmd := geometryTestCommand(t)
	if err := cmd.Flags().Set(geometryFlagName, "bad"); err != nil {
		t.Fatalf("set geometry: %v", err)
	}

	if err := validateHeadlessReexecFlags(cmd); err == nil {
		t.Fatalf("validateHeadlessReexecFlags accepted invalid geometry")
	}
}

func TestValidateHeadlessReexecFlagsAcceptsValidGeometry(t *testing.T) {
	cmd := geometryTestCommand(t)
	if err := cmd.Flags().Set(geometryFlagName, "80x0"); err != nil {
		t.Fatalf("set geometry: %v", err)
	}

	if err := validateHeadlessReexecFlags(cmd); err != nil {
		t.Fatalf("validateHeadlessReexecFlags: %v", err)
	}
}

func TestRootCommandRegistersGeometryFlag(t *testing.T) {
	cmd := NewRootCommand(lingon.NewLoader())
	flag := cmd.Flags().Lookup(geometryFlagName)
	if flag == nil {
		t.Fatalf("missing --%s flag", geometryFlagName)
	}
	if flag.Shorthand != "g" {
		t.Fatalf("--%s shorthand = %q, want g", geometryFlagName, flag.Shorthand)
	}
}

func geometryTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("cols", lingon.DefaultTerminalCols, "")
	cmd.Flags().Int("rows", lingon.DefaultTerminalRows, "")
	cmd.Flags().StringP(geometryFlagName, "g", "", "")
	return cmd
}
