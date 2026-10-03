package relay

import (
	"net/http"
	"sync"
)

// sessionListConnections covers account sockets without a hub session. Token
// validation under this lock fences registration against credential revocation.
type sessionListConnections struct {
	mu    sync.Mutex
	users map[string]map[*wsConn]struct{}
}

func (s *HTTPServer) registerSessionList(ws *wsConn, username string, r *http.Request) (func(), error) {
	lists := &s.sessionLists
	lists.mu.Lock()
	defer lists.mu.Unlock()
	access, err := s.accessTokenFromRequest(r)
	if err != nil {
		return nil, err
	}
	if access.Username != username {
		return nil, ErrInvalidCredentials
	}
	if lists.users == nil {
		lists.users = make(map[string]map[*wsConn]struct{})
	}
	if lists.users[username] == nil {
		lists.users[username] = make(map[*wsConn]struct{})
	}
	lists.users[username][ws] = struct{}{}
	return func() {
		lists.mu.Lock()
		defer lists.mu.Unlock()
		delete(lists.users[username], ws)
		if len(lists.users[username]) == 0 {
			delete(lists.users, username)
		}
	}, nil
}

func (s *HTTPServer) disconnectSessionLists(username string) {
	lists := &s.sessionLists
	lists.mu.Lock()
	conns := make([]*wsConn, 0, len(lists.users[username]))
	for ws := range lists.users[username] {
		conns = append(conns, ws)
	}
	delete(lists.users, username)
	lists.mu.Unlock()
	for _, ws := range conns {
		ws.closeNow()
	}
}
