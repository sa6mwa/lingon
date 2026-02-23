package mvu

import (
	"fmt"

	"pkt.systems/version"
)

// HelpTitle returns the help modal title with the current Lingon version.
func HelpTitle() string {
	return fmt.Sprintf("lingon %s controls", version.Current())
}

// HelpLines returns the help modal content lines.
func HelpLines(state State) []string {
	return []string{
		HelpTitle(),
		"session: " + state.SessionID,
		"endpoint: " + state.Endpoint,
		"",
		"Ctrl+L c  new session",
		"Ctrl+L Q  close session",
		"Ctrl+L [  scrollback (PgUp/PgDn/w/s half page; Up/Down or p/n line; Home/End/a/d top/bottom; wheel scrolls)",
		"Ctrl+L r  toggle respawn",
		"Ctrl+L o  toggle offline (host local-only)",
		"Ctrl+L w  cycle inactivity wall",
		"Ctrl+L h  help",
		"Ctrl+L n  next tab",
		"Ctrl+L p  prev tab",
		"Ctrl+L b  toggle tab bar",
		"Ctrl+L t  next theme",
		"Ctrl+L Ctrl+L  send literal Ctrl+L",
		"",
		"press q or Q to close help",
	}
}
