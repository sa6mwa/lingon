package clipboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestClipboardInputContinuesAfterFailure(t *testing.T) {
	for _, mode := range []string{"read", "paste", "repeated read"} {
		t.Run(mode, func(t *testing.T) {
			failure := errors.New("clipboard failed")
			reads, pastes := 0, 0
			img := testImage(t, "image/png", false)
			input := Input{Reader: readerFunc(func(context.Context) (Image, error) {
				reads++
				if mode == "repeated read" || (mode == "read" && reads == 1) {
					return Image{}, failure
				}
				return img, nil
			})}
			var raw bytes.Buffer
			err := input.Forward(context.Background(), []byte("\x16x\x16"), func(b []byte) error { _, err := raw.Write(b); return err }, func(Image) error {
				pastes++
				if mode == "paste" && pastes == 1 {
					return failure
				}
				return nil
			})
			if !IsPasteError(err) || !errors.Is(err, failure) {
				t.Fatal("clipboard error was lost", err)
			}
			wantPastes := 1
			if mode == "paste" {
				wantPastes = 2
			}
			if mode == "repeated read" {
				wantPastes = 0
			}
			if reads != 2 || pastes != wantPastes || raw.String() != "x" {
				t.Fatalf("remaining input bypassed parsing: reads=%d pastes=%d raw=%q", reads, pastes, raw.String())
			}
		})
	}
}

func TestClipboardInputFailurePreservesBracketedPaste(t *testing.T) {
	data := []byte("\x16\x1b[200~x\x16y\x1b[201~\x16")
	for split := 0; split <= len(data); split++ {
		reads, pastes := 0, 0
		var raw bytes.Buffer
		input := Input{Reader: readerFunc(func(context.Context) (Image, error) {
			reads++
			if reads == 1 {
				return Image{}, errors.New("read failed")
			}
			return testImage(t, "image/png", false), nil
		})}
		rawWrite := func(b []byte) error { _, err := raw.Write(b); return err }
		paste := func(Image) error { pastes++; return nil }
		errA := input.Forward(context.Background(), data[:split], rawWrite, paste)
		errB := input.Forward(context.Background(), data[split:], rawWrite, paste)
		if !IsPasteError(errors.Join(errA, errB)) || reads != 2 || pastes != 1 || raw.String() != "\x1b[200~x\x16y\x1b[201~" {
			t.Fatalf("split %d lost parser state: reads=%d pastes=%d raw=%q", split, reads, pastes, raw.String())
		}
	}
}

func malformedClipboardImages(t *testing.T) map[string]Image {
	t.Helper()
	png := testImage(t, "image/png", false)
	jpeg := testImage(t, "image/jpeg", false)
	badCRC := append([]byte(nil), png.Data...)
	badCRC[len(badCRC)-1] ^= 1
	return map[string]Image{"PNG header only": {MIME: png.MIME, Data: png.Data[:33]}, "PNG corrupt checksum": {MIME: png.MIME, Data: badCRC}, "JPEG truncated body": {MIME: jpeg.MIME, Data: jpeg.Data[:len(jpeg.Data)-2]}}
}

func TestClipboardValidationRejectsMalformedBody(t *testing.T) {
	for name, img := range malformedClipboardImages(t) {
		t.Run(name, func(t *testing.T) {
			if _, _, err := image.DecodeConfig(bytes.NewReader(img.Data)); err != nil {
				t.Fatal("fixture lacks valid metadata", err)
			}
			if _, _, err := image.Decode(bytes.NewReader(img.Data)); err == nil {
				t.Fatal("fixture is not malformed")
			}
			if err := img.Validate(); err == nil {
				t.Fatal("malformed image body accepted")
			}
		})
	}
}

func TestClipboardReceiverRejectsMalformedBody(t *testing.T) {
	for name, img := range malformedClipboardImages(t) {
		t.Run(name, func(t *testing.T) {
			var receiver Receiver
			got, err := receiver.Accept(&protocolpb.Clipboard{TransferId: strings.Repeat("a", 32), Source: "client", Mime: img.MIME, Total: uint32(len(img.Data)), Data: img.Data})
			if err == nil || len(got.Data) != 0 {
				t.Fatal("completed malformed image reached publication")
			}
		})
	}
}

func TestClipboardValidationBoundsDecodedSize(t *testing.T) {
	for _, dims := range [][2]uint32{{32769, 1}, {1, 32769}, {8192, 4097}} {
		img := testImage(t, "image/png", false)
		img.Data = append([]byte(nil), img.Data[:33]...)
		binary.BigEndian.PutUint32(img.Data[16:], dims[0])
		binary.BigEndian.PutUint32(img.Data[20:], dims[1])
		binary.BigEndian.PutUint32(img.Data[29:], crc32.ChecksumIEEE(img.Data[12:29]))
		if _, _, err := image.DecodeConfig(bytes.NewReader(img.Data)); err != nil {
			t.Fatal(err)
		}
		if err := img.Validate(); err == nil || !strings.Contains(err.Error(), "decoded size limit") {
			t.Fatalf("decoded allocation was not bounded for %dx%d: %v", dims[0], dims[1], err)
		}
	}
}
