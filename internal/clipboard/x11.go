package clipboard

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.design/x/x11"
	"pkt.systems/lingon/internal/clock"
)

var le = binary.LittleEndian

const propertyNotify = 28

// X11 connects directly to an X server; no Cgo or Xlib is needed. Display and
// Authority are explicit so a host's private endpoint cannot replace the
// originating terminal's clipboard environment.
type X11 struct {
	Display   string
	Authority string
	mu        sync.Mutex
	owner     *xconn
	closed    bool
	clock     clock.Clock
}

// NewX11 constructs an X11 reader and selection owner.
func NewX11(display, authority string) Backend { return &X11{Display: display, Authority: authority} }

// Read prefers PNG, then JPEG, and preserves the original encoded bytes.
func (x *X11) Read(ctx context.Context) (Image, error) {
	if x.Display == "" {
		return Image{}, nil
	}
	c, err := x.connect(ctx)
	if err != nil {
		return Image{}, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	owner, err := c.reply(x11.GetSelectionOwner(c.atoms["CLIPBOARD"]))
	if err != nil {
		return Image{}, err
	}
	if owner.SelectionOwner() == 0 {
		return Image{}, nil
	}
	for _, mime := range []string{"image/png", "image/jpeg"} {
		prop := c.atoms["LINGON_CLIPBOARD"]
		target := c.atoms[mime]
		if err := c.send(x11.ConvertSelection(c.win, c.atoms["CLIPBOARD"], target, prop, 0)); err != nil {
			return Image{}, err
		}
		for {
			p, err := c.read()
			if err != nil {
				return Image{}, err
			}
			if p.EventCode() != x11.EventSelectionNotify {
				continue
			}
			e := p.SelectionNotify()
			if e.Requestor != c.win || e.Target != target {
				continue
			}
			if e.Property == 0 {
				break
			}
			data, err := c.readSelection(prop, target)
			if err != nil {
				return Image{}, err
			}
			img := Image{MIME: mime, Data: data}
			if err := img.Validate(); err != nil {
				return Image{}, err
			}
			return img, nil
		}
	}
	return Image{}, nil
}

// Set synchronously acquires and confirms selection ownership before returning.
// The connection remains alive until replacement, selection loss, or Close.
func (x *X11) Set(ctx context.Context, img Image) error {
	if err := img.Validate(); err != nil {
		return err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closed {
		return net.ErrClosed
	}
	c, err := x.connect(ctx)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	if err = c.send(x11.SetSelectionOwner(c.win, c.atoms["CLIPBOARD"], 0)); err != nil {
		_ = c.Close()
		return err
	}
	p, err := c.reply(x11.GetSelectionOwner(c.atoms["CLIPBOARD"]))
	if err != nil || p.SelectionOwner() != c.win {
		_ = c.Close()
		if err != nil {
			return fmt.Errorf("could not acquire X11 clipboard: %w", err)
		}
		return fmt.Errorf("could not acquire X11 clipboard ownership")
	}
	if err := ctx.Err(); err != nil {
		_ = c.Close()
		return err
	}
	_ = c.net.SetDeadline(time.Time{})
	old := x.owner
	x.owner = c
	if old != nil {
		_ = old.Close()
	}
	owned := Image{MIME: img.MIME, Data: append([]byte(nil), img.Data...)}
	go c.serveSelection(owned)
	return nil
}

// Close releases the owned X11 selection.
func (x *X11) Close() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.closed = true
	if x.owner != nil {
		err := x.owner.Close()
		x.owner = nil
		return err
	}
	return nil
}

type xconn struct {
	net        net.Conn
	r          *bufio.Reader
	seq        uint16
	win        uint32
	atoms      map[string]uint32
	maxRequest int
	clock      clock.Clock
}

func (c *xconn) Close() error { return c.net.Close() }
func (c *xconn) send(b []byte) error {
	_, err := c.net.Write(b)
	c.seq++
	return err
}

// read bounds packet allocation before reading any advertised body.
func (c *xconn) read() (x11.Packet, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(c.r, b); err != nil {
		return x11.Packet{}, err
	}
	if b[0] == 1 || b[0]&127 == 35 {
		n := uint64(le.Uint32(b[4:])) * 4
		if n > 1<<20 {
			return x11.Packet{}, fmt.Errorf("oversized X11 packet")
		}
		b = append(b, make([]byte, int(n))...)
		if _, err := io.ReadFull(c.r, b[32:]); err != nil {
			return x11.Packet{}, err
		}
	}
	if b[0] == 0 {
		return x11.Packet{}, &x11Error{code: b[1], opcode: b[10], resource: le.Uint32(b[4:])}
	}
	return x11.Packet{Raw: b}, nil
}

func (c *xconn) reply(req []byte) (x11.Packet, error) {
	if err := c.send(req); err != nil {
		return x11.Packet{}, err
	}
	seq := c.seq
	for {
		p, err := c.read()
		if err != nil {
			return p, err
		}
		if p.IsReply() && p.Sequence() == seq {
			return p, nil
		}
	}
}

func (x *X11) connect(ctx context.Context) (*xconn, error) {
	d, err := parseDisplay(x.Display)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	n, err := dialer.DialContext(ctx, d.Net, d.Addr)
	if err != nil && d.Net == "unix" && !strings.HasPrefix(x.Display, "/") && runtime.GOOS == "linux" {
		n, err = dialer.DialContext(ctx, "unix", "@/tmp/.X11-unix/X"+strconv.Itoa(d.Num))
	}
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = n.Close()
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	if dl, has := ctx.Deadline(); has && dl.Before(deadline) {
		deadline = dl
	}
	_ = n.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = n.Close() })
	defer stop()
	authPath := x.Authority
	if authPath == "" {
		home, _ := os.UserHomeDir()
		authPath = filepath.Join(home, ".Xauthority")
	}
	var name string
	var cookie []byte
	if data, err := os.ReadFile(authPath); err == nil {
		entries, err := x11.ParseXauthority(data)
		if err != nil {
			return nil, err
		}
		host, _ := os.Hostname()
		name, cookie = chooseCookie(entries, d.Num, n.RemoteAddr(), host)
	}
	if _, err := n.Write(x11.SetupRequest(name, cookie)); err != nil {
		return nil, err
	}
	r := bufio.NewReader(n)
	s, err := x11.ReadSetup(r)
	if err != nil {
		return nil, err
	}
	c := &xconn{net: n, r: r, win: x11.NewIDGen(s).Next(), atoms: make(map[string]uint32), maxRequest: int(s.MaxReqLen) * 4, clock: x.clock}
	// Select property notifications for INCR transfers.
	req := x11.CreateWindow(c.win, s.Root)
	le.PutUint32(req[28:], 1<<11)
	req = append(req, make([]byte, 4)...)
	le.PutUint16(req[2:], uint16(len(req)/4))
	le.PutUint32(req[32:], 1<<22)
	if err := c.send(req); err != nil {
		return nil, err
	}
	for _, atom := range []string{"CLIPBOARD", "image/png", "image/jpeg", "LINGON_CLIPBOARD", "TARGETS", "INCR", "TIMESTAMP"} {
		p, err := c.reply(x11.InternAtom(atom, false))
		if err != nil {
			return nil, err
		}
		c.atoms[atom] = p.Atom()
	}
	ok = true
	return c, nil
}

