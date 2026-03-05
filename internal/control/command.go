package control

import "pkt.systems/lingon/internal/protocolpb"

// CommandForAction maps control actions to protocol commands.
func CommandForAction(action Action) (protocolpb.CommandKind, bool) {
	switch action {
	case ActionSendCtrlD:
		return protocolpb.CommandKind_COMMAND_KIND_SEND_EOF, true
	case ActionToggleOffline:
		return protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE, true
	case ActionToggleRespawn:
		return protocolpb.CommandKind_COMMAND_KIND_TOGGLE_RESPAWN, true
	case ActionToggleWallInactivity:
		return protocolpb.CommandKind_COMMAND_KIND_CYCLE_WALL_INACTIVITY, true
	default:
		return protocolpb.CommandKind_COMMAND_KIND_UNSPECIFIED, false
	}
}
