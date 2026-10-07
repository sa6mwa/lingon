package control

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"pkt.systems/lingon/internal/clock"
)

// RenamePrompt consumes terminal bytes while editing a display name.
// Escape or Ctrl+C cancels, Enter accepts, and Backspace removes one complete code point.
type RenamePrompt struct {
	Active         bool
	SessionID      string
	text           []byte
	escape         bool
	escapeSequence bool
	clock          clock.Clock
	escapeDeadline time.Time
}

// Begin starts an empty rename prompt for an immutable session ID using clk.
// A nil clock uses the system clock.
func (p *RenamePrompt) Begin(id string, clk clock.Clock) {
	if clk == nil {
		clk = clock.New()
	}
	*p = RenamePrompt{Active: true, SessionID: id, clock: clk}
}

// Label returns a printable prompt without incomplete UTF-8 or terminal escapes.
func (p *RenamePrompt) Label() string {
	text := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(p.text), ""))
	return "Rename session (Enter saves, Esc cancels): " + text
}

// Feed returns the accepted name and whether editing finished.
func (p *RenamePrompt) Feed(b byte) (name string, done bool) {
	if b == 0x1b || b == 0x03 {
		p.Active = false
		p.escape = b == 0x1b
		p.escapeSequence = false
		if p.escape {
			p.escapeDeadline = p.clock.Now().Add(100 * time.Millisecond)
		}
		return "", true
	}
	if b == '\r' || b == '\n' {
		p.Active = false
		return strings.TrimSpace(string(p.text)), true
	}
	if b == 0x7f || b == 0x08 {
		if len(p.text) > 0 {
			_, n := utf8.DecodeLastRune(p.text)
			p.text = p.text[:len(p.text)-n]
		}
		return "", false
	}
	if b >= 0x20 && len(p.text) < 320 {
		p.text = append(p.text, b)
	}
	return "", false
}

// DiscardEscapeTail prevents an arrow-key sequence that cancels editing from reaching the shell.
// Recognition ends 100 ms after Escape, even for an incomplete CSI or SS3 tail.
func (p *RenamePrompt) DiscardEscapeTail(b byte) bool {
	if !p.escape {
		return false
	}
	if !p.clock.Now().Before(p.escapeDeadline) {
		p.escape = false
		p.escapeSequence = false
		return false
	}
	if p.escapeSequence {
		if b >= 0x40 && b <= 0x7e {
			p.escape = false
			p.escapeSequence = false
		}
		return true
	}
	if b == '[' || b == 'O' {
		p.escapeSequence = true
		return true
	}
	p.escape = false
	return false
}
