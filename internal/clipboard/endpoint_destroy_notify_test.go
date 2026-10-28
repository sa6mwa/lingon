package clipboard

import (
	"errors"
	"testing"

	"golang.design/x/x11"
)

func endpointSelectStructure(t *testing.T, c *xconn, window uint32, create bool, events uint32) {
	t.Helper()
	var req []byte
	if create {
		req = x11.CreateWindow(window, 1)
		le.PutUint32(req[28:], 1<<1|1<<11|1<<12)
	} else {
		req = make([]byte, 12)
		req[0] = 2
		le.PutUint32(req[4:], window)
		le.PutUint32(req[8:], 1<<1|1<<11|1<<12)
	}
	// EventMask is not necessarily the first value in the attribute list.
	req = le.AppendUint32(req, 0)
	req = le.AppendUint32(req, events)
	req = le.AppendUint32(req, 0)
	le.PutUint16(req[2:], uint16(len(req)/4))
	if err := c.send(req); err != nil {
		t.Fatal(err)
	}
}

// GetInputFocus is a roundtrip fence: the server must deliver DestroyNotify
// before answering the later request. Absence fails immediately at that reply,
// without a sleep, polling loop or elapsed-time assertion.
func assertEndpointDestroyed(t *testing.T, c *xconn, window uint32, want bool) {
	t.Helper()
	destroyEndpointWindow(t, c, window)
	destroySeq := c.seq
	if err := c.send(x11.GetInputFocus()); err != nil {
		t.Fatal(err)
	}
	fenceSeq := c.seq
	got := false
	for {
		p, err := c.read()
		if err != nil {
			t.Fatal(err)
		}
		if p.EventCode() == 17 {
			if got || le.Uint32(p.Raw[4:]) != window || le.Uint32(p.Raw[8:]) != window || p.Sequence() != destroySeq {
				t.Fatalf("invalid or duplicate DestroyNotify: %x", p.Raw)
			}
			got = true
		}
		if p.IsReply() && p.Sequence() == fenceSeq {
			break
		}
	}
	if got != want {
		t.Fatalf("DestroyNotify received=%v, want=%v; a clipboard reader waiting to join its event thread would freeze", got, want)
	}
}

func TestClipboardEndpointDestroyNotifiesClipboardReader(t *testing.T) {
	for _, mode := range []string{"create", "change", "unsubscribed", "unsubscribe", "unrelated-change"} {
		t.Run(mode, func(t *testing.T) {
			e := regressionEndpoint(t)
			c := endpointWireClient(t, e, x11.FamilyLocal)
			window := c.win + 1
			const structureNotify = 1 << 17
			events := uint32(structureNotify)
			if mode == "unsubscribed" {
				events = 1 << 22 // PropertyChange alone must not select destruction.
			}
			if mode == "change" {
				events = 0
			}
			endpointSelectStructure(t, c, window, true, events)
			if mode == "change" {
				endpointSelectStructure(t, c, window, false, structureNotify|1<<22)
			}
			if mode == "unsubscribe" {
				endpointSelectStructure(t, c, window, false, 0)
			}
			if mode == "unrelated-change" {
				req := []byte{2, 0, 4, 0}
				req = le.AppendUint32(req, window)
				req = le.AppendUint32(req, 1<<1) // background pixel, no EventMask
				req = le.AppendUint32(req, 42)
				if err := c.send(req); err != nil {
					t.Fatal(err)
				}
			}
			want := mode == "create" || mode == "change" || mode == "unrelated-change"
			assertEndpointDestroyed(t, c, window, want)
			// A destroyed window must not retain its old event subscription.
			assertEndpointDestroyed(t, c, window, false)
		})
	}
}

func TestClipboardEndpointStructureSubscriptionsBounded(t *testing.T) {
	c := endpointWireClient(t, regressionEndpoint(t), x11.FamilyLocal)
	for n := range uint32(128) {
		endpointSelectStructure(t, c, c.win+1+n, true, 1<<17)
	}
	if _, err := c.reply(x11.GetInputFocus()); err != nil {
		t.Fatal(err)
	}
	endpointSelectStructure(t, c, c.win+129, true, 1<<17)
	_, err := c.read()
	var protocolErr *x11Error
	if !errors.As(err, &protocolErr) || protocolErr.code != 11 {
		t.Fatalf("unbounded subscription allocation: %v", err)
	}
	assertEndpointDestroyed(t, c, c.win+1, true)
	endpointSelectStructure(t, c, c.win+129, true, 1<<17)
	if _, err := c.reply(x11.GetInputFocus()); err != nil {
		t.Fatal("destroyed subscription was not reclaimed", err)
	}
	assertEndpointDestroyed(t, c, c.win+129, true)
}
