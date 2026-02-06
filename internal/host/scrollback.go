package host

import (
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

const scrollbackChunkSize = 100

func buildScrollbackFrames(sessionID string, cols int, rows []terminal.ScrollbackRow, clear bool) []*protocolpb.Frame {
	if len(rows) == 0 && !clear {
		return nil
	}
	chunkSize := scrollbackChunkSize
	if chunkSize <= 0 {
		chunkSize = 100
	}
	if cols <= 0 && len(rows) > 0 {
		cols = rows[0].Cols
	}
	var frames []*protocolpb.Frame
	if len(rows) == 0 {
		msg := &protocolpb.Scrollback{
			Cols:  uint32(cols),
			Clear: clear,
		}
		frames = append(frames, &protocolpb.Frame{
			SessionId: sessionID,
			Payload:   &protocolpb.Frame_Scrollback{Scrollback: msg},
		})
		return frames
	}
	for i := 0; i < len(rows); i += chunkSize {
		end := i + chunkSize
		if end > len(rows) {
			end = len(rows)
		}
		msg := &protocolpb.Scrollback{
			Cols:  uint32(cols),
			Clear: clear && i == 0,
		}
		for _, row := range rows[i:end] {
			msg.Rows = append(msg.Rows, protocol.ScrollbackRowToProto(row))
		}
		frames = append(frames, &protocolpb.Frame{
			SessionId: sessionID,
			Payload:   &protocolpb.Frame_Scrollback{Scrollback: msg},
		})
	}
	return frames
}
