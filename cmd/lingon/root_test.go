package main

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/testutil"
)

func TestRootCommandSuppressesUsageOnError(t *testing.T) {
	testutil.SetXDGConfigEnv(t)

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"attach", "--pick", "session1"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if strings.Contains(out.String(), "Usage:") || strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("unexpected usage output: out=%q err=%q", out.String(), errOut.String())
	}
	if errOut.String() != "" {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}

func TestRootOfflineFlag(t *testing.T) {
	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	if err := cmd.Flags().Parse([]string{"--offline"}); err != nil {
		t.Fatalf("parse --offline: %v", err)
	}
	offline, err := cmd.Flags().GetBool("offline")
	if err != nil {
		t.Fatalf("get offline flag: %v", err)
	}
	if !offline {
		t.Fatalf("expected --offline to set flag")
	}

	cmd = NewRootCommand(loader)
	if err := cmd.Flags().Parse([]string{"-o"}); err != nil {
		t.Fatalf("parse -o: %v", err)
	}
	offline, err = cmd.Flags().GetBool("offline")
	if err != nil {
		t.Fatalf("get offline flag: %v", err)
	}
	if !offline {
		t.Fatalf("expected -o to set flag")
	}
}

func TestRootHostnameOnlyFlag(t *testing.T) {
	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	if err := cmd.Flags().Parse([]string{"--hostname-only"}); err != nil {
		t.Fatalf("parse --hostname-only: %v", err)
	}
	hostnameOnly, err := cmd.Flags().GetBool("hostname-only")
	if err != nil {
		t.Fatalf("get hostname-only flag: %v", err)
	}
	if !hostnameOnly {
		t.Fatalf("expected --hostname-only to set flag")
	}
}
