package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/theme"
	"pkt.systems/lingon/internal/webui"
	"pkt.systems/pslog"
)

const (
	wsReadLimit    = config.DefaultWSReadLimit
	wsPingInterval = 30 * time.Second
	wsPongTimeout  = 60 * time.Second

	accessCookieName  = "lingon_access"
	refreshCookieName = "lingon_refresh"
	shareCookieName   = "bifrons_share_session"
	shareSessionTTL   = 20 * time.Minute
)

var (
	errMissingAuthorization = errors.New("missing authorization")
	errInvalidAuthorization = errors.New("invalid authorization")
	errStoreUnavailable     = errors.New("store unavailable")
)

// HTTPServer exposes relay HTTP and WSS endpoints.
type HTTPServer struct {
	Store                   *Store
	Users                   *UserStore
	Authenticator           *Authenticator
	Logger                  pslog.Logger
	BasePath                string
	DataDir                 string
	UsersFile               string
	Hub                     *Hub
	WebUI                   webui.Options
	sessions                *sessionNotifier
	ConnectLimiter          *ConnectLimiter
	WallTimeout             time.Duration
	WallInactiveAfterLevels []time.Duration
	shareMu                 sync.Mutex
	shareSessions           map[string]shareSession
	wallMu                  sync.Mutex
	wallSvc                 *WallService
}

type shareSession struct {
	ID         string
	ShareToken string
	SessionID  string
	Scope      ShareScope
	ExpiresAt  time.Time
}

// NewHTTPServer constructs a relay HTTP server.
func NewHTTPServer(store *Store, users *UserStore, auth *Authenticator, logger pslog.Logger, hub *Hub) *HTTPServer {
	if logger == nil {
		logger = logging.Default()
	}
	if hub == nil {
		hub = NewHub(logger)
	}
	return &HTTPServer{
		Store:                   store,
		Users:                   users,
		Authenticator:           auth,
		Hub:                     hub,
		Logger:                  logger,
		sessions:                newSessionNotifier(),
		WallTimeout:             defaultWallTimeout,
		WallInactiveAfterLevels: cloneWallInactiveAfterLevels(defaultWallInactiveAfterLevels),
	}
}

// Handler returns the HTTP handler for relay endpoints.
func (s *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/auth/login", s.handleLogin)
	mux.HandleFunc("/auth/refresh", s.handleRefresh)
	mux.HandleFunc("/auth/share", s.handleShareAuth)
	mux.HandleFunc("/auth/share/session", s.handleShareSession)
	mux.HandleFunc("/auth/logout", s.handleLogout)
	mux.HandleFunc("/auth/logout/clients", s.handleLogoutClients)
	mux.HandleFunc("/themes", s.handleThemes)
	mux.HandleFunc("/sessions", s.handleListSessions)
	mux.HandleFunc("/users", s.handleUsers)
	mux.HandleFunc("/users/", s.handleUserAction)
	mux.HandleFunc("/share/create", s.handleShareCreate)
	mux.HandleFunc("/share/list", s.handleShareList)
	mux.HandleFunc("/share/revoke", s.handleShareRevoke)
	mux.HandleFunc("/share/revoke-all", s.handleShareRevokeAll)
	mux.HandleFunc("/wall", s.handleWall)
	mux.HandleFunc("/wall/events", s.handleWallEvents)
	mux.HandleFunc("/wall/inactivity", s.handleWallInactivity)
	mux.HandleFunc("/ws/host", s.handleWSHost)
	mux.HandleFunc("/ws/client", s.handleWSClient)
	mux.Handle("/", webui.HandlerWithOptions(s.WebUI))
	return mux
}

type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	TOTP       string `json:"totp"`
	ClientType string `json:"client_type,omitempty"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type shareAuthRequest struct {
	Token string `json:"token"`
}

type shareSessionResponse struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name,omitempty"`
	Scope     string `json:"scope"`
}

type loginResponse struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type userResponse struct {
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type userCreateRequest struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

type userCreateResponse struct {
	Username   string    `json:"username"`
	Password   string    `json:"password"`
	TOTPSecret string    `json:"totp_secret"`
	TOTPURL    string    `json:"totp_url"`
	CreatedAt  time.Time `json:"created_at"`
}

type userPasswordRequest struct {
	Password string `json:"password,omitempty"`
}

type userPasswordResponse struct {
	Password string `json:"password"`
}

type userTOTPResponse struct {
	TOTPSecret string `json:"totp_secret"`
	TOTPURL    string `json:"totp_url"`
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HTTPServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Users == nil {
		writeError(w, http.StatusInternalServerError, "user store unavailable")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	user, err := s.Authenticator.Validate(req.Username, req.Password, req.TOTP, time.Now())
	if err != nil {
		setLogReason(w, authReason(ErrInvalidCredentials))
		writeError(w, http.StatusUnauthorized, ErrInvalidCredentials.Error())
		return
	}
	now := time.Now().UTC()
	clientType := normalizeClientType(req.ClientType)
	refresh, err := s.Store.CreateRefreshTokenForClient(user.Username, clientType, DefaultRefreshTokenTTL, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	access, err := s.Store.CreateAccessTokenForRefresh(user.Username, refresh.Token, refresh.ClientType, DefaultAccessTokenTTL, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist state")
		return
	}
	s.setAuthCookies(w, access, refresh)
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:      access.Token,
		AccessExpiresAt:  access.ExpiresAt,
		RefreshToken:     refresh.Token,
		RefreshExpiresAt: refresh.ExpiresAt,
	})
}

func (s *HTTPServer) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Users == nil {
		writeError(w, http.StatusInternalServerError, "user store unavailable")
		return
	}
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.RefreshToken == "" {
		if cookie, err := r.Cookie(refreshCookieName); err == nil {
			req.RefreshToken = cookie.Value
		}
	}
	now := time.Now().UTC()
	refresh, err := s.Store.ValidateRefreshToken(req.RefreshToken, now)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	clientType := normalizeClientType(refresh.ClientType)
	access, err := s.Store.CreateAccessTokenForRefresh(refresh.Username, refresh.Token, clientType, DefaultAccessTokenTTL, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist state")
		return
	}
	s.setAuthCookies(w, access, refresh)
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:      access.Token,
		AccessExpiresAt:  access.ExpiresAt,
		RefreshToken:     refresh.Token,
		RefreshExpiresAt: refresh.ExpiresAt,
	})
}

