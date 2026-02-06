package attach_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachTabSwitchUpdatesTabBar(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	h := newHarness(t)
	_ = h.StartHost(ptytest.HostOptions{
		SessionID:   "host-1",
		SessionName: "alpha",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	_ = h.StartHost(ptytest.HostOptions{
		SessionID:   "host-2",
		SessionName: "beta",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	_ = h.StartHost(ptytest.HostOptions{
		SessionID:   "host-3",
		SessionName: "gamma",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-1", "host-2", "host-3"})

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "host-1",
		Cols:      120,
		Rows:      30,
	})

	attach.Eventually(6*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if hasConnectionStatusBanner(row) {
			return fmt.Errorf("waiting for connection banner to clear; row=%q", row)
		}
		if !strings.Contains(row, "alpha") || !strings.Contains(row, "beta") || !strings.Contains(row, "gamma") {
			return fmt.Errorf("expected tabs for alpha/beta/gamma, got %q", row)
		}
		return nil
	})

	order, err := tabOrder(attach.Screen().Row(0), []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("tab order: %v", err)
	}
	active0, err := activeTabLabel(attach, []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("active tab: %v", err)
	}

	// Visit all tabs to establish connections before testing fast updates.
	for i := 0; i < len(order)-1; i++ {
		attach.SendCtrlL()
		attach.Send("n")
		attach.Eventually(5*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
			active, err := activeTabLabel(attach, []string{"alpha", "beta", "gamma"})
			if err != nil {
				return err
			}
			if active == active0 {
				return fmt.Errorf("expected active tab to change while priming connections")
			}
			active0 = active
			return nil
		})
	}
	h.Advance(4 * time.Second)
	attach.Eventually(6*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if hasConnectionStatusBanner(row) {
			return fmt.Errorf("waiting for connection banner to clear after priming; row=%q", row)
		}
		if !strings.Contains(row, "alpha") || !strings.Contains(row, "beta") || !strings.Contains(row, "gamma") {
			return fmt.Errorf("expected tabs for alpha/beta/gamma after priming, got %q", row)
		}
		return nil
	})

	order, err = tabOrder(attach.Screen().Row(0), []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("tab order after priming: %v", err)
	}
	active0, err = activeTabLabel(attach, []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("active tab after priming: %v", err)
	}

	attach.SendCtrlL()
	attach.Send("n")

	expect1 := nextLabel(order, active0)
	attach.Eventually(5*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		active1, err := activeTabLabel(attach, []string{"alpha", "beta", "gamma"})
		if err != nil {
			return err
		}
		if active1 != expect1 {
			return fmt.Errorf("expected active tab %q after next, got %q", expect1, active1)
		}
		return nil
	})

	attach.SendCtrlL()
	attach.Send("n")

	expect2 := nextLabel(order, expect1)
	attach.Eventually(5*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		active2, err := activeTabLabel(attach, []string{"alpha", "beta", "gamma"})
		if err != nil {
			return err
		}
		if active2 != expect2 {
			return fmt.Errorf("expected active tab %q after second next, got %q", expect2, active2)
		}
		return nil
	})

	attach.SendCtrlL()
	attach.Send("p")

	expect3 := prevLabel(order, expect2)
	attach.Eventually(5*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		active3, err := activeTabLabel(attach, []string{"alpha", "beta", "gamma"})
		if err != nil {
			return err
		}
		if active3 != expect3 {
			return fmt.Errorf("expected active tab %q after prev, got %q", expect3, active3)
		}
		return nil
	})
}

func tabOrder(row string, labels []string) ([]string, error) {
	type entry struct {
		label string
		idx   int
	}
	entries := make([]entry, 0, len(labels))
	for _, label := range labels {
		idx := strings.Index(row, label)
		if idx == -1 {
			return nil, fmt.Errorf("missing label %q in row %q", label, row)
		}
		entries = append(entries, entry{label: label, idx: idx})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].idx < entries[j].idx
	})
	order := make([]string, 0, len(entries))
	for _, entry := range entries {
		order = append(order, entry.label)
	}
	return order, nil
}

func nextLabel(order []string, current string) string {
	if len(order) == 0 {
		return ""
	}
	for i, label := range order {
		if label == current {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

func prevLabel(order []string, current string) string {
	if len(order) == 0 {
		return ""
	}
	for i, label := range order {
		if label == current {
			return order[(i-1+len(order))%len(order)]
		}
	}
	return order[0]
}
