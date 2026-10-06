package attach

import "bytes"

// mouseInputFilter keeps only an ambiguous ESC/CSI prefix. Once CSI '<' is
// recognized, SGR mouse parameters are discarded incrementally across reads.
// Ordinary input and non-mouse escape sequences retain their original bytes.
type mouseInputFilter struct{ state uint8 }

func (f *mouseInputFilter) feed(data []byte) []byte {
	if f.state == 0 && bytes.IndexByte(data, 0x1b) < 0 {
		return data
	}
	out := make([]byte, 0, len(data)+2)
	for _, b := range data {
		switch f.state {
		case 1:
			if b == '[' {
				f.state = 2
				continue
			}
			out = append(out, 0x1b)
			f.state = 0
		case 2:
			if b == '<' {
				f.state = 3
				continue
			}
			out = append(out, 0x1b, '[')
			f.state = 0
		case 3:
			if b == 'M' || b == 'm' {
				f.state = 0
				continue
			}
			if (b >= '0' && b <= '9') || b == ';' {
				continue
			}
			f.state = 0
		}
		if b == 0x1b {
			f.state = 1
		} else {
			out = append(out, b)
		}
	}
	return out
}

func (f *mouseInputFilter) ambiguous() bool { return f.state == 1 || f.state == 2 }

func (f *mouseInputFilter) flush() []byte {
	switch f.state {
	case 1:
		f.state = 0
		return []byte{0x1b}
	case 2:
		f.state = 0
		return []byte{0x1b, '['}
	}
	return nil
}