func (s *HTTPServer) handleShareAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	var req shareAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		setLogReason(w, "invalid_share_token")
		s.clearShareSessionCookie(w)
		writeError(w, http.StatusUnauthorized, "invalid share token")
		return
	}
	session, err := s.issueShareSession(token, time.Now().UTC())
	if err != nil {
		setLogReason(w, "invalid_share_token")
		s.clearShareSessionCookie(w)
		writeError(w, http.StatusUnauthorized, "invalid share token")
		return
	}
	s.setShareSessionCookie(w, session.ID)
	writeJSON(w, http.StatusOK, shareSessionResponse{
		SessionID: session.SessionID,
		Name:      s.sessionName(session.SessionID),
		Scope:     string(session.Scope),
	})
}

func (s *HTTPServer) handleShareSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.shareSessionFromRequest(r, time.Now().UTC())
	if err != nil {
		s.clearShareSessionCookie(w)
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	writeJSON(w, http.StatusOK, shareSessionResponse{
		SessionID: session.SessionID,
		Name:      s.sessionName(session.SessionID),
		Scope:     string(session.Scope),
	})
}

func (s *HTTPServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.RefreshToken == "" {
		if cookie, err := r.Cookie(refreshCookieName); err == nil {
			req.RefreshToken = cookie.Value
		}
	}
	now := time.Now().UTC()
	if req.RefreshToken != "" {
		_ = s.Store.RevokeRefreshTokenAndAccess(req.RefreshToken, now)
	} else if token, err := s.accessTokenFromRequest(r); err == nil {
		s.Store.DeleteAccessToken(token.Token)
	}
	s.revokeShareSessionFromRequest(r)
	_ = s.persist()
	s.clearAuthCookies(w)
	s.clearShareSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (s *HTTPServer) handleLogoutClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	now := time.Now().UTC()
	count := s.Store.RevokeClientTokens(username, now)
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist state")
		return
	}
	s.clearAuthCookies(w)
	s.revokeShareSessionFromRequest(r)
	s.clearShareSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"status": "logged out", "revoked": count})
}

func (s *HTTPServer) handleThemes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, theme.All())
}

func (s *HTTPServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	sessions, changed := s.listActiveSessions(username, time.Now().UTC())
	if changed {
		s.persistIfPossible()
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *HTTPServer) streamSessionsWS(ctx context.Context, ws *wsConn, username string) {
	if username == "" || s.sessions == nil || s.Store == nil {
		return
	}
	ch, unsubscribe := s.sessions.Subscribe(username)
	defer unsubscribe()

	writeSessions := func() error {
		sessions, changed := s.listActiveSessions(username, time.Now().UTC())
		if changed {
			s.persistIfPossible()
		}
		if ws.logger != nil {
			ws.logger.Trace("relay.sessions.push", "user", username, "count", len(sessions))
		}
		return ws.Send(ctx, frameSessions(sessions))
	}

	if err := writeSessions(); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if err := writeSessions(); err != nil {
				return
			}
		}
	}
}

func (s *HTTPServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	if _, err := s.requireAuth(r); err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		users := s.Users.List()
		resp := make([]userResponse, 0, len(users))
		for _, user := range users {
			resp = append(resp, userResponse{
				Username:  user.Username,
				CreatedAt: user.CreatedAt,
			})
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		var req userCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		result, err := CreateUser(s.Users, req.Username, req.Password, time.Now().UTC())
		if err != nil {
			switch {
			case errors.Is(err, ErrUsernameRequired):
				writeError(w, http.StatusBadRequest, "username is required")
			case errors.Is(err, ErrUserExists):
				writeError(w, http.StatusConflict, "username already exists")
			default:
				writeError(w, http.StatusInternalServerError, "user creation failed")
			}
			return
		}
		if err := s.persistUsers(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist state")
			return
		}
		writeJSON(w, http.StatusOK, userCreateResponse{
			Username:   result.User.Username,
			Password:   result.Password,
			TOTPSecret: result.TOTPSecret,
			TOTPURL:    result.TOTPURL,
			CreatedAt:  result.User.CreatedAt,
		})
	}
}

