package session

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/trace"
)

const oscProbeTimeout = 200 * time.Millisecond
const oscProbeGrace = 750 * time.Millisecond

const oscQuerySequence = "\x1b]10;?\x07\x1b]11;?\x07\x1b]12;?\x07"

type oscDefaults struct {
	fg     string
	bg     string
	cursor string
}

type oscStreamParser struct {
	state       int
	oscEsc      bool
	oscBuf      []byte
	oscRaw      []byte
	passthrough []byte
}

func (p *oscStreamParser) resetOSC() {
	p.state = 0
	p.oscEsc = false
	p.oscBuf = p.oscBuf[:0]
	p.oscRaw = p.oscRaw[:0]
}

func (p *oscStreamParser) resetAll() {
	p.resetOSC()
	p.passthrough = p.passthrough[:0]
}

func (p *oscStreamParser) AddPassthrough(raw []byte) {
	if len(raw) == 0 {
		return
	}
	p.passthrough = append(p.passthrough, raw...)
}

func (p *oscStreamParser) Feed(b byte) (code int, payload string, raw []byte, ok bool) {
	switch p.state {
	case 0:
		if b == 0x1b {
			p.state = 1
			p.oscRaw = p.oscRaw[:0]
			p.oscRaw = append(p.oscRaw, b)
			return 0, "", nil, false
		}
		p.passthrough = append(p.passthrough, b)
	case 1:
		if b == ']' {
			p.state = 2
			p.oscRaw = append(p.oscRaw, b)
			p.oscBuf = p.oscBuf[:0]
			p.oscEsc = false
			return 0, "", nil, false
		}
		if b == 0x1b {
			// Some terminals/multiplexers (notably tmux passthrough paths) can
			// duplicate ESC before OSC. Keep waiting for OSC introducer instead of
			// flushing partial bytes as passthrough.
			p.oscRaw = append(p.oscRaw, b)
			return 0, "", nil, false
		}
		p.oscRaw = append(p.oscRaw, b)
		p.passthrough = append(p.passthrough, p.oscRaw...)
		p.resetOSC()
	case 2:
		p.oscRaw = append(p.oscRaw, b)
		if p.oscEsc {
			p.oscEsc = false
			if b == '\\' {
				code, payload, ok = parseOSCQuery(p.oscBuf)
				raw = append([]byte(nil), p.oscRaw...)
				p.resetOSC()
				return code, payload, raw, ok
			}
			p.oscBuf = append(p.oscBuf, 0x1b, b)
			return 0, "", nil, false
		}
		switch b {
		case 0x1b:
			p.oscEsc = true
		case 0x07:
			code, payload, ok = parseOSCQuery(p.oscBuf)
			raw = append([]byte(nil), p.oscRaw...)
			p.resetOSC()
			return code, payload, raw, ok
		default:
			p.oscBuf = append(p.oscBuf, b)
		}
	default:
		p.resetOSC()
	}
	return 0, "", nil, false
}

func (p *oscStreamParser) FlushPassthrough() []byte {
	if p.state != 0 && len(p.oscRaw) > 0 {
		p.passthrough = append(p.passthrough, p.oscRaw...)
	}
	out := append([]byte(nil), p.passthrough...)
	p.resetAll()
	return out
}

func (p *oscStreamParser) DrainPassthrough() []byte {
	if len(p.passthrough) == 0 {
		return nil
	}
	out := append([]byte(nil), p.passthrough...)
	p.passthrough = p.passthrough[:0]
	return out
}

func oscQueryBytes() []byte {
	return []byte(oscQuerySequence)
}

func probeOuterColors(ctx context.Context, stdout, stdin *os.File, clk clock.Clock, tr *trace.Writer) (oscDefaults, []byte) {
	if stdout == nil || stdin == nil {
		return oscDefaults{}, nil
	}
	query := oscQueryBytes()
	_ = writeAll(ctx, stdout, query, clk)
	if tr != nil {
		tr.Event("outer_osc_query_request", map[string]any{
			"component": "host",
			"query":     trace.SummarizeBytes(query, 80),
		})
	}

	readCtx, cancel := context.WithTimeout(ctx, oscProbeTimeout)
	defer cancel()

	parser := oscStreamParser{}
	var defaults oscDefaults
	pending := map[int]struct{}{
		10: {},
		11: {},
		12: {},
	}
	buf := make([]byte, 256)

	for len(pending) > 0 && readCtx.Err() == nil {
		n, err := readPTY(readCtx, stdin, buf)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				break
			}
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				continue
			}
			break
		}
		if n == 0 {
			continue
		}
		for _, b := range buf[:n] {
			code, payload, raw, ok := parser.Feed(b)
			if !ok {
				continue
			}
			if payload == "" || payload == "?" {
				parser.AddPassthrough(raw)
				continue
			}
			switch code {
			case 10:
				defaults.fg = payload
			case 11:
				defaults.bg = payload
			case 12:
				defaults.cursor = payload
			default:
				parser.AddPassthrough(raw)
				continue
			}
			delete(pending, code)
			if tr != nil {
				tr.Event("outer_osc_query_response", map[string]any{
					"component": "host",
					"code":      code,
					"payload":   payload,
				})
			}
		}
	}

	return defaults, parser.FlushPassthrough()
}
