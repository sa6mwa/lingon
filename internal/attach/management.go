package attach

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"pkt.systems/lingon/internal/sessionmanage"
)

// Manage performs an owner-authorized operation without taking terminal control.
func (c *Client) Manage(ctx context.Context, req sessionmanage.Request) (sessionmanage.Result, error) {
	if c.ShareToken != "" {
		return sessionmanage.Result{}, fmt.Errorf("session management requires the owner account")
	}
	endpoint := "http://unix"
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	token := c.AccessToken
	if c.UnixSocket != "" {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", c.UnixSocket)
		}
	} else {
		var err error
		_, endpoint, err = normalizeEndpoint(c.Endpoint)
		if err != nil {
			return sessionmanage.Result{}, err
		}
		transport.TLSClientConfig, err = clientTLSConfig(c.TLSDir, c.Insecure)
		if err != nil {
			return sessionmanage.Result{}, err
		}
		if c.TokenRefresher != nil {
			token, err = c.TokenRefresher(ctx)
			if err != nil {
				return sessionmanage.Result{}, err
			}
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return sessionmanage.Call(callCtx, &http.Client{Transport: transport}, endpoint, token, req)
}