func (s *HTTPServer) handleUserAction(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	if _, err := s.requireAuth(r); err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/users/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "user is required")
		return
	}
	username := parts[0]

	if len(parts) == 1 && r.Method == http.MethodDelete {
		user, err := DeleteUser(s.Users, username)
		if err != nil {
			switch {
			case errors.Is(err, ErrUserNotFound):
				writeError(w, http.StatusNotFound, "user not found")
			case errors.Is(err, ErrUsernameRequired):
				writeError(w, http.StatusBadRequest, "user is required")
			default:
				writeError(w, http.StatusInternalServerError, "user delete failed")
			}
			return
		}
		if s.Store != nil {
			s.Store.RevokeTokensForUsername(user.Username)
		}
		if err := s.persistUsers(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to persist state")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}

	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "rotate-totp":
			result, err := RotateUserTOTP(s.Users, username)
			if err != nil {
				switch {
				case errors.Is(err, ErrUserNotFound):
					writeError(w, http.StatusNotFound, "user not found")
				case errors.Is(err, ErrUsernameRequired):
					writeError(w, http.StatusBadRequest, "user is required")
				default:
					writeError(w, http.StatusInternalServerError, "totp generation failed")
				}
				return
			}
			if err := s.persistUsers(); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to persist state")
				return
			}
			writeJSON(w, http.StatusOK, userTOTPResponse{TOTPSecret: result.TOTPSecret, TOTPURL: result.TOTPURL})
			return
		case "password":
			var req userPasswordRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			result, err := ChangeUserPassword(s.Users, username, req.Password)
			if err != nil {
				switch {
				case errors.Is(err, ErrUserNotFound):
					writeError(w, http.StatusNotFound, "user not found")
				case errors.Is(err, ErrUsernameRequired):
					writeError(w, http.StatusBadRequest, "user is required")
				default:
					writeError(w, http.StatusInternalServerError, "password generation failed")
				}
				return
			}
			if err := s.persistUsers(); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to persist state")
				return
			}
			writeJSON(w, http.StatusOK, userPasswordResponse{Password: result.Password})
			return
		}
	}

	writeError(w, http.StatusNotFound, "unsupported user action")
}

type shareCreateRequest struct {
	SessionID string `json:"session_id"`
	Scope     string `json:"scope"`
	TTL       string `json:"ttl,omitempty"`
}

type shareCreateResponse struct {
	Token string `json:"token"`
}

func (s *HTTPServer) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	var req shareCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	if owner, ok := s.Store.SessionOwner(req.SessionID); !ok || owner != username {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	scope := ShareScope(strings.ToLower(req.Scope))
	var ttl time.Duration
	if req.TTL != "" {
		parsed, err := time.ParseDuration(req.TTL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid ttl")
			return
		}
		ttl = parsed
	}
	share, err := s.Store.CreateShareToken(req.SessionID, scope, ttl, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist state")
		return
	}
	writeJSON(w, http.StatusOK, shareCreateResponse{Token: share.Token})
}

func (s *HTTPServer) handleShareList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	statuses, err := parseShareListStatuses(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tokens := s.Store.ListShareTokensFiltered(username, statuses, time.Now().UTC())
	writeJSON(w, http.StatusOK, tokens)
}

func parseShareListStatuses(values url.Values) (map[ShareTokenStatus]bool, error) {
	raw := values["status"]
	if len(raw) == 0 {
		return map[ShareTokenStatus]bool{ShareTokenStatusValid: true}, nil
	}
	statuses := make(map[ShareTokenStatus]bool)
	for _, entry := range raw {
		switch strings.ToLower(strings.TrimSpace(entry)) {
		case "":
			continue
		case string(ShareTokenStatusValid):
			statuses[ShareTokenStatusValid] = true
		case string(ShareTokenStatusRevoked):
			statuses[ShareTokenStatusRevoked] = true
		case string(ShareTokenStatusExpired):
			statuses[ShareTokenStatusExpired] = true
		case "all":
			statuses[ShareTokenStatusValid] = true
			statuses[ShareTokenStatusRevoked] = true
			statuses[ShareTokenStatusExpired] = true
		default:
			return nil, fmt.Errorf("invalid status filter: %s", entry)
		}
	}
	if len(statuses) == 0 {
		statuses[ShareTokenStatusValid] = true
	}
	return statuses, nil
}

type shareRevokeRequest struct {
	Token string `json:"token"`
}

type wallRequest struct {
	Message string `json:"message"`
}

type wallResponse struct {
	Status   string `json:"status"`
	Sessions int    `json:"sessions"`
}

type wallEventResponse struct {
	ID             uint64    `json:"id"`
	SessionID      string    `json:"session_id,omitempty"`
	Sender         string    `json:"sender"`
	Message        string    `json:"message"`
	TimeoutSeconds uint32    `json:"timeout_seconds"`
	Kind           uint32    `json:"kind,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type wallEventsResponse struct {
	Events  []wallEventResponse `json:"events"`
	NextID  uint64              `json:"next_id"`
	HasMore bool                `json:"has_more"`
}

type wallInactivityRequest struct {
	SessionID string `json:"session_id"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

type wallInactivityResponse struct {
	SessionID     string `json:"session_id"`
	Enabled       bool   `json:"enabled"`
	InactiveAfter string `json:"inactive_after,omitempty"`
}

func (s *HTTPServer) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	var req shareRevokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	if owner, ok := s.Store.ShareTokenOwner(req.Token); !ok || owner != username {
		writeError(w, http.StatusNotFound, "share token not found")
		return
	}
	if err := s.Store.RevokeShareToken(req.Token, time.Now().UTC()); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *HTTPServer) handleShareRevokeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	revoked := s.Store.RevokeShareTokensForUsername(username, time.Now().UTC())
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "revoked": revoked})
}

