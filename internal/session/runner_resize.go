package session

import (
	"context"
	"strings"

	"pkt.systems/lingon/internal/config"
)

// terminalGeometry snapshots the pair shared by resize and terminal creation.
// Never hold geometryMu during terminal, process, render or network I/O.
func (r *Runner) terminalGeometry() (int, int) {
	r.geometryMu.RLock()
	defer r.geometryMu.RUnlock()
	return r.opts.Cols, r.opts.Rows
}

func (r *Runner) setTerminalGeometry(cols, rows int) {
	r.geometryMu.Lock()
	r.opts.Cols, r.opts.Rows = cols, rows
	r.geometryMu.Unlock()
}

func (r *Runner) initializeTerminalGeometry() {
	cols, rows := r.terminalGeometry()
	if cols <= 0 || rows <= 0 {
		detectedCols, detectedRows := termSizeAny(r.stdout(), r.stdin())
		if detectedCols > 0 && detectedRows > 0 {
			if cols <= 0 {
				cols = detectedCols
			}
			if rows <= 0 {
				rows = detectedRows
			}
		}
	}
	if cols <= 0 {
		cols = config.DefaultTerminalCols
	}
	if rows <= 0 {
		rows = config.DefaultTerminalRows
	}
	r.setTerminalGeometry(cols, rows)
}

// ResizeActive applies a size update to the current active session.
func (r *Runner) ResizeActive(cols, rows int) {
	if r == nil || cols <= 0 || rows <= 0 {
		return
	}
	r.setTerminalGeometry(cols, rows)
	activeID, activeLocal := r.activeSession()
	if activeLocal {
		targetID := strings.TrimSpace(activeID)
		if targetID == "" {
			targetID = r.firstLocalID()
		}
		local := r.localSession(targetID)
		if local == nil {
			return
		}
		r.resizeLocalSession(local, cols, rows)
		return
	}
	if r.remoteSessions != nil && strings.TrimSpace(activeID) != "" {
		_ = r.remoteSessions.SendResize(context.Background(), activeID, cols, rows)
	}
}

// ResizeActiveIfChanged applies a size update only when the active session has
// not already observed the requested dimensions.
func (r *Runner) ResizeActiveIfChanged(cols, rows int) bool {
	if r == nil || cols <= 0 || rows <= 0 {
		return false
	}
	activeID, activeLocal := r.activeSession()
	if activeLocal {
		targetID := strings.TrimSpace(activeID)
		if targetID == "" {
			targetID = r.firstLocalID()
		}
		local := r.localSession(targetID)
		if local == nil {
			return false
		}
		curCols, curRows := local.Size()
		if curCols == cols && curRows == rows {
			return false
		}
		r.ResizeActive(cols, rows)
		return true
	}
	curCols, curRows := r.terminalGeometry()
	if curCols == cols && curRows == rows {
		return false
	}
	r.ResizeActive(cols, rows)
	return true
}
