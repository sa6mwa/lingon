package clipboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.design/x/x11"
	"pkt.systems/lingon/internal/clock"
)

// Endpoint exposes a selection to a shell. With an inherited display it uses
// that X server. Otherwise it serves only the X11 clipboard subset, with a
// per-session cookie and loopback listener; it is not a graphical X server.
type Endpoint struct {
	Display   string
	Authority string
	selection Selection
}

// NewEndpoint prepares the clipboard environment before starting a child shell.
func NewEndpoint(ctx context.Context, display, authority string) (*Endpoint, error) {
	if display != "" {
		return &Endpoint{Display: display, Authority: authority, selection: NewX11(display, authority)}, nil
	}
	s, err := newServer(ctx)
	if err != nil {
		return nil, err
	}
	return &Endpoint{Display: s.display, Authority: s.authority, selection: s}, nil
}

// Env supplies the endpoint to a child without turning an unset inherited
// XAUTHORITY into an empty filename, which disables Xlib's default cookie lookup.
func (e *Endpoint) Env(environ []string) []string {
	environ = append(environ, "DISPLAY="+e.Display)
	if e.Authority != "" {
		environ = append(environ, "XAUTHORITY="+e.Authority)
	}
	return environ
}

// Set publishes a validated image before the caller delivers Ctrl+V.
func (e *Endpoint) Set(ctx context.Context, img Image) error { return e.selection.Set(ctx, img) }

// Close releases owned selections, sockets and private authorization files.
func (e *Endpoint) Close() error { return e.selection.Close() }

type clipboardServer struct {
	mu                            sync.Mutex
	image                         Image
	listener                      net.Listener
	connections                   map[net.Conn]struct{}
	closed                        bool
	display, authority, directory string
	cookie                        [16]byte
	wg                            sync.WaitGroup
	closeOnce                     sync.Once
	closeErr                      error
	clock                         clock.Clock
}

func newServer(ctx context.Context) (*clipboardServer, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	if port <= 6000 {
		_ = l.Close()
		return nil, fmt.Errorf("invalid clipboard endpoint port")
	}
	s := &clipboardServer{listener: l, connections: make(map[net.Conn]struct{}), display: "127.0.0.1:" + strconv.Itoa(port-6000), clock: clock.New()}
	if _, err := rand.Read(s.cookie[:]); err != nil {
		_ = l.Close()
		return nil, err
	}
	s.directory, err = os.MkdirTemp("", "lingon-clipboard-")
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	s.authority = filepath.Join(s.directory, "Xauthority")
	hostname, err := os.Hostname()
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("get clipboard authority hostname: %w", err)
	}
	// Xauthority uses big endian regardless of the X11 client's byte order.
	// Xlib/libxcb use FamilyLocal for loopback TCP; direct wire clients can
	// also use the explicit IPv4 record. Both records require the same cookie.
	var data []byte
	for _, entry := range []struct {
		family  uint16
		address []byte
	}{
		{x11.FamilyInternet, []byte{127, 0, 0, 1}},
		{x11.FamilyLocal, []byte(hostname)},
	} {
		data = binary.BigEndian.AppendUint16(data, entry.family)
		for _, field := range [][]byte{entry.address, []byte(strconv.Itoa(port - 6000)), []byte(x11.MITCookie), s.cookie[:]} {
			data = binary.BigEndian.AppendUint16(data, uint16(len(field)))
			data = append(data, field...)
		}
	}
	if err := os.WriteFile(s.authority, data, 0600); err != nil {
		_ = s.Close()
		return nil, err
	}
	s.wg.Add(1)
	go s.accept()
	context.AfterFunc(ctx, func() { _ = s.Close() })
	return s, nil
}

func (s *clipboardServer) Set(ctx context.Context, img Image) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := img.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.image = Image{MIME: img.MIME, Data: append([]byte(nil), img.Data...)}
	return nil
}

func (s *clipboardServer) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.image = Image{}
		_ = s.listener.Close()
		for c := range s.connections {
			_ = c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
		s.closeErr = os.RemoveAll(s.directory)
	})
	return s.closeErr
}