func (s *HTTPServer) handleWall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	var req wallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	svc := s.wallService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "wall service unavailable")
		return
	}
	sender := svc.senderLabel(username, server.RealIP(r))
	sent, err := svc.sendUserWall(username, sender, message, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wallResponse{
		Status:   "sent",
		Sessions: sent,
	})
}

func (s *HTTPServer) handleWallEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	svc := s.wallService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "wall service unavailable")
		return
	}
	query := r.URL.Query()
	var sinceID uint64
	if raw := strings.TrimSpace(query.Get("since")); raw != "" {
		parsed, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid since value")
			return
		}
		sinceID = parsed
	}
	limit := defaultWallEventLimit
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid limit value")
			return
		}
		limit = parsed
	}
	events, nextID, hasMore := svc.listEvents(username, sinceID, limit, time.Now().UTC())
	payload := make([]wallEventResponse, 0, len(events))
	for _, event := range events {
		payload = append(payload, wallEventResponse{
			ID:             event.ID,
			SessionID:      event.SessionID,
			Sender:         event.Sender,
			Message:        event.Message,
			TimeoutSeconds: event.TimeoutSeconds,
			Kind:           uint32(event.Kind),
			CreatedAt:      event.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, wallEventsResponse{
		Events:  payload,
		NextID:  nextID,
		HasMore: hasMore,
	})
}

func (s *HTTPServer) handleWallInactivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	var req wallInactivityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	if owner, ok := s.Store.SessionOwner(req.SessionID); !ok || owner != username {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	svc := s.wallService()
	if svc == nil {
		writeError(w, http.StatusInternalServerError, "wall service unavailable")
		return
	}
	sender := svc.senderLabel(username, server.RealIP(r))
	now := time.Now().UTC()
	var (
		enabled bool
		after   time.Duration
	)
	if req.Enabled == nil {
		enabled, after = svc.toggleInactivity(username, req.SessionID, sender, now)
	} else {
		enabled, after = svc.setInactivity(username, req.SessionID, sender, *req.Enabled, now)
	}
	afterLabel := ""
	if enabled {
		afterLabel = formatDurationCompact(after)
	}
	s.Hub.BroadcastSessionFrame(r.Context(), req.SessionID, &protocolpb.Frame{
		Payload: &protocolpb.Frame_WallInactivityStatus{WallInactivityStatus: &protocolpb.WallInactivityStatus{
			Enabled:       enabled,
			InactiveAfter: afterLabel,
		}},
	}, true)
	writeJSON(w, http.StatusOK, wallInactivityResponse{
		SessionID:     req.SessionID,
		Enabled:       enabled,
		InactiveAfter: afterLabel,
	})
}

func (s *HTTPServer) handleWSHost(w http.ResponseWriter, r *http.Request) {
	remoteIP := server.RealIP(r)
	if s.ConnectLimiter != nil {
		if allowed, retryAfter := s.ConnectLimiter.Allow(time.Now()); !allowed {
			ctx := withConnectionContext(r.Context(), string(RoleHost))
			ctx = pslog.ContextWithLogger(ctx, s.Logger)
			logger := s.loggerWithContext(ctx).With("role", "host")
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
				InsecureSkipVerify: false,
			})
			if err != nil {
				return
			}
			ws := newWSConn(newConnID(), RoleHost, "", ShareScopeControl, conn, logger)
			defer func() {
				_ = ws.Close(ctx, "rate limited")
			}()
			_ = ws.SendImmediate(ctx, frameErrorRetry("rate limited", retryAfter))
			return
		}
	}
	token, err := s.accessTokenFromRequest(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, err)
		return
	}
	username := token.Username
	s.markTokenClientType(token, "host")
	ctx := withConnectionContext(r.Context(), string(RoleHost))
	ctx = pslog.ContextWithLogger(ctx, s.Logger)
	logger := s.loggerWithContext(ctx).With("role", "host")
	if logger != nil {
		logger.Debug("relay.ws.host.accept.start", "ip", remoteIP)
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: false,
	})
	if err != nil {
		return
	}
	ws := newWSConn(newConnID(), RoleHost, "", ShareScopeControl, conn, logger)
	hostConnID := ws.ID()
	unregistered := false
	defer func() {
		_ = ws.Close(ctx, "closing")
		if !unregistered {
			s.Hub.Unregister(ws)
		}
	}()

	frame, err := readFrame(ctx, conn, wsReadLimit)
	if err != nil {
		logger.Debug("relay.ws.hello.read.failed", "err", err)
		return
	}
	if frame.GetHello() == nil || frame.SessionId == "" {
		_ = ws.SendImmediate(ctx, frameError("missing hello"))
		return
	}
	ws.sessionID = frame.SessionId
	cols := int(frame.GetHello().Cols)
	rows := int(frame.GetHello().Rows)
	sessionName := strings.TrimSpace(frame.GetHello().ClientId)
	reconnected := false
	var (
		storedSession Session
		sessionExists bool
	)
	if s.Store != nil {
		storedSession, sessionExists = s.Store.GetSession(frame.SessionId)
		if sessionExists && storedSession.Username != username {
			_ = ws.SendImmediate(ctx, frameError("session belongs to another user"))
			return
		}
		reconnected = sessionExists
	}
	if s.Store == nil && s.Hub.HasHost(frame.SessionId) {
		reconnected = true
	}
	replacedHost := s.Hub.registerHost(ws, frame.SessionId, cols, rows)
	if s.Store != nil {
		now := time.Now().UTC()
		if !sessionExists {
			storedSession = Session{
				ID:           frame.SessionId,
				Username:     username,
				Name:         sessionName,
				CreatedAt:    now,
				LastActiveAt: now,
				Status:       "active",
			}
		} else {
			storedSession.LastActiveAt = now
			storedSession.Status = "active"
			if sessionName != "" {
				storedSession.Name = sessionName
			}
		}
		s.Store.CreateSession(storedSession)
		s.Store.SetActiveSession(ActiveSession{
			SessionID:        frame.SessionId,
			HostConnectionID: hostConnID,
			Cols:             cols,
			Rows:             rows,
			LastSeenAt:       now,
		})
		if err := s.persist(); err != nil {
			_ = ws.SendImmediate(ctx, frameError("failed to persist state"))
			s.Hub.Unregister(ws)
			unregistered = true
			return
		}
	}
	if replacedHost != nil {
		rejected := frameErrorSessionRejected("superseded by reconnect")
		if replacedWS, ok := replacedHost.(*wsConn); ok {
			_ = replacedWS.SendImmediate(context.Background(), rejected)
		} else {
			_ = replacedHost.Send(context.Background(), rejected)
		}
		_ = replacedHost.Close(context.Background(), "superseded by reconnect")
	}
	logger.Info("relay.host.connect.done", "session", frame.SessionId, "reconnected", reconnected, "ip", remoteIP)
	s.notifySessions(username)

	streamCtx, cancelStream := context.WithCancel(ctx)
	go s.streamSessionsWS(streamCtx, ws, username)

	s.serveWSLoop(ctx, ws)
	cancelStream()
	s.Hub.Unregister(ws)
	unregistered = true
	if s.Store != nil {
		_ = s.Store.MarkSessionInactive(frame.SessionId, hostConnID, time.Now().UTC())
	}
	s.notifySessions(username)
}

