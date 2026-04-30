package session

import (
	"context"
	"strings"
)

// ResizeActive applies a size update to the current active session.
func (r *Runner) ResizeActive(cols, rows int) {
	if r == nil || cols <= 0 || rows <= 0 {
		return
	}
	r.opts.Cols = cols
	r.opts.Rows = rows
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
		if snap, err := local.Resize(cols, rows); err == nil {
			if local.publisher != nil {
				local.publisher.Resize(cols, rows, snap)
			}
		} else if r.logger != nil {
			r.logger.Debug("session.local.resize.failed", "err", err, "session", local.ID(), "cols", cols, "rows", rows)
		}
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
	if r.opts.Cols == cols && r.opts.Rows == rows {
		return false
	}
	r.ResizeActive(cols, rows)
	return true
}
