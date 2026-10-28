package clipboard

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

// The peer acts as an X server over an owned pipe. No desktop display, caller
// terminal, real-time sleeps or polling participate in these wire regressions.
type selectionFixture struct {
	peer  net.Conn
	clock *clock.MockClock
	image Image
}

func newSelectionFixture(t *testing.T, large bool) *selectionFixture {
	t.Helper()
	owner, peer := net.Pipe()
	if err := peer.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	f := &selectionFixture{peer: peer, clock: clock.NewMock(), image: testImage(t, "image/png", large)}
	c := &xconn{net: owner, r: bufio.NewReader(owner), maxRequest: 4 * ChunkBytes, clock: f.clock,
		atoms: map[string]uint32{"CLIPBOARD": 100, "TARGETS": 101, "image/png": 102, "INCR": 104}}
	done := make(chan struct{})
	go func() { defer close(done); c.serveSelection(f.image) }()
	t.Cleanup(func() { _ = peer.Close(); _ = owner.Close(); <-done })
	return f
}

func (f *selectionFixture) event(t *testing.T, event []byte) {
	t.Helper()
	if _, err := f.peer.Write(event); err != nil {
		t.Fatal("clipboard owner stopped accepting events", err)
	}
}

func (f *selectionFixture) request(t *testing.T) []byte {
	t.Helper()
	head := make([]byte, 4)
	if _, err := io.ReadFull(f.peer, head); err != nil {
		t.Fatal("clipboard owner stopped serving requests", err)
	}
	n := int(le.Uint16(head[2:])) * 4
	if n < 4 || n > ChunkBytes+32 {
		t.Fatalf("invalid owner request length: %d", n)
	}
	request := append(head, make([]byte, n-4)...)
	if _, err := io.ReadFull(f.peer, request[4:]); err != nil {
		t.Fatal(err)
	}
	return request
}

func (f *selectionFixture) begin(t *testing.T, window, property uint32) (uint32, uint32) {
	t.Helper()
	event := make([]byte, 32)
	event[0] = 30 // SelectionRequest
	le.PutUint32(event[12:], window)
	le.PutUint32(event[16:], 100)
	le.PutUint32(event[20:], 102)
	le.PutUint32(event[24:], property)
	f.event(t, event)
	var mask uint32
	for {
		request := f.request(t)
		switch request[0] {
		case 2:
			mask = le.Uint32(request[12:])
		case 25:
			return le.Uint32(request[32:]), mask
		}
	}
}

func (f *selectionFixture) accepted(t *testing.T, window, property uint32) {
	t.Helper()
	if got, _ := f.begin(t, window, property); got != property {
		t.Fatalf("image read rejected for window %d property %d", window, property)
	}
}

func TestClipboardOwnerBadWindowPreservesSelection(t *testing.T) {
	for _, opcode := range []byte{2, 18, 25} {
		t.Run(map[byte]string{2: "subscribe", 18: "property", 25: "notify"}[opcode], func(t *testing.T) {
			f := newSelectionFixture(t, true)
			// Seven sibling readers must survive, while the failed reader's slot
			// must become available to a ninth reader.
			for window := uint32(10); window < 18; window++ {
				f.accepted(t, window, 110)
			}
			packet := make([]byte, 32)
			packet[1], packet[10] = 3, opcode // BadWindow
			le.PutUint32(packet[4:], 10)
			f.event(t, packet)
			f.accepted(t, 19, 110)
			if got, _ := f.begin(t, 20, 110); got != 0 {
				t.Fatal("BadWindow erased unaffected readers")
			}
		})
	}
	t.Run("small image", func(t *testing.T) {
		f := newSelectionFixture(t, false)
		f.accepted(t, 10, 110)
		packet := make([]byte, 32)
		packet[1], packet[10] = 3, 25
		le.PutUint32(packet[4:], 10)
		f.event(t, packet)
		f.accepted(t, 11, 110)
	})
}

func TestClipboardOwnerDestroyedReadersReleaseCapacity(t *testing.T) {
	f := newSelectionFixture(t, true)
	// Destroying one window must release every property on it.
	for property := uint32(110); property < 118; property++ {
		f.accepted(t, 10, property)
	}
	for window := uint32(10); window < 30; window++ {
		destroy := make([]byte, 32)
		destroy[0] = 17 // DestroyNotify
		le.PutUint32(destroy[4:], window)
		le.PutUint32(destroy[8:], window)
		f.event(t, destroy)
		f.accepted(t, window+1, 110)
	}
}