func (s *HTTPServer) handleWSClient(w http.ResponseWriter, r *http.Request) {
	remoteIP := server.RealIP(r)
	ctx := withConnectionContext(r.Context(), string(RoleClient))
	ctx = pslog.ContextWithLogger(ctx, s.Logger)
	logger := s.loggerWithContext(ctx).With("role", "client")
	if logger != nil {
		logger.Debug("relay.ws.client.accept.start", "ip", remoteIP)
	}
	if s.ConnectLimiter != nil {
		if allowed, retryAfter := s.ConnectLimiter.Allow(time.Now()); !allowed {
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
				InsecureSkipVerify: false,
			})
			if err != nil {
				return
			}
			ws := newWSConn(newConnID(), RoleClient, "", ShareScopeControl, conn, logger)
			defer func() {
				_ = ws.Close(ctx, "rate limited")
			}()
			_ = ws.SendImmediate(ctx, frameErrorRetry("rate limited", retryAfter))
			return
		}
	}
	var sessionID string
	scope := ShareScopeControl
	var username string
	shareToken := strings.TrimSpace(r.URL.Query().Get("token"))
	shareAuthenticated := false
	if shareSession, err := s.shareSessionFromRequest(r, time.Now().UTC()); err == nil {
		sessionID = shareSession.SessionID
		scope = shareSession.Scope
		shareAuthenticated = true
	} else if shareToken != "" {
		if s.Store == nil {
			writeError(w, http.StatusInternalServerError, "store unavailable")
			return
		}
		share, ok := s.Store.GetShareToken(shareToken)
		if !ok || share.IsExpired(time.Now().UTC()) {
			setLogReason(w, "invalid_share_token")
			writeError(w, http.StatusUnauthorized, "invalid share token")
			return
		}
		sessionID = share.SessionID
		scope = share.Scope
		shareAuthenticated = true
	} else {
		token, err := s.accessTokenFromRequest(r)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, err)
			return
		}
		username = token.Username
		s.markTokenClientType(token, "client")
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: false,
	})
	if err != nil {
		return
	}
	ws := newWSConn(newConnID(), RoleClient, sessionID, scope, conn, logger)
	defer func() {
		_ = ws.Close(ctx, "closing")
		s.Hub.Unregister(ws)
	}()

	frame, err := readFrame(ctx, conn, wsReadLimit)
	if err != nil {
		logger.Debug("relay.ws.hello.read.failed", "err", err)
		return
	}
	if frame.GetHello() == nil {
		_ = ws.SendImmediate(ctx, frameError("missing hello"))
		return
	}
	if frame.SessionId != "" {
		if shareAuthenticated && sessionID != "" && frame.SessionId != sessionID {
			_ = ws.SendImmediate(ctx, frameError("session mismatch"))
			return
		}
		sessionID = frame.SessionId
	}
	if sessionID == "" {
		_ = ws.SendImmediate(ctx, frameError("missing session"))
		return
	}
	if !shareAuthenticated && s.Store != nil {
		if session, ok := s.Store.GetSession(sessionID); ok && session.Username != username {
			_ = ws.SendImmediate(ctx, frameError("session belongs to another user"))
			return
		}
	}
	ws.sessionID = sessionID
	clientID := frame.GetHello().ClientId
	reconnected := s.Hub.HasClientID(sessionID, clientID)
	replacedClient, granted, holder, cols, rows := s.Hub.registerClient(ws, sessionID, clientID, frame.GetHello().WantsControl)
	if !s.Hub.HasHost(sessionID) {
		_ = ws.SendImmediate(ctx, frameError("no host connected"))
		return
	}
	_ = ws.Send(ctx, frameWelcome(granted, cols, rows, holder, sessionID))
	if granted {
		s.Hub.BroadcastControl(ctx, sessionID)
	}
	_ = s.Hub.HandleClientFrame(ctx, ws, frame)
	if replacedClient != nil {
		rejected := frameErrorSessionRejected("superseded by reconnect")
		if replacedWS, ok := replacedClient.(*wsConn); ok {
			_ = replacedWS.SendImmediate(context.Background(), rejected)
		} else {
			_ = replacedClient.Send(context.Background(), rejected)
		}
		_ = replacedClient.Close(context.Background(), "superseded by reconnect")
	}
	logger.Info("relay.client.connect.done", "session", sessionID, "client", clientID, "reconnected", reconnected, "ip", remoteIP)

	streamCtx, cancelStream := context.WithCancel(ctx)
	go s.streamSessionsWS(streamCtx, ws, username)

	s.serveWSLoop(ctx, ws)
	cancelStream()
}

