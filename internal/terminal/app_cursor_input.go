package terminal

// TranslateAppCursorKeys rewrites plain CSI cursor keys to SS3 form when
// application cursor mode is active. Incomplete or non-cursor CSI sequences are
// returned unchanged.
func TranslateAppCursorKeys(in []byte, active bool) []byte {
	if !active || len(in) == 0 {
		return in
	}
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		if in[i] != 0x1b || i+2 >= len(in) || in[i+1] != '[' {
			out = append(out, in[i])
			continue
		}
		switch in[i+2] {
		case 'A', 'B', 'C', 'D', 'F', 'H':
			out = append(out, 0x1b, 'O', in[i+2])
			i += 2
		default:
			out = append(out, in[i])
		}
	}
	return out
}
