package main

import (
	"testing"

	"pkt.systems/lingon"
)

func TestAttachHostnameOnlyFlag(t *testing.T) {
	loader := lingon.NewLoader()
	cmd := NewAttachCommand(loader)

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
