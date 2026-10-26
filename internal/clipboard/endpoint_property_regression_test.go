package clipboard

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"testing"

	"golang.design/x/x11"
)

// Read independently of Lingon's bounded client parser: standard X11 consumers
// can ask for an entire image in a single property reply, larger than 1 MiB.
func endpointReply(t *testing.T, c *xconn, request []byte) x11.Packet {
	t.Helper()
	if err := c.send(request); err != nil {
		t.Fatal(err)
	}
	for {
		b := make([]byte, 32)
		if _, err := io.ReadFull(c.r, b); err != nil {
			t.Fatal(err)
		}
		if b[0] == 1 {
			n := uint64(le.Uint32(b[4:])) * 4
			if n > MaxBytes {
				t.Fatal("property reply exceeds image bound")
			}
			b = append(b, make([]byte, int(n))...)
			if _, err := io.ReadFull(c.r, b[32:]); err != nil {
				t.Fatal(err)
			}
		}
		p := x11.Packet{Raw: b}
		if !p.IsEvent() && p.Sequence() == c.seq {
			return p
		}
	}
}

func TestClipboardEndpointWholePropertyRead(t *testing.T) {
	for _, size := range []int{1<<20 + 3, MaxBytes} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			e := regressionEndpoint(t)
			c := endpointWireClient(t, e, x11.FamilyLocal)
			selection := endpointAtom(t, c, "CLIPBOARD", false)
			target := endpointAtom(t, c, "image/png", false)
			property := endpointAtom(t, c, "WHOLE_IMAGE", false)
			img := testImage(t, "image/png", false)
			img.Data = append(img.Data, make([]byte, size-len(img.Data))...)
			if err := e.Set(context.Background(), img); err != nil {
				t.Fatal(err)
			}
			endpointConvert(t, c, selection, target, property)
			// This is the standard single-read request used by whole-value clients.
			p := endpointReply(t, c, x11.GetProperty(false, c.win, property, target, 0, ^uint32(0)))
			if !p.IsReply() || p.Raw[1] != 8 || le.Uint32(p.Raw[8:]) != target {
				t.Fatal("invalid image property reply")
			}
			if le.Uint32(p.Raw[12:]) != 0 || !bytes.Equal(p.PropertyValue(), img.Data) {
				t.Fatalf("whole property was truncated: got %d bytes, want %d", len(p.PropertyValue()), len(img.Data))
			}
			if len(p.Raw[32:]) != (size+3)&^3 {
				t.Fatal("incorrect reply padding")
			}
		})
	}
}

func TestClipboardEndpointPropertyReadLengths(t *testing.T) {
	e := regressionEndpoint(t)
	c := endpointWireClient(t, e, x11.FamilyLocal)
	selection := endpointAtom(t, c, "CLIPBOARD", false)
	target := endpointAtom(t, c, "image/png", false)
	property := endpointAtom(t, c, "LENGTH_IMAGE", false)
	img := testImage(t, "image/png", true)
	if err := e.Set(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	endpointConvert(t, c, selection, target, property)
	for _, length := range []uint32{0, 1, 20000, ^uint32(0)} {
		p := endpointReply(t, c, x11.GetProperty(false, c.win, property, target, 1, length))
		want := min(uint64(length)*4, uint64(len(img.Data)-4))
		if !p.IsReply() || !bytes.Equal(p.PropertyValue(), img.Data[4:4+int(want)]) || le.Uint32(p.Raw[12:]) != uint32(len(img.Data)-4)-uint32(want) {
			t.Fatalf("property length %d was not honored", length)
		}
	}
	partial := endpointReply(t, c, x11.GetProperty(true, c.win, property, target, 0, 1))
	if len(partial.PropertyValue()) != 4 || le.Uint32(partial.Raw[12:]) == 0 {
		t.Fatal("partial delete read malformed")
	}
	full := endpointReply(t, c, x11.GetProperty(true, c.win, property, target, 0, ^uint32(0)))
	if !bytes.Equal(full.PropertyValue(), img.Data) {
		t.Fatal("partial read deleted or truncated property")
	}
	missing := endpointReply(t, c, x11.GetProperty(false, c.win, property, target, 0, ^uint32(0)))
	if le.Uint32(missing.Raw[8:]) != 0 || len(missing.PropertyValue()) != 0 {
		t.Fatal("completed delete read retained the property")
	}
}

func TestClipboardEndpointAtomNameDiscovery(t *testing.T) {
	for _, mime := range []string{"image/png", "image/jpeg"} {
		t.Run(mime, func(t *testing.T) {
			e := regressionEndpoint(t)
			c := endpointWireClient(t, e, x11.FamilyLocal)
			if err := e.Set(context.Background(), testImage(t, mime, false)); err != nil {
				t.Fatal(err)
			}
			selection := endpointAtom(t, c, "CLIPBOARD", false)
			targets := endpointAtom(t, c, "TARGETS", false)
			property := endpointAtom(t, c, "DISCOVERY_NAMES", false)
			endpointConvert(t, c, selection, targets, property)
			p := endpointReply(t, c, x11.GetProperty(false, c.win, property, x11.AtomATOM, 0, 32))
			var names []string
			for offset := 0; offset < len(p.PropertyValue()); offset += 4 {
				atom := le.Uint32(p.PropertyValue()[offset:])
				name := endpointReply(t, c, x11.GetAtomName(atom))
				if !name.IsReply() {
					t.Fatalf("GetAtomName rejected advertised atom %d", atom)
				}
				names = append(names, name.AtomName())
			}
			if len(names) != 2 || names[0] != "TARGETS" || names[1] != mime {
				t.Fatalf("bad discovery names: %q", names)
			}
			for _, name := range []string{"ATOM", "STRING", "CLIPBOARD", "CUSTOM_PADDED_NAME", "UTF8_🍒"} {
				atom := endpointAtom(t, c, name, false)
				p := endpointReply(t, c, x11.GetAtomName(atom))
				if !p.IsReply() || p.AtomName() != name || len(p.Raw[32:]) != (len(name)+3)&^3 {
					t.Fatalf("atom name did not round-trip: %q", name)
				}
			}
		})
	}
}

func TestClipboardEndpointUnknownAtomName(t *testing.T) {
	e := regressionEndpoint(t)
	c := endpointWireClient(t, e, x11.FamilyLocal)
	for _, id := range []uint32{0, ^uint32(0)} {
		p := endpointReply(t, c, x11.GetAtomName(id))
		if !p.IsError() || p.ErrorCode() != 5 || le.Uint32(p.Raw[4:]) != id || p.Raw[10] != 17 {
			t.Fatal("unknown atom did not return BadAtom", p.Raw)
		}
	}
	atom := endpointAtom(t, c, "STRING", false)
	p := endpointReply(t, c, x11.GetAtomName(atom))
	if !p.IsReply() || p.AtomName() != "STRING" {
		t.Fatal("BadAtom closed the connection")
	}
}
