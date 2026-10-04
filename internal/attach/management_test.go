package attach_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/sessionmanage"
)

func TestManagementBudgetIncludesStalledRefresh(t *testing.T) {
	clk := clock.NewMock()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	client := &attach.Client{Endpoint: "http://127.0.0.1:1", Clock: clk,
		TokenRefresher: func(ctx context.Context) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	done := make(chan error, 1)
	go func() { _, err := client.Manage(parent, managementRenameRequest()); done <- err }()
	awaitManagementSignal(t, started)
	clk.Add(15 * time.Second)
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("refresh remained blocked after the operation budget expired")
	}
	if parent.Err() != nil {
		t.Fatal("management timeout canceled the attach context")
	}
}

func TestManagementMutationUsesRemainingRefreshBudget(t *testing.T) {
	clk := clock.NewMock()
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	client := &attach.Client{Endpoint: server.URL, Clock: clk,
		TokenRefresher: func(context.Context) (string, error) {
			clk.Add(14 * time.Second)
			return "owner", nil
		},
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.Manage(parent, managementRenameRequest()); done <- err }()
	awaitManagementSignal(t, started)
	clk.Add(time.Second)
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout: %v", err)
		}
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("mutation received a fresh budget after refresh consumed fourteen seconds")
	}
}

func managementRenameRequest() sessionmanage.Request {
	return sessionmanage.Request{RequestID: "deadline-regression", SessionID: "source", Kind: "rename", Name: "Build"}
}

func awaitManagementSignal(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("management stage did not start")
	}
}
