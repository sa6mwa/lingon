package main

import "testing"

func TestResolveLoginInputNonInteractiveRequiresEnv(t *testing.T) {
	_, _, err := resolveLoginInput(true, true, func(string) string { return "" })
	if err == nil {
		t.Fatalf("expected error for missing env vars")
	}
}

func TestResolveLoginInputNonInteractiveUsesEnv(t *testing.T) {
	got, useEnv, err := resolveLoginInput(true, true, func(key string) string {
		switch key {
		case "LINGON_USERNAME":
			return "alice"
		case "LINGON_PASSWORD":
			return "secret"
		case "LINGON_TOTP":
			return "123456"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !useEnv {
		t.Fatalf("expected useEnv=true")
	}
	if got.Username != "alice" || got.Password != "secret" || got.TOTP != "123456" {
		t.Fatalf("unexpected login input: %+v", got)
	}
}

func TestResolveLoginInputNonInteractiveRequiresFlagForNonTTY(t *testing.T) {
	_, _, err := resolveLoginInput(false, false, func(string) string { return "" })
	if err == nil {
		t.Fatalf("expected error for non-tty without --non-interactive")
	}
}
