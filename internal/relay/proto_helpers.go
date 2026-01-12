package relay

import "pkt.systems/lingon/internal/protocolpb"

func frameError(message string) *protocolpb.Frame {
	return &protocolpb.Frame{Payload: &protocolpb.Frame_Error{Error: &protocolpb.Error{Message: message}}}
}

func frameWelcome(granted bool, cols, rows int, holder string, sessionID string) *protocolpb.Frame {
	return &protocolpb.Frame{
		SessionId: sessionID,
		Payload: &protocolpb.Frame_Welcome{Welcome: &protocolpb.Welcome{
			GrantedControl: granted,
			ServerCols:     uint32(cols),
			ServerRows:     uint32(rows),
			HolderClientId: holder,
		}},
	}
}

func frameControl(sessionID, holder string) *protocolpb.Frame {
	return &protocolpb.Frame{
		SessionId: sessionID,
		Payload:   &protocolpb.Frame_Control{Control: &protocolpb.Control{HolderClientId: holder}},
	}
}
