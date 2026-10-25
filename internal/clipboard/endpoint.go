package clipboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
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
	s := &clipboardServer{listener: l, connections: make(map[net.Conn]struct{}), display: "127.0.0.1:" + strconv.Itoa(port-6000)}
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
	// Xauthority uses big endian regardless of the X11 client's byte order.
	data := []byte{0, 0}
	for _, field := range [][]byte{{127, 0, 0, 1}, []byte(strconv.Itoa(port - 6000)), []byte("MIT-MAGIC-COOKIE-1"), s.cookie[:]} {
		data = binary.BigEndian.AppendUint16(data, uint16(len(field)))
		data = append(data, field...)
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

// serve implements only selection-related core requests. Unsupported requests
// receive BadRequest; no request opens another display or executes a command.
func (s *clipboardServer) serve(c net.Conn) {
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
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
	setup := make([]byte, 80)
	setup[0] = 1
	order.PutUint16(setup[2:], 11)
	order.PutUint16(setup[6:], 18)
	order.PutUint32(setup[12:], 0x200000)
	order.PutUint32(setup[16:], 0x1fffff)
	order.PutUint16(setup[26:], 65535)
	setup[28] = 1
	setup[34] = 8
	setup[35] = 255
	order.PutUint32(setup[40:], 1) // one synthetic root, no graphics/depths
	if _, err := c.Write(setup); err != nil {
		return
	}
	atoms := map[string]uint32{"ATOM": 4, "STRING": 31}
	nextAtom := uint32(100)
	type property struct {
		typ    uint32
		format byte
		data   []byte
	}
	properties := make(map[[2]uint32]property)
	var captured Image
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
		_ = c.SetDeadline(time.Now().Add(30 * time.Second))
		head := make([]byte, 4)
		if _, err := io.ReadFull(c, head); err != nil {
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
		case 1, 2, 4, 127: // create/change/destroy window, NoOperation
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
		case 18:
			if len(req) < 24 {
				return
			}
			n := uint64(u32(20)) * uint64(req[16]) / 8
			if n > uint64(len(req)-24) || n > ChunkBytes || (req[16] != 8 && req[16] != 32) || len(properties) >= 32 {
				return
			}
			key := [2]uint32{u32(4), u32(8)}
			properties[key] = property{typ: u32(12), format: req[16], data: append([]byte(nil), req[24:24+int(n)]...)}
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
				n := min(uint64(u32(20))*4, uint64(ChunkBytes), uint64(len(p.data))-offset)
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
			// Capture one immutable image for this requestor, including pagination.
			if captured.Data == nil {
				s.mu.Lock()
				captured = s.image
				s.mu.Unlock()
			}
			prop := u32(16)
			target := u32(12)
			if prop == 0 {
				prop = target
			}
			if u32(8) != atoms["CLIPBOARD"] {
				prop = 0
			} else if target == atoms[captured.MIME] && len(captured.Data) > 0 {
				properties[[2]uint32{u32(4), prop}] = property{typ: target, format: 8, data: captured.Data}
			} else if target == atoms["TARGETS"] {
				value := make([]byte, 8)
				order.PutUint32(value, atoms["TARGETS"])
				order.PutUint32(value[4:], atoms[captured.MIME])
				properties[[2]uint32{u32(4), prop}] = property{typ: 4, format: 32, data: value}
			} else {
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
