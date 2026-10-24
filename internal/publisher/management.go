package publisher

import (
	"context"

	"pkt.systems/lingon/internal/protocolpb"
)

func (p *Publisher) managementLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-p.management:
			reply := &protocolpb.SessionManagement{Error: "session management unavailable in this host"}
			if p.OnManagement != nil {
				reply = p.OnManagement(ctx, request)
			}
			reply.CorrelationId = request.CorrelationId
			reply.RequestId = request.RequestId
			reply.Kind = request.Kind
			reply.Completed = true
			p.SendManagement(reply)
		}
	}
}

// SendManagement acknowledges lifecycle work without entering terminal replay history.
func (p *Publisher) SendManagement(reply *protocolpb.SessionManagement) {
	p.sendFrame(&protocolpb.Frame{SessionId: p.opts.SessionID, Payload: &protocolpb.Frame_Management{Management: reply}})
}

// SetSessionName updates metadata used by subsequent host registrations.
func (p *Publisher) SetSessionName(name string) {
	p.mu.Lock()
	p.opts.SessionName = name
	p.mu.Unlock()
}

// AccessToken returns the current host credential, including refreshed tokens.
func (p *Publisher) AccessToken() string { p.mu.Lock(); defer p.mu.Unlock(); return p.opts.Token }
