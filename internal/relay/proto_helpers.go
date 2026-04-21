package relay

import (
	"time"

	"pkt.systems/lingon/internal/protocolpb"
)

func frameError(message string) *protocolpb.Frame {
	return &protocolpb.Frame{Payload: &protocolpb.Frame_Error{Error: &protocolpb.Error{Message: message}}}
}

func frameErrorSessionRejected(message string) *protocolpb.Frame {
	return &protocolpb.Frame{Payload: &protocolpb.Frame_Error{Error: &protocolpb.Error{
		Message:         message,
		SessionRejected: true,
	}}}
}

func frameErrorRetry(message string, retryAfter time.Duration) *protocolpb.Frame {
	seconds := uint32(0)
	if retryAfter > 0 {
		seconds = uint32(retryAfter.Round(time.Second).Seconds())
		if seconds == 0 {
			seconds = 1
		}
	}
	return &protocolpb.Frame{Payload: &protocolpb.Frame_Error{Error: &protocolpb.Error{
		Message:           message,
		RetryAfterSeconds: seconds,
	}}}
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

func frameSessions(sessions []Session) *protocolpb.Frame {
	infos := make([]*protocolpb.SessionInfo, 0, len(sessions))
	for _, session := range sessions {
		infos = append(infos, &protocolpb.SessionInfo{
			Id:             session.ID,
			Name:           session.Name,
			Status:         session.Status,
			LastActiveUnix: session.LastActiveAt.Unix(),
			Headless:       session.Headless,
		})
	}
	return &protocolpb.Frame{
		Payload: &protocolpb.Frame_Sessions{Sessions: &protocolpb.Sessions{Sessions: infos}},
	}
}

func frameWall(sessionID string, eventID uint64, sender, message string, timeoutSeconds uint32, kind protocolpb.WallKind, sourceSessionID, sourceSessionName string) *protocolpb.Frame {
	return &protocolpb.Frame{
		SessionId: sessionID,
		Payload: &protocolpb.Frame_Wall{Wall: &protocolpb.Wall{
			Id:                eventID,
			Sender:            sender,
			Message:           message,
			TimeoutSeconds:    timeoutSeconds,
			Kind:              kind,
			SourceSessionId:   sourceSessionID,
			SourceSessionName: sourceSessionName,
		}},
	}
}

func frameActivity(sessionID string) *protocolpb.Frame {
	return &protocolpb.Frame{
		SessionId: sessionID,
		Payload:   &protocolpb.Frame_Activity{Activity: &protocolpb.Activity{}},
	}
}