func (s *HTTPServer) serveWSLoop(ctx context.Context, ws *wsConn) {
	pingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.pingLoop(pingCtx, ws)

	for {
		frame, err := readFrame(ctx, ws.conn, wsReadLimit)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			closeStatus := websocket.CloseStatus(err)
			if closeStatus == websocket.StatusNormalClosure ||
				closeStatus == websocket.StatusGoingAway ||
				closeStatus == websocket.StatusNoStatusRcvd ||
				errors.Is(err, io.EOF) ||
				errors.Is(err, net.ErrClosed) ||
				errors.Is(err, context.Canceled) ||
				errors.Is(err, syscall.ECONNRESET) ||
				errors.Is(err, syscall.EPIPE) {
				return
			}
			if ws.logger != nil {
				ws.logger.Debug("relay.ws.read.failed", "err", err, "role", ws.role, "session", ws.sessionID)
			}
			return
		}
		ws.touchActivity()
		frame.SessionId = ws.sessionID

		switch ws.role {
		case RoleHost:
			if frame.GetActivity() != nil {
				if svc := s.wallService(); svc != nil {
					svc.markActivity(ws.sessionID, time.Now().UTC())
				}
				continue
			}
			if err := s.Hub.HandleHostFrame(ctx, ws, frame); err != nil {
				if errors.Is(err, errHostSessionClosed) {
					if ws.logger != nil {
						ws.logger.Info("relay.host.session.closed", "session", ws.sessionID)
					}
					return
				}
				if errors.Is(err, errStaleHostConnection) {
					if ws.logger != nil {
						ws.logger.Debug("relay.host.stale.connection", "session", ws.sessionID, "conn", ws.ID())
					}
					rejectCtx, rejectCancel := context.WithTimeout(context.Background(), time.Second)
					_ = ws.SendImmediate(rejectCtx, frameErrorSessionRejected("superseded by reconnect"))
					rejectCancel()
					return
				}
				_ = ws.Send(ctx, frameError(err.Error()))
			}
		case RoleClient:
			if err := s.Hub.HandleClientFrame(ctx, ws, frame); err != nil {
				_ = ws.Send(ctx, frameError(err.Error()))
			} else if isClientActivityFrame(frame) {
				if svc := s.wallService(); svc != nil {
					svc.markActivity(ws.sessionID, time.Now().UTC())
				}
			}
		}
	}
}

func (s *HTTPServer) pingLoop(ctx context.Context, conn *wsConn) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, wsPongTimeout)
			if err := conn.PingIfIdle(pingCtx, wsPingInterval); err != nil && conn.logger != nil {
				conn.logger.Debug("relay.ws.ping.failed", "err", err)
			}
			cancel()
		}
	}
}

func (s *HTTPServer) requireAuth(r *http.Request) (string, error) {
	access, err := s.accessTokenFromRequest(r)
	if err != nil {
		return "", err
	}
	return access.Username, nil
}

func (s *HTTPServer) accessTokenFromRequest(r *http.Request) (AccessToken, error) {
	token, err := tokenFromRequest(r)
	if err != nil {
		return AccessToken{}, err
	}
	if s.Store == nil {
		return AccessToken{}, errStoreUnavailable
	}
	return s.Store.ValidateAccessToken(token, time.Now().UTC())
}

