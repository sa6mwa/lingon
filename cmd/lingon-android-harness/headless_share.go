package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/protocolpb"
)

func (h *harness) createHeadlessShare(ctx context.Context, id string) (lingon.ShareCreateResponse, error) {
	// A local socket can exist before its publisher registers with the relay.
	// Observe the account session stream before asking the SDK for a real share.
	endpoint, err := url.Parse(h.endpoint)
	if err != nil {
		return lingon.ShareCreateResponse{}, err
	}
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/ws/client"
	ws, _, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{
		HTTPClient: h.server.Client(),
		HTTPHeader: http.Header{"Authorization": {"Bearer " + h.access}},
	})
	if err != nil {
		return lingon.ShareCreateResponse{}, fmt.Errorf("observe headless publication: %w", err)
	}
	defer ws.CloseNow()
	hello, err := proto.Marshal(&protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
		ClientId: "harness-publication-" + id, ClientType: "android-harness",
	}}})
	if err != nil {
		return lingon.ShareCreateResponse{}, err
	}
	if err := ws.Write(ctx, websocket.MessageBinary, hello); err != nil {
		return lingon.ShareCreateResponse{}, err
	}
publication:
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return lingon.ShareCreateResponse{}, fmt.Errorf("wait for headless %s publication: %w", id, err)
		}
		var frame protocolpb.Frame
		if err := proto.Unmarshal(data, &frame); err != nil {
			return lingon.ShareCreateResponse{}, err
		}
		for _, session := range frame.GetSessions().GetSessions() {
			if session.GetId() == id && session.GetStatus() == "active" {
				break publication
			}
		}
	}
	return lingon.ShareCreate(ctx, lingon.ShareCreateOptions{
		Endpoint: h.endpoint, AccessToken: h.access, SessionID: id,
		Scope: lingon.ShareScopeView, TTL: time.Hour, TLSDir: filepath.Dir(h.config.CACertPath),
	})
}
