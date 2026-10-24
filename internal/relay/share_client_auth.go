package relay

import (
	"net/http"
	"strings"
	"time"
)

type clientRegistration struct {
	replaced   connection
	granted    bool
	holder     string
	cols, rows int
}

// Share revalidation and hub registration must be atomic with removal during
// revocation. Otherwise a hello delayed past revocation can regain access.
// Socket writes and closes happen only after releasing the admission lock.
func (s *HTTPServer) registerSessionClient(ws *wsConn, r *http.Request, clientID string, wantsControl bool) (clientRegistration, error) {
	if ws.shareToken != "" {
		s.shareAdmissionMu.Lock()
		defer s.shareAdmissionMu.Unlock()
		var current shareSession
		var err error
		now := time.Now().UTC()
		if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
			current, err = s.shareSessionForToken(token, now)
		} else {
			current, err = s.shareSessionFromRequest(r, now)
		}
		if err != nil {
			return clientRegistration{}, err
		}
		if current.ShareToken != ws.shareToken || current.SessionID != ws.sessionID {
			return clientRegistration{}, ErrInvalidCredentials
		}
		ws.scope = current.Scope
	}
	var result clientRegistration
	result.replaced, result.granted, result.holder, result.cols, result.rows = s.Hub.registerClient(ws, ws.sessionID, clientID, wantsControl, true)
	return result, nil
}