func tokenFromRequest(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			return "", errInvalidAuthorization
		}
		return strings.TrimSpace(parts[1]), nil
	}
	if cookie, err := r.Cookie(accessCookieName); err == nil {
		return strings.TrimSpace(cookie.Value), nil
	}
	return "", errMissingAuthorization
}

func (s *HTTPServer) markTokenClientType(access AccessToken, clientType string) {
	if s.Store == nil {
		return
	}
	if clientType == "" {
		return
	}
	s.Store.MarkTokenClientType(access.Token, clientType)
	_ = s.persist()
}

func (s *HTTPServer) notifySessions(username string) {
	if s.sessions == nil {
		return
	}
	if s.Logger != nil {
		s.Logger.Debug("relay.sessions.notify", "user", username)
	}
	s.sessions.Notify(username)
}

// Close disconnects active websocket sessions.
func (s *HTTPServer) Close(reason string) {
	s.wallMu.Lock()
	if s.wallSvc != nil {
		s.wallSvc.close()
	}
	s.wallMu.Unlock()
	if s.Hub != nil {
		s.Hub.CloseAll(reason)
	}
}

func (s *HTTPServer) listActiveSessions(username string, now time.Time) ([]Session, bool) {
	if s.Store == nil {
		return []Session{}, false
	}
	changed := false
	if pruned := s.Store.PruneSessions(now, 24*time.Hour); pruned > 0 {
		changed = true
	}
	sessions := s.Store.ListSessions(username)
	if sessions == nil {
		sessions = []Session{}
	}
	if s.Hub == nil {
		active := sessions[:0]
		for _, session := range sessions {
			if session.Status == "active" {
				active = append(active, session)
			}
		}
		return active, changed
	}

	active := sessions[:0]
	for _, session := range sessions {
		if s.Hub.HasHost(session.ID) {
			if session.Status != "active" {
				session.Status = "active"
				session.LastActiveAt = now
				s.Store.CreateSession(session)
				changed = true
			}
			active = append(active, session)
			continue
		}
		if s.Store.MarkSessionInactive(session.ID, "", now) {
			changed = true
		}
	}
	return active, changed
}

func (s *HTTPServer) persistIfPossible() {
	if s.DataDir == "" {
		return
	}
	if _, err := os.Stat(s.DataDir); err == nil {
		_ = s.persist()
	}
}

func (s *HTTPServer) wallService() *WallService {
	s.wallMu.Lock()
	defer s.wallMu.Unlock()
	if s.wallSvc == nil {
		s.wallSvc = newWallService(s.Store, s.Hub, s.Logger, s.WallTimeout, s.WallInactiveAfterLevels)
		return s.wallSvc
	}
	s.wallSvc.setConfig(s.WallTimeout, s.WallInactiveAfterLevels)
	return s.wallSvc
}

// ConfigureWall sets wall notification timeout and inactivity trigger levels.
func (s *HTTPServer) ConfigureWall(timeout time.Duration, inactiveAfterLevels []time.Duration) {
	s.WallTimeout = timeout
	s.WallInactiveAfterLevels = normalizeWallInactiveAfterLevels(inactiveAfterLevels)
	if svc := s.wallService(); svc != nil {
		svc.setConfig(timeout, s.WallInactiveAfterLevels)
	}
}

func formatDurationCompact(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d%time.Second != 0 {
		return d.String()
	}
	totalSeconds := int64(d / time.Second)
	hours := totalSeconds / int64(time.Hour/time.Second)
	totalSeconds %= int64(time.Hour / time.Second)
	minutes := totalSeconds / int64(time.Minute/time.Second)
	seconds := totalSeconds % int64(time.Minute/time.Second)
	var b strings.Builder
	if hours > 0 {
		b.WriteString(strconv.FormatInt(hours, 10))
		b.WriteByte('h')
	}
	if minutes > 0 {
		b.WriteString(strconv.FormatInt(minutes, 10))
		b.WriteByte('m')
	}
	if seconds > 0 {
		b.WriteString(strconv.FormatInt(seconds, 10))
		b.WriteByte('s')
	}
	if b.Len() == 0 {
		return "0s"
	}
	return b.String()
}

func isClientActivityFrame(frame *protocolpb.Frame) bool {
	if frame == nil {
		return false
	}
	if in := frame.GetIn(); in != nil {
		return len(in.GetData()) > 0
	}
	if command := frame.GetCommand(); command != nil {
		return command.GetKind() == protocolpb.CommandKind_COMMAND_KIND_SEND_EOF
	}
	return false
}

func (s *HTTPServer) cookiePath() string {
	if s.BasePath == "" {
		return "/"
	}
	return s.BasePath
}

func (s *HTTPServer) sessionName(sessionID string) string {
	if s.Store == nil || sessionID == "" {
		return ""
	}
	if session, ok := s.Store.GetSession(sessionID); ok {
		return session.Name
	}
	return ""
}

func (s *HTTPServer) issueShareSession(token string, now time.Time) (shareSession, error) {
	if s.Store == nil {
		return shareSession{}, errStoreUnavailable
	}
	share, ok := s.Store.GetShareToken(token)
	if !ok || share.IsExpired(now) {
		return shareSession{}, ErrTokenNotFound
	}
	session := shareSession{
		ID:         newConnID(),
		ShareToken: token,
		SessionID:  share.SessionID,
		Scope:      share.Scope,
		ExpiresAt:  now.Add(shareSessionTTL),
	}
	s.shareMu.Lock()
	s.pruneShareSessionsLocked(now)
	if s.shareSessions == nil {
		s.shareSessions = make(map[string]shareSession)
	}
	s.shareSessions[session.ID] = session
	s.shareMu.Unlock()
	return session, nil
}

