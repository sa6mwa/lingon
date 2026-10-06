package session

func (r *Runner) accessToken() string {
	r.credentialMu.RLock()
	defer r.credentialMu.RUnlock()
	return r.currentToken
}

func (r *Runner) setAccessToken(token string) {
	r.credentialMu.Lock()
	r.currentToken = token
	r.credentialMu.Unlock()
}
