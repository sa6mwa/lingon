package mvu

// Read returns a resolved snapshot for UI read-model consumers.
func (r *Runtime) Read() State {
	return r.State()
}
