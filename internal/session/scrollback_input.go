package session

import "strconv"

type scrollCommand int

const (
	scrollNone scrollCommand = iota
	scrollExit
	scrollPageUp
	scrollPageDown
	scrollLineUp
	scrollLineDown
	scrollFiveUp
	scrollFiveDown
	scrollTop
	scrollBottom
	scrollWheelUp
	scrollWheelDown
)

type scrollInputState struct {
	escState int
	escBuf   []byte
}

func (s *scrollInputState) reset() {
	s.escState = 0
	s.escBuf = s.escBuf[:0]
}

func (s *scrollInputState) feed(b byte) scrollCommand {
	switch b {
	case 'q', 'Q':
		s.reset()
		return scrollExit
	case 'k':
		return scrollLineUp
	case 'j':
		return scrollLineDown
	case 'K':
		return scrollFiveUp
	case 'J':
		return scrollFiveDown
	case 'w', 'u':
		return scrollPageUp
	case 's', 'd':
		return scrollPageDown
	case 'g':
		return scrollTop
	case 'G':
		return scrollBottom
	}
	switch s.escState {
	case 0:
		if b == 0x1b {
			s.escState = 1
		}
		return scrollNone
	case 1:
		if b == '[' {
			s.escState = 2
		} else if b == 'O' {
			s.escState = 5
		} else {
			s.reset()
		}
		return scrollNone
	case 2:
		switch b {
		case '<':
			s.escState = 6
			s.escBuf = s.escBuf[:0]
			return scrollNone
		case 'A':
			s.reset()
			return scrollLineUp
		case 'B':
			s.reset()
			return scrollLineDown
		case 'H':
			s.reset()
			return scrollTop
		case 'F':
			s.reset()
			return scrollBottom
		}
		if b >= '0' && b <= '9' {
			s.escState = 3
			s.escBuf = s.escBuf[:0]
			s.escBuf = append(s.escBuf, b)
			return scrollNone
		}
		s.reset()
		return scrollNone
	case 3:
		if b == '~' {
			cmd := scrollFromTildeSeq(s.escBuf)
			s.reset()
			return cmd
		}
		if (b >= '0' && b <= '9') || b == ';' {
			s.escBuf = append(s.escBuf, b)
			return scrollNone
		}
		s.reset()
		return scrollNone
	case 5:
		switch b {
		case 'A':
			s.reset()
			return scrollLineUp
		case 'B':
			s.reset()
			return scrollLineDown
		case 'H':
			s.reset()
			return scrollTop
		case 'F':
			s.reset()
			return scrollBottom
		default:
			s.reset()
			return scrollNone
		}
	case 6:
		if b == 'M' || b == 'm' {
			cmd := scrollFromMouseSeq(s.escBuf)
			s.reset()
			return cmd
		}
		if (b >= '0' && b <= '9') || b == ';' {
			s.escBuf = append(s.escBuf, b)
			return scrollNone
		}
		s.reset()
		return scrollNone
	default:
		s.reset()
		return scrollNone
	}
}

func scrollFromTildeSeq(buf []byte) scrollCommand {
	num, ok := parseFirstInt(buf)
	if !ok {
		return scrollNone
	}
	switch num {
	case 1, 7:
		return scrollTop
	case 4, 8:
		return scrollBottom
	case 5:
		return scrollPageUp
	case 6:
		return scrollPageDown
	default:
		return scrollNone
	}
}

func scrollFromMouseSeq(buf []byte) scrollCommand {
	num, ok := parseFirstInt(buf)
	if !ok {
		return scrollNone
	}
	switch num {
	case 64:
		return scrollWheelUp
	case 65:
		return scrollWheelDown
	default:
		return scrollNone
	}
}

func parseFirstInt(buf []byte) (int, bool) {
	if len(buf) == 0 {
		return 0, false
	}
	end := 0
	for end < len(buf) && buf[end] >= '0' && buf[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(string(buf[:end]))
	if err != nil {
		return 0, false
	}
	return n, true
}
