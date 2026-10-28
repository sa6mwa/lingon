package clipboard

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg" // JPEG clipboard validation.
	_ "image/png"  // PNG clipboard validation.
	"sync"

	"pkt.systems/lingon/internal/protocolpb"
)

// MaxBytes matches Softline's maximum clipboard image size.
const MaxBytes = 32 << 20

// MaxPixels bounds decoded images to at most 256 MiB of eight-byte pixels.
const MaxPixels = 32 << 20

// MaxDimension bounds scanline buffers in PNG/JPEG decoders.
const MaxDimension = 32768

// ChunkBytes keeps image messages below the ordinary websocket read limit.
const ChunkBytes = 64 << 10

// WindowBytes bounds unacknowledged image data on a slow relay/host link.
const WindowBytes = 1 << 20

// Progress acknowledges a full upload window without retaining image bytes.
func Progress(c *protocolpb.Clipboard) *protocolpb.Clipboard {
	offset := c.Offset + uint32(len(c.Data))
	if offset < c.Total && offset%WindowBytes == 0 {
		return &protocolpb.Clipboard{TransferId: c.TransferId, Source: c.Source, Offset: offset}
	}
	return nil
}

// Image contains original encoded bytes; clipboard delivery never transcodes.
type Image struct {
	MIME string
	Data []byte
}

// Validate rejects unsupported, malformed, or oversized images.
func (i Image) Validate() error {
	if len(i.Data) == 0 || len(i.Data) > MaxBytes {
		return fmt.Errorf("clipboard image must contain 1–%d bytes", MaxBytes)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(i.Data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("invalid clipboard image")
	}
	if (i.MIME != "image/png" || format != "png") && (i.MIME != "image/jpeg" || format != "jpeg") {
		return fmt.Errorf("clipboard image must be PNG or JPEG with a matching MIME type")
	}
	if cfg.Width > MaxDimension || cfg.Height > MaxDimension || uint64(cfg.Width)*uint64(cfg.Height) > MaxPixels {
		return fmt.Errorf("clipboard image exceeds decoded size limit (%d pixels, %d per side)", MaxPixels, MaxDimension)
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(i.Data))
	if err != nil {
		return fmt.Errorf("invalid clipboard image body: %w", err)
	}
	if decodedFormat != format || decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return fmt.Errorf("clipboard image metadata does not match its body")
	}
	return nil
}

// Reader reads an image selection. An empty image means no supported selection.
type Reader interface {
	Read(context.Context) (Image, error)
}

// Selection owns an immutable image snapshot until replaced or closed.
type Selection interface {
	Set(context.Context, Image) error
	Close() error
}

// Backend reads and owns an external clipboard selection.
type Backend interface {
	Reader
	Selection
}

// Transfer sends bounded chunks in order. The receiver publishes the image and
// delivers Ctrl+V only after the last chunk has been validated.
func Transfer(ctx context.Context, img Image, send func(context.Context, *protocolpb.Clipboard) error) error {
	if err := img.Validate(); err != nil {
		return err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	transferID := hex.EncodeToString(id[:])
	for offset := 0; offset < len(img.Data); offset += ChunkBytes {
		end := min(offset+ChunkBytes, len(img.Data))
		chunk := &protocolpb.Clipboard{TransferId: transferID, Mime: img.MIME, Total: uint32(len(img.Data)), Offset: uint32(offset), Data: img.Data[offset:end]}
		if err := send(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

// Receiver buffers at most one bounded transfer. Source is assigned by the
// authenticated transport, never trusted from a terminal client.
type Receiver struct {
	mu               sync.Mutex
	id, source, mime string
	total            uint32
	data             []byte
}

// Reset discards an incomplete transfer on disconnect or control change.
func (r *Receiver) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reset()
}

// Cancel discards only the specified sender's transfer; an old cancellation
// must never erase a newer upload from another client.
func (r *Receiver) Cancel(source, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.source == source && r.id == id {
		r.reset()
	}
}

func (r *Receiver) reset() {
	r.id, r.source, r.mime, r.total, r.data = "", "", "", 0, nil
}

// Accept validates and assembles a chunk, returning an image only on completion.
func (r *Receiver) Accept(c *protocolpb.Clipboard) (Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c == nil || len(c.TransferId) != 32 || len(c.Source) > 128 || c.Total == 0 || c.Total > MaxBytes || len(c.Data) == 0 || len(c.Data) > ChunkBytes || (c.Mime != "image/png" && c.Mime != "image/jpeg") {
		r.reset()
		return Image{}, fmt.Errorf("invalid clipboard chunk")
	}
	if c.Offset == 0 {
		r.reset()
		r.id, r.source, r.mime, r.total = c.TransferId, c.Source, c.Mime, c.Total
	}
	if r.id != c.TransferId || r.source != c.Source || r.mime != c.Mime || r.total != c.Total || uint32(len(r.data)) != c.Offset || uint64(c.Offset)+uint64(len(c.Data)) > uint64(r.total) {
		r.reset()
		return Image{}, fmt.Errorf("out-of-order clipboard chunk")
	}
	r.data = append(r.data, c.Data...)
	if uint32(len(r.data)) != r.total {
		return Image{}, nil
	}
	img := Image{MIME: r.mime, Data: r.data}
	r.reset()
	if err := img.Validate(); err != nil {
		return Image{}, err
	}
	return img, nil
}
