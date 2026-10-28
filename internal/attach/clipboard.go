package attach

import (
	"context"
	"fmt"
	"os"
	"time"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/protocolpb"
)

// SendTerminalInput handles clipboard keys from a terminal. SendInput remains a
// raw byte API and never reads the machine's clipboard.
func (c *Client) SendTerminalInput(ctx context.Context, data []byte) error {
	if c.isViewOnly() {
		return c.SendInput(ctx, data)
	}
	c.clipboardInputMu.Lock()
	defer c.clipboardInputMu.Unlock()
	if c.clipboardInput.Reader == nil {
		c.clipboardInput.Reader = c.ClipboardReader
		if c.clipboardInput.Reader == nil {
			c.clipboardInput.Reader = clipboard.NewX11(os.Getenv("DISPLAY"), os.Getenv("XAUTHORITY"))
		}
	}
	err := c.clipboardInput.Forward(ctx, data, func(raw []byte) error { return c.SendInput(ctx, raw) }, func(img clipboard.Image) error { return c.SendImage(ctx, img) })
	if clipboard.IsPasteError(err) {
		c.showInfoStatus("Image paste: " + err.Error())
		// Clipboard failures must not end the interactive input loop.
		return nil
	}
	return err
}

// SendImage transfers a PNG/JPEG and waits for host-side publication and Ctrl+V.
// It is independent of X11 and can be reused by other client frontends.
func (c *Client) SendImage(ctx context.Context, img clipboard.Image) error {
	if c.isViewOnly() {
		return fmt.Errorf("control not permitted")
	}
	c.clipboardMu.Lock()
	defer c.clipboardMu.Unlock()
	c.mu.RLock()
	ws := c.ws
	c.mu.RUnlock()
	if ws == nil {
		return fmt.Errorf("client not connected")
	}
	replies := make(chan *protocolpb.Clipboard, 2)
	c.clipboardReplyMu.Lock()
	c.clipboardReply = replies
	c.clipboardTransferID = ""
	c.clipboardReplyMu.Unlock()
	defer func() {
		c.clipboardReplyMu.Lock()
		c.clipboardReply = nil
		c.clipboardTransferID = ""
		c.clipboardReplyMu.Unlock()
	}()
	var transferID string
	waitReply := func(offset uint32, completed bool) error {
		timer := c.clock().NewTimer(15 * time.Second)
		defer timer.Stop()
		select {
		case reply := <-replies:
			if reply.Error != "" {
				return fmt.Errorf("image paste: %s", reply.Error)
			}
			if reply.Completed != completed || (!completed && reply.Offset != offset) {
				return fmt.Errorf("invalid image paste acknowledgement")
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("image paste acknowledgement timed out")
		}
	}
	defer func() {
		if transferID == "" {
			return
		}
		cancelCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.writeFrame(cancelCtx, ws, &protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: &protocolpb.Clipboard{TransferId: transferID, Cancel: true}}})
	}()
	err := clipboard.Transfer(ctx, img, func(ctx context.Context, chunk *protocolpb.Clipboard) error {
		transferID = chunk.TransferId
		c.clipboardReplyMu.Lock()
		c.clipboardTransferID = transferID
		c.clipboardReplyMu.Unlock()
		if err := c.writeFrame(ctx, ws, &protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: chunk}}); err != nil {
			return err
		}
		offset := chunk.Offset + uint32(len(chunk.Data))
		if offset < chunk.Total && offset%clipboard.WindowBytes == 0 {
			return waitReply(offset, false)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return waitReply(uint32(len(img.Data)), true)
}

func (c *Client) receiveClipboardReply(reply *protocolpb.Clipboard) {
	if len(reply.Data) != 0 {
		return
	}
	c.clipboardReplyMu.Lock()
	defer c.clipboardReplyMu.Unlock()
	if reply.TransferId == c.clipboardTransferID && c.clipboardReply != nil {
		select {
		case c.clipboardReply <- reply:
		default:
		}
	}
}