func TestClipboardOwnerRequestsDestroyNotifications(t *testing.T) {
	f := newSelectionFixture(t, true)
	if _, mask := f.begin(t, 10, 110); mask&(1<<17|1<<22) != 1<<17|1<<22 {
		t.Fatalf("owner must subscribe to StructureNotify and PropertyChange: mask=%#x", mask)
	}
}

func TestClipboardOwnerAbandonedReadersExpire(t *testing.T) {
	f := newSelectionFixture(t, true)
	for window := uint32(10); window < 18; window++ {
		f.accepted(t, window, 110)
	}
	if got, _ := f.begin(t, 18, 110); got != 0 {
		t.Fatal("live-reader capacity is unbounded")
	}
	f.clock.Add(30 * time.Second)
	for window := uint32(20); window < 28; window++ {
		f.accepted(t, window, 110)
	}
	if got, _ := f.begin(t, 28, 110); got != 0 {
		t.Fatal("capacity was not preserved after expiry")
	}
}

func TestClipboardOwnerProgressRefreshesOnlyActiveReader(t *testing.T) {
	f := newSelectionFixture(t, true)
	for window := uint32(10); window < 18; window++ {
		f.accepted(t, window, 110)
	}
	f.clock.Add(20 * time.Second)
	deleted := make([]byte, 32)
	deleted[0], deleted[16] = 28, 1
	le.PutUint32(deleted[4:], 10)
	le.PutUint32(deleted[8:], 110)
	f.event(t, deleted)
	chunk := f.request(t)
	if chunk[0] != 18 || !bytes.Equal(chunk[24:], f.image.Data[:ChunkBytes]) {
		t.Fatal("active reader lost its original image")
	}
	f.clock.Add(10 * time.Second)
	for window := uint32(20); window < 27; window++ {
		f.accepted(t, window, 110)
	}
	if got, _ := f.begin(t, 27, 110); got != 0 {
		t.Fatal("active reader expired despite progress")
	}
	f.clock.Add(20 * time.Second)
	f.accepted(t, 27, 110)
	if got, _ := f.begin(t, 28, 110); got != 0 {
		t.Fatal("reader progress extended other readers' lifetimes")
	}
}

func TestClipboardOwnerReplacesSameTransferAtCapacity(t *testing.T) {
	f := newSelectionFixture(t, true)
	for window := uint32(10); window < 18; window++ {
		f.accepted(t, window, 110)
	}
	f.accepted(t, 10, 110)
	if got, _ := f.begin(t, 18, 110); got != 0 {
		t.Fatal("replacement increased capacity")
	}
}

func TestClipboardOwnerCompletedReadReleasesCapacity(t *testing.T) {
	f := newSelectionFixture(t, true)
	for window := uint32(10); window < 18; window++ {
		f.accepted(t, window, 110)
	}
	var got []byte
	for {
		deleted := make([]byte, 32)
		deleted[0], deleted[16] = 28, 1
		le.PutUint32(deleted[4:], 10)
		le.PutUint32(deleted[8:], 110)
		f.event(t, deleted)
		chunk := f.request(t)
		n := int(le.Uint32(chunk[20:]))
		if chunk[0] != 18 || n > ChunkBytes || n > len(chunk)-24 {
			t.Fatal("invalid INCR chunk size")
		}
		if n == 0 {
			break
		}
		got = append(got, chunk[24:24+n]...)
	}
	if !bytes.Equal(got, f.image.Data) {
		t.Fatal("INCR changed encoded bytes")
	}
	f.accepted(t, 18, 110)
	if got, _ := f.begin(t, 19, 110); got != 0 {
		t.Fatal("completed read erased another reader's transfer")
	}
}

func TestClipboardOwnerIdleAndFatalErrors(t *testing.T) {
	t.Run("idle ownership", func(t *testing.T) {
		f := newSelectionFixture(t, true)
		f.clock.Add(time.Hour)
		f.accepted(t, 10, 110)
	})
	for _, code := range []byte{3, 5} {
		t.Run(map[byte]string{3: "unrelated BadWindow", 5: "BadAtom"}[code], func(t *testing.T) {
			f := newSelectionFixture(t, true)
			packet := make([]byte, 32)
			packet[1], packet[10] = code, 16 // InternAtom is not a reader write.
			f.event(t, packet)
			var b [1]byte
			if _, err := f.peer.Read(b[:]); err != io.EOF {
				t.Fatalf("unexpected fatal-error result: %v", err)
			}
		})
	}
}
