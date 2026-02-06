package lingon

import "pkt.systems/lingon/internal/theme"

// ThemeNames returns all known theme names.
func ThemeNames() []string {
	return theme.Names()
}
