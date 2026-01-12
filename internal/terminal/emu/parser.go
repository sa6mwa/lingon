package emu

import (
	"strconv"
	"strings"
)

const (
	stateGround = iota
	stateEscape
	stateCSI
	stateOSC
	stateString
	stateCharset
)

type parserState struct {
	state int

	private   bool
	params    []int
	paramSeen bool
	current   int
	hasParam  bool

	oscBuf []byte
	oscEsc bool

	utf8Buf []byte
	charsetTarget int
}

func (p *parserState) resetCSI() {
	p.private = false
	p.params = p.params[:0]
	p.paramSeen = false
	p.current = 0
	p.hasParam = false
}

func (p *parserState) addDigit(d int) {
	p.paramSeen = true
	if !p.hasParam {
		p.current = 0
		p.hasParam = true
	}
	p.current = p.current*10 + d
}

func (p *parserState) nextParam() {
	if p.hasParam {
		p.params = append(p.params, p.current)
	} else {
		p.params = append(p.params, -1)
	}
	p.hasParam = false
	p.current = 0
}

func (p *parserState) finalizeParams() []int {
	if p.hasParam {
		p.params = append(p.params, p.current)
	} else if len(p.params) == 0 {
		p.params = append(p.params, -1)
	}
	out := make([]int, len(p.params))
	copy(out, p.params)
	p.resetCSI()
	return out
}

func (p *parserState) resetOSC() {
	p.oscBuf = p.oscBuf[:0]
	p.oscEsc = false
}

func (p *parserState) resetString() {
	p.oscEsc = false
}

func parseOSC(buf []byte) (int, string) {
	if len(buf) == 0 {
		return -1, ""
	}
	parts := strings.SplitN(string(buf), ";", 2)
	code, err := strconv.Atoi(parts[0])
	if err != nil {
		return -1, ""
	}
	if len(parts) == 1 {
		return code, ""
	}
	return code, parts[1]
}
