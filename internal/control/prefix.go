package control

// CtrlL is the control-L (form feed) prefix byte.
const (
	CtrlL byte = 0x0c
	CtrlW byte = 0x17
)

// Action represents a handled control action.
type Action int

// Control actions emitted by Prefix.
const (
	// ActionNone indicates no control action.
	ActionNone Action = iota
	// ActionQuit quits the local session/attach.
	ActionQuit
	// ActionSendCtrlD sends an explicit Ctrl-D to the remote session.
	ActionSendCtrlD
	// ActionNewPTY creates a new local PTY session.
	ActionNewPTY
	// ActionToggleRespawn toggles respawn for the active local session.
	ActionToggleRespawn
	// ActionToggleWallInactivity cycles relay-managed inactivity wall notifications.
	ActionToggleWallInactivity
	// ActionToggleOffline toggles relay publishing for the active local session.
	ActionToggleOffline
	// ActionHelp shows the help overlay.
	ActionHelp
	// ActionNextTab selects the next session tab.
	ActionNextTab
	// ActionPrevTab selects the previous session tab.
	ActionPrevTab
	// ActionToggleTabBar toggles tab bar visibility.
	ActionToggleTabBar
	// ActionNextTheme cycles to the next theme.
	ActionNextTheme
	// ActionScrollback enters scrollback buffer mode.
	ActionScrollback
	// ActionResizeHeadless resizes the active headless remote session to the local viewport.
	ActionResizeHeadless
)

// Prefix tracks ctrl+l command state.
type Prefix struct {
	pending    bool
	repeatWall bool
}

// Feed consumes a byte and returns an action plus passthrough bytes.
func (p *Prefix) Feed(b byte) (Action, []byte) {
	if p.repeatWall {
		if b == CtrlW {
			return ActionToggleWallInactivity, nil
		}
		p.repeatWall = false
	}
	if p.pending {
		p.pending = false
		switch b {
		case 'Q':
			return ActionQuit, nil
		case 'd', 'D':
			return ActionSendCtrlD, nil
		case 'l', 'L':
			return ActionNone, []byte{CtrlL}
		case 'c', 'C':
			return ActionNewPTY, nil
		case 'r', 'R':
			return ActionToggleRespawn, nil
		case 'w', 'W':
			return ActionToggleWallInactivity, nil
		case CtrlW:
			p.repeatWall = true
			return ActionToggleWallInactivity, nil
		case 'o', 'O':
			return ActionToggleOffline, nil
		case 'h', 'H':
			return ActionHelp, nil
		case 'n', 'N':
			return ActionNextTab, nil
		case 'p', 'P':
			return ActionPrevTab, nil
		case 'b', 'B':
			return ActionToggleTabBar, nil
		case 't', 'T':
			return ActionNextTheme, nil
		case '[':
			return ActionScrollback, nil
		case '0', 0:
			return ActionResizeHeadless, nil
		case CtrlL:
			return ActionNone, []byte{CtrlL}
		default:
			return ActionNone, nil
		}
	}
	if b == CtrlL {
		p.pending = true
		p.repeatWall = false
		return ActionNone, nil
	}
	return ActionNone, []byte{b}
}

// Pending reports whether a prefix is pending.
func (p *Prefix) Pending() bool {
	return p.pending
}
