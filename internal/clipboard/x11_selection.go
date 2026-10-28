package clipboard

import (
	"errors"
	"fmt"
	"time"

	"golang.design/x/x11"
	"pkt.systems/lingon/internal/clock"
)

const selectionIdleTimeout = 30 * time.Second

type x11Error struct {
	code, opcode byte
	resource     uint32
}

func (e *x11Error) Error() string { return fmt.Sprintf("X11 error %d", e.code) }

// BadWindow on a requestor write is an ordinary reader-disconnect race, not
// failure of the connection owning CLIPBOARD. Other protocol errors stay fatal.
func (e *x11Error) vanishedReader() bool {
	return e.code == 3 && (e.opcode == 2 || e.opcode == 18 || e.opcode == 25)
}

type selectionKey struct{ window, property uint32 }
type selectionTransfer struct {
	data    []byte
	target  uint32
	expires time.Time
}

type selectionPacket struct {
	packet x11.Packet
	err    error
}

// Only the owner loop mutates transfers or writes requests. A bounded reader
// channel lets that loop expire idle transfers even when the X server is quiet.
// Closing the connection and joining the reader keeps shutdown self-contained.
func (c *xconn) serveSelection(img Image) {
	packets := make(chan selectionPacket, 1)
	stop, readDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			p, err := c.read()
			select {
			case packets <- selectionPacket{packet: p, err: err}:
			case <-stop:
				return
			}
			if err != nil {
				var protocolErr *x11Error
				if !errors.As(err, &protocolErr) {
					return
				}
			}
		}
	}()
	defer func() { close(stop); _ = c.Close(); <-readDone }()
	clk := c.clock
	if clk == nil {
		clk = clock.New()
	}
	timer := clk.NewTimer(selectionIdleTimeout)
	defer timer.Stop()
	transfers := make(map[selectionKey]selectionTransfer)
	chunkSize := min(ChunkBytes, c.maxRequest-24)
	if chunkSize <= 0 {
		return
	}
	for {
		timer.Stop()
		select {
		case <-timer.C:
		default:
		}
		var expiry <-chan time.Time
		if next := expireSelections(transfers, clk.Now()); !next.IsZero() {
			timer.Reset(next.Sub(clk.Now()))
			expiry = timer.C
		}
		var incoming selectionPacket
		select {
		case incoming = <-packets:
		case <-expiry:
			continue
		}
		// Packet delivery and a deadline can become ready together.
		expireSelections(transfers, clk.Now())
		if incoming.err != nil {
			var protocolErr *x11Error
			if errors.As(incoming.err, &protocolErr) && protocolErr.vanishedReader() {
				cancelSelectionWindow(transfers, protocolErr.resource)
				continue
			}
			return
		}
		p := incoming.packet
		if p.EventCode() == x11.EventSelectionClear {
			return
		}
		if p.EventCode() == 17 { // DestroyNotify.window
			cancelSelectionWindow(transfers, le.Uint32(p.Raw[8:]))
			continue
		}
		if p.EventCode() == propertyNotify && p.Raw[16] == 1 {
			key := selectionKey{le.Uint32(p.Raw[4:]), le.Uint32(p.Raw[8:])}
			if tr, ok := transfers[key]; ok {
				tr.expires = clk.Now().Add(selectionIdleTimeout)
				n := min(chunkSize, len(tr.data))
				if c.send(x11.ChangeProperty(key.window, key.property, tr.target, 8, tr.data[:n])) != nil {
					return
				}
				tr.data = tr.data[n:]
				if n == 0 {
					delete(transfers, key)
				} else {
					transfers[key] = tr
				}
			}
		}
		if p.EventCode() != x11.EventSelectionRequest {
			continue
		}
		e := p.SelectionRequest()
		prop := e.Property
		if prop == 0 {
			prop = e.Target
		}
		key := selectionKey{e.Requestor, prop}
		// A conversion to the same property supersedes the previous read,
		// including at capacity or when replacing INCR with a small target.
		delete(transfers, key)
		var value []byte
		format, typ := byte(8), e.Target
		switch e.Target {
		case c.atoms["TARGETS"]:
			value = x11.AtomList(c.atoms["TARGETS"], c.atoms[img.MIME])
			format, typ = 32, x11.AtomATOM
		case c.atoms[img.MIME]:
			value = img.Data
		default:
			prop = 0
		}
		if prop != 0 && len(value) > chunkSize {
			if len(transfers) >= 8 {
				prop = 0
			} else {
				// PropertyChange drives INCR; StructureNotify reclaims readers
				// that destroy their window without completing the handshake.
				req := make([]byte, 16)
				req[0] = 2
				le.PutUint16(req[2:], 4)
				le.PutUint32(req[4:], e.Requestor)
				le.PutUint32(req[8:], 1<<11)
				le.PutUint32(req[12:], 1<<22|1<<17)
				if c.send(req) != nil {
					return
				}
				transfers[key] = selectionTransfer{data: value, target: typ, expires: clk.Now().Add(selectionIdleTimeout)}
				size := make([]byte, 4)
				le.PutUint32(size, uint32(len(value)))
				if c.send(x11.ChangeProperty(e.Requestor, prop, c.atoms["INCR"], 32, size)) != nil {
					return
				}
			}
		} else if prop != 0 {
			if c.send(x11.ChangeProperty(e.Requestor, prop, typ, format, value)) != nil {
				return
			}
		}
		if c.send(x11.SendSelectionNotify(x11.SelectionNotify{Requestor: e.Requestor, Selection: e.Selection, Target: e.Target, Property: prop, Time: e.Time})) != nil {
			return
		}
	}
}

func cancelSelectionWindow(transfers map[selectionKey]selectionTransfer, window uint32) {
	for key := range transfers {
		if key.window == window {
			delete(transfers, key)
		}
	}
}

func expireSelections(transfers map[selectionKey]selectionTransfer, now time.Time) time.Time {
	var next time.Time
	for key, tr := range transfers {
		if !now.Before(tr.expires) {
			delete(transfers, key)
		} else if next.IsZero() || tr.expires.Before(next) {
			next = tr.expires
		}
	}
	return next
}