// XQuartz launchd displays name a Unix socket including the display suffix.
// Retain the display number for cookie selection and strip only a screen suffix.
func parseDisplay(display string) (x11.Display, error) {
	d, err := x11.ParseDisplay(display)
	if err != nil {
		return d, err
	}
	if strings.HasPrefix(display, "/") {
		d.Net, d.Addr = "unix", display
		suffix := strings.LastIndexByte(display, ':') + 1
		if screen := strings.IndexByte(display[suffix:], '.'); screen >= 0 {
			d.Addr = display[:suffix+screen]
		}
	}
	return d, nil
}

func (c *xconn) readProperty(prop, target uint32) ([]byte, bool, error) {
	var data []byte
	for offset := uint32(0); ; {
		p, err := c.reply(x11.GetProperty(false, c.win, prop, 0, offset, ChunkBytes/4))
		if err != nil {
			return nil, false, err
		}
		if offset == 0 && le.Uint32(p.Raw[8:]) == c.atoms["INCR"] && p.Raw[1] == 32 {
			return nil, true, nil
		}
		v := p.PropertyValue()
		if p.Raw[1] != 8 || le.Uint32(p.Raw[8:]) != target || uint64(le.Uint32(p.Raw[16:])) > uint64(len(p.Raw)-32) || len(v) > MaxBytes-len(data) {
			return nil, false, fmt.Errorf("invalid X11 image property")
		}
		data = append(data, v...)
		if le.Uint32(p.Raw[12:]) == 0 {
			return data, false, nil
		}
		if len(v) == 0 || len(v)%4 != 0 {
			return nil, false, fmt.Errorf("invalid X11 property pagination")
		}
		offset += uint32(len(v) / 4)
	}
}

func (c *xconn) readSelection(prop, target uint32) ([]byte, error) {
	data, incr, err := c.readProperty(prop, target)
	if err != nil || !incr {
		return data, err
	}
	if err := c.send(x11.DeleteProperty(c.win, prop)); err != nil {
		return nil, err
	}
	data = nil
	for {
		p, err := c.read()
		if err != nil {
			return nil, err
		}
		if p.EventCode() != propertyNotify || le.Uint32(p.Raw[4:]) != c.win || le.Uint32(p.Raw[8:]) != prop || p.Raw[16] != 0 {
			continue
		}
		part, nested, err := c.readProperty(prop, target)
		if err != nil {
			return nil, err
		}
		if nested || len(part) > MaxBytes-len(data) {
			return nil, fmt.Errorf("oversized X11 INCR image")
		}
		if err := c.send(x11.DeleteProperty(c.win, prop)); err != nil {
			return nil, err
		}
		if len(part) == 0 {
			return data, nil
		}
		data = append(data, part...)
	}
}
