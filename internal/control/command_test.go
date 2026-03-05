package control

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestCommandForAction(t *testing.T) {
	tests := []struct {
		name   string
		action Action
		want   protocolpb.CommandKind
		ok     bool
	}{
		{name: "send eof", action: ActionSendCtrlD, want: protocolpb.CommandKind_COMMAND_KIND_SEND_EOF, ok: true},
		{name: "toggle offline", action: ActionToggleOffline, want: protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE, ok: true},
		{name: "toggle respawn", action: ActionToggleRespawn, want: protocolpb.CommandKind_COMMAND_KIND_TOGGLE_RESPAWN, ok: true},
		{name: "cycle wall", action: ActionToggleWallInactivity, want: protocolpb.CommandKind_COMMAND_KIND_CYCLE_WALL_INACTIVITY, ok: true},
		{name: "quit", action: ActionQuit, want: protocolpb.CommandKind_COMMAND_KIND_UNSPECIFIED, ok: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CommandForAction(tc.action)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("kind = %v, want %v", got, tc.want)
			}
		})
	}
}
