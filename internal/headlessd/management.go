package headlessd

import (
	"encoding/json"
	"net/http"

	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/sessionmanage"
)

func (d *Daemon) persistRename(id, name string) error {
	return d.store.WithLock(func(state *headless.State) error {
		rec := state.Sessions[id]
		rec.Name = name
		state.Sessions[id] = rec
		return nil
	})
}

func (d *Daemon) handleSessionManagement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req sessionmanage.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID != d.sessionID || d.runner == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(sessionmanage.Result{Error: "invalid session operation"})
		return
	}
	result := d.runner.Manage(r.Context(), req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