func (s *HTTPServer) shareSessionFromRequest(r *http.Request, now time.Time) (shareSession, error) {
	if s.Store == nil {
		return shareSession{}, errStoreUnavailable
	}
	cookie, err := r.Cookie(shareCookieName)
	if err != nil {
		return shareSession{}, errMissingAuthorization
	}
	id := strings.TrimSpace(cookie.Value)
	if id == "" {
		return shareSession{}, errMissingAuthorization
	}
	s.shareMu.Lock()
	s.pruneShareSessionsLocked(now)
	session, ok := s.shareSessions[id]
	if !ok {
		s.shareMu.Unlock()
		return shareSession{}, ErrTokenNotFound
	}
	session.ExpiresAt = now.Add(shareSessionTTL)
	s.shareSessions[id] = session
	s.shareMu.Unlock()
	share, ok := s.Store.GetShareToken(session.ShareToken)
	if !ok || share.IsExpired(now) {
		s.revokeShareSession(id)
		return shareSession{}, ErrTokenNotFound
	}
	session.SessionID = share.SessionID
	session.Scope = share.Scope
	return session, nil
}

func (s *HTTPServer) pruneShareSessionsLocked(now time.Time) {
	if len(s.shareSessions) == 0 {
		return
	}
	for id, session := range s.shareSessions {
		if now.After(session.ExpiresAt) {
			delete(s.shareSessions, id)
		}
	}
}

func (s *HTTPServer) revokeShareSession(id string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	s.shareMu.Lock()
	delete(s.shareSessions, id)
	s.shareMu.Unlock()
}

func (s *HTTPServer) revokeShareSessionFromRequest(r *http.Request) {
	if r == nil {
		return
	}
	cookie, err := r.Cookie(shareCookieName)
	if err != nil {
		return
	}
	s.revokeShareSession(strings.TrimSpace(cookie.Value))
}

func (s *HTTPServer) setAuthCookies(w http.ResponseWriter, access AccessToken, refresh RefreshToken) {
	path := s.cookiePath()
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    access.Token,
		Path:     path,
		Expires:  access.ExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    refresh.Token,
		Path:     path,
		Expires:  refresh.ExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *HTTPServer) setShareSessionCookie(w http.ResponseWriter, sessionID string) {
	path := s.cookiePath()
	http.SetCookie(w, &http.Cookie{
		Name:     shareCookieName,
		Value:    sessionID,
		Path:     path,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *HTTPServer) clearAuthCookies(w http.ResponseWriter) {
	path := s.cookiePath()
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *HTTPServer) clearShareSessionCookie(w http.ResponseWriter) {
	path := s.cookiePath()
	http.SetCookie(w, &http.Cookie{
		Name:     shareCookieName,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func normalizeClientType(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return "client"
	}
	return trimmed
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type logReasonSetter interface {
	SetLogReason(string)
}

func setLogReason(w http.ResponseWriter, reason string) {
	if reason == "" {
		return
	}
	if setter, ok := w.(logReasonSetter); ok {
		setter.SetLogReason(reason)
	}
}

func authReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTokenExpired):
		return "token_expired"
	case errors.Is(err, ErrTokenNotFound):
		return "token_not_found"
	case errors.Is(err, ErrTokenRevoked):
		return "token_revoked"
	case errors.Is(err, ErrInvalidCredentials):
		return "invalid_credentials"
	case errors.Is(err, errMissingAuthorization):
		return "missing_auth"
	case errors.Is(err, errInvalidAuthorization):
		return "invalid_auth_header"
	case errors.Is(err, errStoreUnavailable):
		return "store_unavailable"
	default:
		return "auth_failed"
	}
}

func writeAuthError(w http.ResponseWriter, status int, err error) {
	setLogReason(w, authReason(err))
	message := "unauthorized"
	if err != nil {
		message = err.Error()
	}
	writeError(w, status, message)
}

func withConnectionContext(ctx context.Context, role string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey("role"), role)
}

type contextKey string

func (s *HTTPServer) persist() error {
	if s.Store == nil || s.DataDir == "" {
		return nil
	}
	if err := s.Store.Save(s.DataDir); err != nil {
		if s.Logger != nil {
			s.Logger.Error("relay.store.persist.failed", "err", err)
		}
		return err
	}
	return nil
}

func (s *HTTPServer) persistUsers() error {
	if s.Users == nil || s.UsersFile == "" {
		return nil
	}
	if err := s.Users.Save(s.UsersFile); err != nil {
		if s.Logger != nil {
			s.Logger.Error("relay.users.persist.failed", "err", err)
		}
		return err
	}
	return nil
}

func (s *HTTPServer) loggerWithContext(ctx context.Context) pslog.Logger {
	if ctx == nil {
		return s.Logger
	}
	logger := pslog.Ctx(ctx)
	if logger != nil {
		return logger
	}
	return s.Logger
}