func (s *clipboardServer) accept() {
	defer s.wg.Done()
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed || len(s.connections) >= 8 {
			s.mu.Unlock()
			_ = c.Close()
			continue
		}
		s.connections[c] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			defer c.Close()
			defer func() { s.mu.Lock(); delete(s.connections, c); s.mu.Unlock() }()
			s.serve(c)
		}()
	}
}

// clipboardSetup describes one minimal valid screen for clients that validate
// X11 setup before using selections. This does not enable graphics requests.
func clipboardSetup(order binary.ByteOrder) []byte {
	setup := make([]byte, 128)
	setup[0] = 1
	order.PutUint16(setup[2:], 11)
	order.PutUint16(setup[6:], uint16((len(setup)-8)/4))
	order.PutUint32(setup[8:], 1)
	order.PutUint32(setup[12:], 0x200000)
	order.PutUint32(setup[16:], 0x1fffff)
	order.PutUint16(setup[24:], 6)
	order.PutUint16(setup[26:], 65535)
	setup[28], setup[29] = 1, 1   // one root and one pixmap format
	setup[32], setup[33] = 32, 32 // bitmap scanline unit and pad
	setup[34], setup[35] = 8, 255
	copy(setup[40:], "lingon")
	setup[48], setup[49], setup[50] = 24, 32, 32 // depth, bits/pixel, scanline pad
	root := setup[56:96]
	order.PutUint32(root, 1)
	order.PutUint32(root[4:], 2) // synthetic default colormap
	order.PutUint32(root[8:], 0xffffff)
	for _, offset := range []int{20, 22, 24, 26, 28, 30} {
		order.PutUint16(root[offset:], 1) // geometry and installed colormap counts
	}
	order.PutUint32(root[32:], 3) // default visual
	root[38], root[39] = 24, 1    // root depth and one allowed depth
	depth := setup[96:104]
	depth[0] = 24
	order.PutUint16(depth[2:], 1)
	visual := setup[104:]
	order.PutUint32(visual, 3)
	visual[4], visual[5] = 4, 8 // TrueColor, bits per RGB value
	order.PutUint16(visual[6:], 256)
	order.PutUint32(visual[8:], 0xff0000)
	order.PutUint32(visual[12:], 0x00ff00)
	order.PutUint32(visual[16:], 0x0000ff)
	return setup
}

