package host

import (
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
)

func snapshotToProto(s terminal.Snapshot) *protocolpb.Snapshot {
	return protocol.SnapshotToProto(s)
}
