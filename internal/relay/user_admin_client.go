package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// UserAdmin is an administrative mutator with owned transport/state cleanup.
type UserAdmin interface {
	UserMutator
	Close() error
}

// UserAdminOptions identifies the existing local CLI configuration.
type UserAdminOptions struct {
	DataDir   string
	UsersFile string
}

type offlineUserAdmin struct {
	*HTTPServer
	owner *StateOwner
}

func (a *offlineUserAdmin) Close() error { return a.owner.Close() }

type remoteUserAdmin struct {
	client    *http.Client
	transport *http.Transport
	usersFile string
}

func (a *remoteUserAdmin) Close() error { a.transport.CloseIdleConnections(); return nil }

// NewUserAdmin selects the live owner, or acquires exclusive offline ownership.
func NewUserAdmin(opts UserAdminOptions) (UserAdmin, error) {
	owner, err := NewStateOwner(opts.DataDir)
	if errors.Is(err, ErrStateOwned) {
		path := userAdminSocket(opts.DataDir)
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialUserAdminSocket(ctx, path)
		}}
		return &remoteUserAdmin{client: &http.Client{Transport: transport, Timeout: 10 * time.Second}, transport: transport, usersFile: opts.UsersFile}, nil
	}
	if err != nil {
		return nil, err
	}
	store, err := owner.LoadStore()
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	users, err := owner.LoadUsers(opts.UsersFile)
	if err != nil {
		_ = owner.Close()
		return nil, err
	}
	server := NewHTTPServer(store, users, nil, nil, nil)
	server.DataDir, server.UsersFile = opts.DataDir, opts.UsersFile
	return &offlineUserAdmin{HTTPServer: server, owner: owner}, nil
}

func (a *remoteUserAdmin) MutateUser(ctx context.Context, mutation UserMutationRequest) (UserMutationResult, error) {
	ref, err := newUserFileReference(a.usersFile)
	if err != nil {
		return UserMutationResult{}, err
	}
	var payload bytes.Buffer
	if err := json.NewEncoder(&payload).Encode(userAdminRequest{UsersFile: ref, Mutation: mutation}); err != nil {
		return UserMutationResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://relay-owner/mutate", &payload)
	if err != nil {
		return UserMutationResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return UserMutationResult{}, fmt.Errorf("relay state is owned; local user administration unavailable (no offline fallback): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var failure map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		return UserMutationResult{}, fmt.Errorf("local user administration: %s", failure["error"])
	}
	var result userAdminResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return UserMutationResult{}, err
	}
	if result.Code != "" {
		return UserMutationResult{}, decodeUserAdminError(result)
	}
	return result.Result, nil
}