// serve implements selection-related core requests and the minimal resource
// initialization used by Xlib. Unsupported requests receive BadRequest; no
// request opens another display, draws graphics, or executes a command.
func (s *clipboardServer) serve(c net.Conn) {
	_ = c.SetDeadline(s.clock.Now().Add(10 * time.Second))
	h := make([]byte, 12)
	if _, err := io.ReadFull(c, h); err != nil {
		return
	}
	var order binary.ByteOrder
	switch h[0] {
	case 'l':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return
	}
	nameLen, cookieLen := int(order.Uint16(h[6:])), int(order.Uint16(h[8:]))
	if nameLen != 18 || cookieLen != 16 || order.Uint16(h[2:]) != 11 {
		return
	}
	auth := make([]byte, 36)
	if _, err := io.ReadFull(c, auth); err != nil || string(auth[:18]) != "MIT-MAGIC-COOKIE-1" || subtle.ConstantTimeCompare(auth[20:], s.cookie[:]) != 1 {
		return
	}
	if _, err := c.Write(clipboardSetup(order)); err != nil {
		return
	}
	// Supported selection targets must exist before any client queries TARGETS.
	atoms := map[string]uint32{"ATOM": 4, "STRING": 31, "CLIPBOARD": 100, "TARGETS": 101, "image/png": 102, "image/jpeg": 103}
	nextAtom := uint32(104)
	type property struct {
		typ    uint32
		format byte
		data   []byte
	}
	properties := make(map[[2]uint32]property)
	// Clipboard libraries can join their event reader after DestroyWindow.
	// Retain StructureNotify subscriptions so that join can complete even
	// though this endpoint does not implement graphical window management.
	structureWindows := make(map[uint32]bool)
	storeProperty := func(key [2]uint32, value property) bool {
		if _, exists := properties[key]; !exists && len(properties) >= 32 {
			return false
		}
		properties[key] = value
		return true
	}
	var seq uint16
	packet := func(kind byte) []byte { b := make([]byte, 32); b[0] = kind; order.PutUint16(b[2:], seq); return b }
	propertyEvent := func(window, prop uint32, state byte) bool {
		b := packet(28)
		order.PutUint32(b[4:], window)
		order.PutUint32(b[8:], prop)
		order.PutUint32(b[12:], 1)
		b[16] = state
		_, err := c.Write(b)
		return err == nil
	}
	for {
		// An authenticated application may keep its clipboard connection idle.
		// Start the request timeout only once it begins sending another packet.
		_ = c.SetDeadline(time.Time{})
		head := make([]byte, 4)
		if _, err := io.ReadFull(c, head[:1]); err != nil {
			return
		}
		_ = c.SetDeadline(s.clock.Now().Add(30 * time.Second))
		if _, err := io.ReadFull(c, head[1:]); err != nil {
			return
		}
		length := int(order.Uint16(head[2:])) * 4
		if length < 4 || length > ChunkBytes+32 {
			return
		}
		req := make([]byte, length)
		copy(req, head)
		if _, err := io.ReadFull(c, req[4:]); err != nil {
			return
		}
		seq++
		u32 := func(off int) uint32 {
			if off+4 > len(req) {
				return 0
			}
			return order.Uint32(req[off:])
		}
		var out []byte
		switch req[0] {
		case 1, 2: // CreateWindow, ChangeWindowAttributes
			maskOffset, valuesOffset := 28, 32
			if req[0] == 2 {
				maskOffset, valuesOffset = 8, 12
			}
			if len(req) < valuesOffset {
				return
			}
			mask := u32(maskOffset)
			if len(req) != valuesOffset+4*bits.OnesCount32(mask) {
				return
			}
			window := u32(4)
			if req[0] == 1 {
				delete(structureWindows, window)
			}
			if mask&(1<<11) != 0 { // CWEventMask's value follows lower mask bits.
				offset := valuesOffset + 4*bits.OnesCount32(mask&((1<<11)-1))
				if u32(offset)&(1<<17) != 0 {
					if !structureWindows[window] && len(structureWindows) >= 128 {
						out = packet(0)
						out[1], out[10] = 11, req[0] // BadAlloc
						break
					}
					structureWindows[window] = true
				} else {
					delete(structureWindows, window)
				}
			}
		case 55, 60, 127: // default GC lifecycle, NoOperation
		case 4: // DestroyWindow releases the window's retained selection buffers.
			if len(req) != 8 {
				return
			}
			for key := range properties {
				if key[0] == u32(4) {
					delete(properties, key)
				}
			}
			if structureWindows[u32(4)] {
				out = packet(17)                 // DestroyNotify releases arboard's event reader.
				order.PutUint32(out[4:], u32(4)) // event and window are identical
				order.PutUint32(out[8:], u32(4))
				delete(structureWindows, u32(4))
			}
		case 16:
			if len(req) < 8 {
				return
			}
			n := int(order.Uint16(req[4:]))
			if n > len(req)-8 {
				return
			}
			name := string(req[8 : 8+n])
			atom := atoms[name]
			if atom == 0 && req[1] == 0 {
				if len(atoms) >= 128 {
					return
				}
				atom = nextAtom
				nextAtom++
				atoms[name] = atom
			}
			out = packet(1)
			order.PutUint32(out[8:], atom)
		case 17: // GetAtomName
			if len(req) != 8 {
				return
			}
			atom := u32(4)
			var name string
			found := false
			for candidate, id := range atoms {
				if id == atom {
					name, found = candidate, true
					break
				}
			}
			if !found {
				out = packet(0)
				out[1], out[10] = 5, req[0] // BadAtom
				order.PutUint32(out[4:], atom)
				break
			}
			out = packet(1)
			order.PutUint16(out[8:], uint16(len(name)))
			out = append(out, name...)
			for len(out)%4 != 0 {
				out = append(out, 0)
			}
			order.PutUint32(out[4:], uint32(len(out)-32)/4)
		case 18:
			if len(req) < 24 {
				return
			}
			n := uint64(u32(20)) * uint64(req[16]) / 8
			if n > uint64(len(req)-24) || n > ChunkBytes || (req[16] != 8 && req[16] != 32) {
				return
			}
			key := [2]uint32{u32(4), u32(8)}
			if !storeProperty(key, property{typ: u32(12), format: req[16], data: append([]byte(nil), req[24:24+int(n)]...)}) {
				out = packet(0)
				out[1], out[10] = 11, req[0] // BadAlloc
				break
			}
			if !propertyEvent(key[0], key[1], 0) {
				return
			}
		case 19:
			if len(req) != 12 {
				return
			}
			key := [2]uint32{u32(4), u32(8)}
			delete(properties, key)
			if !propertyEvent(key[0], key[1], 1) {
				return
			}
		case 20:
			if len(req) != 24 {
				return
			}
			key := [2]uint32{u32(4), u32(8)}
			p := properties[key]
			out = packet(1)
			if p.typ != 0 {
				offset := uint64(u32(16)) * 4
				if offset > uint64(len(p.data)) {
					return
				}
				// Honor X11's requested length. The image bound limits the reply;
				// relay chunk sizes do not constrain local X11 property reads.
				n := min(uint64(u32(20))*4, uint64(len(p.data))-offset)
				out[1] = p.format
				order.PutUint32(out[8:], p.typ)
				order.PutUint32(out[12:], uint32(uint64(len(p.data))-offset-n))
				order.PutUint32(out[16:], uint32(n)*8/uint32(p.format))
				out = append(out, p.data[int(offset):int(offset+n)]...)
				for len(out)%4 != 0 {
					out = append(out, 0)
				}
				order.PutUint32(out[4:], uint32(len(out)-32)/4)
				if req[1] != 0 && offset+n == uint64(len(p.data)) {
					delete(properties, key)
				}
			}
		case 23:
			out = packet(1)
			s.mu.Lock()
			has := len(s.image.Data) > 0
			s.mu.Unlock()
			if has {
				order.PutUint32(out[8:], 1)
			}
		case 24:
			if len(req) != 24 {
				return
			}
			// Each conversion sees the current image. Existing properties retain
			// their immutable data so concurrent pagination cannot mix images.
			s.mu.Lock()
			captured := s.image
			s.mu.Unlock()
			prop := u32(16)
			target := u32(12)
			if prop == 0 {
				prop = target
			}
			var value property
			if u32(8) != atoms["CLIPBOARD"] {
				prop = 0
			} else if target == atoms[captured.MIME] && len(captured.Data) > 0 {
				value = property{typ: target, format: 8, data: captured.Data}
			} else if target == atoms["TARGETS"] {
				data := make([]byte, 4)
				order.PutUint32(data, atoms["TARGETS"])
				if len(captured.Data) > 0 {
					data = append(data, 0, 0, 0, 0)
					order.PutUint32(data[4:], atoms[captured.MIME])
				}
				value = property{typ: 4, format: 32, data: data}
			} else {
				prop = 0
			}
			if prop != 0 && !storeProperty([2]uint32{u32(4), prop}, value) {
				prop = 0
			}
			out = packet(31)
			order.PutUint32(out[4:], u32(20))
			order.PutUint32(out[8:], u32(4))
			order.PutUint32(out[12:], u32(8))
			order.PutUint32(out[16:], target)
			order.PutUint32(out[20:], prop)
		case 43:
			out = packet(1)
			order.PutUint32(out[8:], 1)
		case 98:
			out = packet(1) // no extensions
		default:
			out = packet(0)
			out[1] = 1
			out[10] = req[0]
		}
		if len(out) > 0 {
			if _, err := c.Write(out); err != nil {
				return
			}
		}
	}
}
