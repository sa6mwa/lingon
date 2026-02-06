package mvu

import (
	"testing"
	"time"
)

func TestAttachWaitControllerLifecycle(t *testing.T) {
	ctrl := NewAttachWaitController(30 * time.Second)
	if ctrl.CanStart() {
		t.Fatalf("wait should not start before allowance")
	}
	ctrl.AllowStart()
	if !ctrl.CanStart() {
		t.Fatalf("wait should be allowed")
	}
	now := time.Now()
	if !ctrl.Start(now) {
		t.Fatalf("expected wait start")
	}
	if ctrl.Start(now.Add(time.Second)) {
		t.Fatalf("second start should be ignored")
	}
	if !ctrl.Waiting() {
		t.Fatalf("expected waiting state")
	}
	if ctrl.WaitUntil().IsZero() {
		t.Fatalf("expected wait deadline")
	}
	if ctrl.Expired(now) {
		t.Fatalf("wait should not be expired immediately")
	}
	if !ctrl.Expired(now.Add(31 * time.Second)) {
		t.Fatalf("wait should be expired")
	}
	if !ctrl.Stop() {
		t.Fatalf("expected stop to report active wait")
	}
	if ctrl.Waiting() {
		t.Fatalf("wait should be inactive")
	}
}
