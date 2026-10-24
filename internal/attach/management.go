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
	// Refresh and mutation share one budget, driven by the same clock as attach.
	callCtx, cancel := context.WithCancelCause(ctx)
	timer := c.clock().AfterFunc(15*time.Second, func() { cancel(context.DeadlineExceeded) })
	defer timer.Stop()
	defer cancel(context.Canceled)
	endpoint := "http://unix"
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	token := c.accessToken()
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
			token, err = c.TokenRefresher(callCtx)
			if err != nil {
				if callCtx.Err() != nil {
					return sessionmanage.Result{}, context.Cause(callCtx)
				}
				return sessionmanage.Result{}, err
			}
		}
	}
	if callCtx.Err() != nil {
		return sessionmanage.Result{}, context.Cause(callCtx)
	}
	result, err := sessionmanage.Call(callCtx, &http.Client{Transport: transport}, endpoint, token, req)
	if err != nil && callCtx.Err() != nil {
		return result, fmt.Errorf("%w: %w", err, context.Cause(callCtx))
	}
	return result, err
}
