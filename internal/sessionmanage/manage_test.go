package sessionmanage

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestManagementCoalescesAndRejectsConflictingOrBusyRequests(t *testing.T) {
	m := New()
	req := Request{RequestID: "once", SessionID: "host", Kind: "create"}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	result := make(chan Result, 1)
	go func() {
		result <- m.Execute(context.Background(), req, func() Result {
			calls.Add(1)
			close(entered)
			<-release
			return Result{SessionID: "child", Name: "child"}
		})
	}()
	<-entered
	other := req
	other.RequestID = "other"
	if got := m.Execute(context.Background(), other, func() Result { t.Error("busy operation executed"); return Result{} }); got.Error == "" {
		t.Fatal("busy request succeeded")
	}
	conflict := req
	conflict.Kind = "rename"
	conflict.Name = "renamed"
	if got := m.Execute(context.Background(), conflict, func() Result { return Result{} }); got.Error == "" {
		t.Fatal("conflicting request ID succeeded")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := m.Execute(cancelled, req, func() Result { return Result{} }); got.Error == "" {
		t.Fatal("cancelled duplicate waited")
	}
	close(release)
	first := <-result
	for range 10 {
		got := m.Execute(context.Background(), req, func() Result { calls.Add(1); return Result{} })
		if got != first {
			t.Fatalf("duplicate=%+v want %+v", got, first)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("launched %d times", calls.Load())
	}
}

func TestManagementBoundsReceiptsAndValidatesNames(t *testing.T) {
	m := New()
	for i := range 300 {
		id := string(rune(0x100 + i))
		m.Execute(context.Background(), Request{RequestID: id, SessionID: "host", Kind: "create"}, func() Result { return Result{SessionID: "child"} })
	}
	if len(m.receipts) != 256 || len(m.order) != 256 {
		t.Fatal("receipt cache is unbounded")
	}
	for _, name := range []string{"", " name", "name ", "line\nname", "\x1b[2J", "hidden\u202ename", string([]byte{0xff})} {
		if ValidateName(name) == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{"build server", "日本語 🦊"} {
		if err := ValidateName(name); err != nil {
			t.Error(err)
		}
	}
}
