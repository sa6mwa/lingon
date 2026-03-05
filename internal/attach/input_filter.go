package attach

// mouseReportFilter removes SGR mouse-report CSI sequences:
// ESC [ < ... M/m
// from stdin streams before forwarding to remote PTYs.
type mouseReportFilter struct {
	state int
	buf   []byte
}

func (f *mouseReportFilter) reset() {
	f.state = 0
	f.buf = f.buf[:0]
}

func (f *mouseReportFilter) Filter(in []byte) []byte {
	if len(in) == 0 {
		return in
	}
	out := make([]byte, 0, len(in))
	return f.FilterInto(in, out)
}

func (f *mouseReportFilter) FilterInto(in []byte, out []byte) []byte {
	if len(in) == 0 {
		return out[:0]
	}
	out = out[:0]
	flush := func() {
		if len(f.buf) == 0 {
			return
		}
		out = append(out, f.buf...)
		f.reset()
	}
	for _, b := range in {
		switch f.state {
		case 0:
			if b == 0x1b {
				f.state = 1
				f.buf = append(f.buf[:0], b)
				continue
			}
			out = append(out, b)
		case 1:
			f.buf = append(f.buf, b)
			if b == '[' {
				f.state = 2
				continue
			}
			flush()
		case 2:
			f.buf = append(f.buf, b)
			if b == '<' {
				f.state = 3
				continue
			}
			flush()
		case 3:
			if (b >= '0' && b <= '9') || b == ';' {
				f.buf = append(f.buf, b)
				continue
			}
			if b == 'M' || b == 'm' {
				// Complete SGR mouse report; drop the buffered sequence.
				f.reset()
				continue
			}
			flush()
			if b == 0x1b {
				f.state = 1
				f.buf = append(f.buf[:0], b)
				continue
			}
			out = append(out, b)
		default:
			f.reset()
			out = append(out, b)
		}
	}
	return out
}

func filterMouseByte(f *mouseReportFilter, b byte, out []byte) []byte {
	var one [1]byte
	one[0] = b
	return f.FilterInto(one[:], out)
}
