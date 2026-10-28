package session_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"golang.design/x/x11"
)

// Model arboard's reader lifetime: its event thread subscribes to destruction,
// and dropping the clipboard destroys its window then joins that thread. This
// fixture connects only to the private endpoint of the test-owned child shell.
func clipboardReaderShutdown(ctx context.Context) (func() error, error) {
	display, err := x11.ParseDisplay(os.Getenv("DISPLAY"))
	if err != nil {
		return nil, err
	}
	auth, err := os.ReadFile(os.Getenv("XAUTHORITY"))
	if err != nil {
		return nil, err
	}
	entries, err := x11.ParseXauthority(auth)
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("missing test authority: %v", err)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, display.Net, display.Addr)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	closeConn := func() { stop(); _ = conn.Close() }
	if _, err := conn.Write(x11.SetupRequest(entries[0].Name, entries[0].Data)); err != nil {
		closeConn()
		return nil, err
	}
	setup, err := x11.ReadSetup(conn)
	if err != nil {
		closeConn()
		return nil, err
	}
	window := x11.NewIDGen(setup).Next()
	req := x11.CreateWindow(window, setup.Root)
	binary.LittleEndian.PutUint32(req[28:], 1<<11)
	req = binary.LittleEndian.AppendUint32(req, 1<<17|1<<22)
	binary.LittleEndian.PutUint16(req[2:], uint16(len(req)/4))
	if _, err := conn.Write(req); err != nil {
		closeConn()
		return nil, err
	}
	return func() error {
		defer closeConn()
		destroy := []byte{4, 0, 2, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint32(destroy[4:], window)
		if _, err := conn.Write(append(destroy, x11.GetInputFocus()...)); err != nil {
			return err
		}
		// The later roundtrip makes a missing event falsifiable without waiting
		// for a timeout; arboard itself would wait indefinitely at this point.
		for {
			packet, err := x11.ReadPacket(conn)
			if err != nil {
				return err
			}
			if packet.EventCode() == 17 && binary.LittleEndian.Uint32(packet.Raw[8:]) == window {
				return nil
			}
			if packet.IsReply() {
				return fmt.Errorf("clipboard reader cannot shut down: DestroyNotify missing")
			}
		}
	}, nil
}
