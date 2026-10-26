package clipboard

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.design/x/x11"
)

// These clients connect only to the test-owned endpoint. Unlike X11.connect,
// they do not pre-intern MIME names or accept unrelated authority families.
func endpointWireClient(t *testing.T, e *Endpoint, family uint16, inspect ...func([]byte)) *xconn {
	t.Helper()
	display, err := x11.ParseDisplay(e.Display)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(e.Authority)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := x11.ParseXauthority(data)
	if err != nil {
		t.Fatal(err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	address := string([]byte{127, 0, 0, 1})
	if family == x11.FamilyLocal {
		address = hostname
	}
	var cookie []byte
	for _, entry := range entries {
		if entry.Family == family && entry.Address == address && entry.Number == strconv.Itoa(display.Num) && entry.Name == x11.MITCookie {
			cookie = entry.Data
		}
	}
	if len(cookie) != 16 {
		t.Fatalf("no usable authority record for family %d, address %q and display %d", family, address, display.Num)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var dialer net.Dialer
	n, err := dialer.DialContext(ctx, display.Net, display.Addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if err := n.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Write(x11.SetupRequest(x11.MITCookie, cookie)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(n)
	var wire bytes.Buffer
	setup, err := x11.ReadSetup(io.TeeReader(r, &wire))
	if err != nil {
		t.Fatal("authentication failed", err)
	}
	for _, check := range inspect {
		check(wire.Bytes())
	}
	c := &xconn{net: n, r: r, win: x11.NewIDGen(setup).Next(), atoms: make(map[string]uint32)}
	if err := c.send(x11.CreateWindow(c.win, setup.Root)); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClipboardEndpointStandardClientSetup(t *testing.T) {
	e := regressionEndpoint(t)
	endpointWireClient(t, e, x11.FamilyLocal, func(wire []byte) {
		// Validate the root's advertised format/depth/visual relationships as a
		// standard X11 client does before it can create its selection window.
		setup := wire[8:]
		if len(setup) < 32 || setup[20] != 1 {
			t.Fatal("clipboard setup must describe one root screen")
		}
		formats := int(setup[21])
		if formats == 0 {
			t.Fatal("standard X11 clients require a pixmap format")
		}
		offset := 32 + (int(le.Uint16(setup[16:]))+3)&^3
		if offset+formats*8+40 > len(setup) {
			t.Fatal("truncated X11 setup")
		}
		formatData := setup[offset : offset+formats*8]
		offset += formats * 8
		root := setup[offset : offset+40]
		depth, visual := root[38], le.Uint32(root[32:])
		if depth == 0 || visual == 0 {
			t.Fatal("root lacks a default depth/visual")
		}
		formatMatches := false
		for i := 0; i < len(formatData); i += 8 {
			if formatData[i] == depth && formatData[i+1] > 0 && formatData[i+2] > 0 {
				formatMatches = true
			}
		}
		if !formatMatches {
			t.Fatal("root depth has no pixmap format")
		}
		offset += 40
		visualMatches := false
		for i := 0; i < int(root[39]); i++ {
			if offset+8 > len(setup) {
				t.Fatal("truncated depth")
			}
			d := setup[offset : offset+8]
			offset += 8
			for j := 0; j < int(le.Uint16(d[2:])); j++ {
				if offset+24 > len(setup) {
					t.Fatal("truncated visual")
				}
				if d[0] == depth && le.Uint32(setup[offset:]) == visual {
					visualMatches = true
				}
				offset += 24
			}
		}
		if !visualMatches {
			t.Fatal("root default visual is not declared")
		}
	})
}

func TestClipboardEndpointStandardClientGC(t *testing.T) {
	e := regressionEndpoint(t)
	c := endpointWireClient(t, e, x11.FamilyLocal)
	// Xlib creates a default GC while opening a display, even for clients
	// that only read selections. No drawing requests are needed or supported.
	gc := c.win + 1
	create := make([]byte, 16)
	create[0] = 55
	le.PutUint16(create[2:], 4)
	le.PutUint32(create[4:], gc)
	le.PutUint32(create[8:], 1)
	if err := c.send(create); err != nil {
		t.Fatal(err)
	}
	free := make([]byte, 8)
	free[0] = 60
	le.PutUint16(free[2:], 2)
	le.PutUint32(free[4:], gc)
	if err := c.send(free); err != nil {
		t.Fatal(err)
	}
	if _, err := c.reply(x11.GetInputFocus()); err != nil {
		t.Fatal("default GC lifecycle rejected", err)
	}
}

func endpointAtom(t *testing.T, c *xconn, name string, onlyIfExists bool) uint32 {
	t.Helper()
	p, err := c.reply(x11.InternAtom(name, onlyIfExists))
	if err != nil {
		t.Fatal(err)
	}
	return p.Atom()
}

func endpointConvert(t *testing.T, c *xconn, selection, target, property uint32) {
	t.Helper()
	if err := c.send(x11.ConvertSelection(c.win, selection, target, property, 0)); err != nil {
		t.Fatal(err)
	}
	for {
		p, err := c.read()
		if err != nil {
			t.Fatal(err)
		}
		if p.EventCode() != x11.EventSelectionNotify {
			continue
		}
		e := p.SelectionNotify()
		if e.Requestor != c.win || e.Selection != selection || e.Target != target {
			continue
		}
		if e.Property != property {
			t.Fatalf("selection conversion rejected: %+v", e)
		}
		return
	}
}

func regressionEndpoint(t *testing.T) *Endpoint {
	t.Helper()
	e, err := NewEndpoint(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	return e
}

func TestClipboardEndpointLoopbackLocalAuthority(t *testing.T) {
	// No clipboard helper executable is available or needed.
	t.Setenv("PATH", t.TempDir())
	e := regressionEndpoint(t)
	// Xlib/libxcb select FamilyLocal with the local hostname for loopback TCP.
	c := endpointWireClient(t, e, x11.FamilyLocal)
	p, err := c.reply(x11.GetInputFocus())
	if err != nil || !p.IsReply() {
		t.Fatal("local authority did not authenticate", err)
	}
	// The explicit IPv4 record remains usable by direct protocol clients too.
	c = endpointWireClient(t, e, x11.FamilyInternet)
	if _, err := c.reply(x11.GetInputFocus()); err != nil {
		t.Fatal(err)
	}
	img := testImage(t, "image/png", false)
	if err := e.Set(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	got, err := NewX11(e.Display, e.Authority).Read(context.Background())
	if err != nil || !bytes.Equal(got.Data, img.Data) {
		t.Fatal("native clipboard read failed with an empty PATH", err)
	}
}

func TestClipboardEndpointRepeatedConversionPreservesPagination(t *testing.T) {
	for _, mime := range []string{"image/png", "image/jpeg"} {
		t.Run(mime, func(t *testing.T) {
			e := regressionEndpoint(t)
			c := endpointWireClient(t, e, x11.FamilyInternet)
			selection := endpointAtom(t, c, "CLIPBOARD", false)
			propertyA := endpointAtom(t, c, "TRANSFER_A", false)
			propertyB := endpointAtom(t, c, "TRANSFER_B", false)
			png := endpointAtom(t, c, "image/png", false)
			targetB := endpointAtom(t, c, mime, false)
			a := testImage(t, "image/png", true)
			b := testImage(t, mime, false)
			if err := e.Set(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			endpointConvert(t, c, selection, png, propertyA)
			first, err := c.reply(x11.GetProperty(false, c.win, propertyA, png, 0, ChunkBytes/4))
			if err != nil {
				t.Fatal(err)
			}
			if le.Uint32(first.Raw[12:]) == 0 {
				t.Fatal("fixture did not require pagination")
			}
			if err := e.Set(context.Background(), b); err != nil {
				t.Fatal(err)
			}
			endpointConvert(t, c, selection, targetB, propertyB)
			gotB, _, err := c.readProperty(propertyB, targetB)
			if err != nil || !bytes.Equal(gotB, b.Data) {
				t.Fatal("reused connection did not receive image B", err)
			}
			gotA, _, err := c.readProperty(propertyA, png)
			if err != nil || !bytes.Equal(gotA, a.Data) {
				t.Fatal("publication of B changed A's paginated property", err)
			}
			// Reusing the same property also starts a fresh transfer.
			if err := e.Set(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			endpointConvert(t, c, selection, png, propertyB)
			gotA, _, err = c.readProperty(propertyB, png)
			if err != nil || !bytes.Equal(gotA, a.Data) {
				t.Fatal("reused property did not receive the new image", err)
			}
		})
	}
}

func TestClipboardEndpointTargetsBeforeMIMEIntern(t *testing.T) {
	for _, mime := range []string{"image/png", "image/jpeg", ""} {
		t.Run(mime, func(t *testing.T) {
			e := regressionEndpoint(t)
			var img Image
			if mime != "" {
				img = testImage(t, mime, false)
				if err := e.Set(context.Background(), img); err != nil {
					t.Fatal(err)
				}
			}
			c := endpointWireClient(t, e, x11.FamilyInternet)
			selection := endpointAtom(t, c, "CLIPBOARD", false)
			targets := endpointAtom(t, c, "TARGETS", false)
			property := endpointAtom(t, c, "DISCOVER", false)
			endpointConvert(t, c, selection, targets, property)
			p, err := c.reply(x11.GetProperty(false, c.win, property, x11.AtomATOM, 0, 32))
			if err != nil {
				t.Fatal(err)
			}
			if p.Raw[1] != 32 || le.Uint32(p.Raw[8:]) != x11.AtomATOM {
				t.Fatal("TARGETS must be a list of atoms")
			}
			value := p.PropertyValue()
			wantCount := 1
			if mime != "" {
				wantCount++
			}
			if len(value) != wantCount*4 {
				t.Fatalf("TARGETS contains %d atoms, want %d", len(value)/4, wantCount)
			}
			for off := 0; off < len(value); off += 4 {
				if le.Uint32(value[off:]) == 0 {
					t.Fatal("TARGETS advertised atom None")
				}
			}
			if le.Uint32(value) != targets {
				t.Fatal("TARGETS failed to advertise itself")
			}
			if mime == "" {
				return
			}
			// Resolve only an existing name, without creating the missing atom.
			mimeAtom := endpointAtom(t, c, mime, true)
			if mimeAtom == 0 || le.Uint32(value[4:]) != mimeAtom {
				t.Fatal("image MIME was not interned before discovery")
			}
			endpointConvert(t, c, selection, mimeAtom, property)
			got, _, err := c.readProperty(property, mimeAtom)
			if err != nil || !bytes.Equal(got, img.Data) {
				t.Fatal("advertised target could not be read", err)
			}
		})
	}
}
