package relay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"pkt.systems/pslog"
)

const (
	wsReadLimit    = 1 << 20
	wsPingInterval = 30 * time.Second
	wsPongTimeout  = 60 * time.Second
)

// HTTPServer exposes relay HTTP and WSS endpoints.
type HTTPServer struct {
	Store         *Store
	Users         *UserStore
	Authenticator *Authenticator
	Logger        pslog.Logger
	DataDir       string
	UsersFile     string
	Hub           *Hub
}

// NewHTTPServer constructs a relay HTTP server.
func NewHTTPServer(store *Store, users *UserStore, auth *Authenticator, logger pslog.Logger, hub *Hub) *HTTPServer {
	if logger == nil {
		logger = pslog.LoggerFromEnv()
	}
	if hub == nil {
		hub = NewHub(logger)
	}
	return &HTTPServer{Store: store, Users: users, Authenticator: auth, Logger: logger, Hub: hub}
}

// Handler returns the HTTP handler for relay endpoints.
func (s *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/auth/login", s.handleLogin)
	mux.HandleFunc("/auth/refresh", s.handleRefresh)
	mux.HandleFunc("/sessions", s.handleListSessions)
	mux.HandleFunc("/users", s.handleUsers)
	mux.HandleFunc("/users/", s.handleUserAction)
	mux.HandleFunc("/share/create", s.handleShareCreate)
	mux.HandleFunc("/share/revoke", s.handleShareRevoke)
	mux.HandleFunc("/ws/host", s.handleWSHost)
	mux.HandleFunc("/ws/client", s.handleWSClient)
	return mux
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
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
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	now := time.Now().UTC()
	access, err := s.Store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	refresh, err := s.Store.CreateRefreshToken(user.Username, DefaultRefreshTokenTTL, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist state")
		return
	}
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	now := time.Now().UTC()
	refresh, err := s.Store.ValidateRefreshToken(req.RefreshToken, now)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	access, err := s.Store.CreateAccessToken(refresh.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if err := s.persist(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist state")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:      access.Token,
		AccessExpiresAt:  access.ExpiresAt,
		RefreshToken:     refresh.Token,
		RefreshExpiresAt: refresh.ExpiresAt,
	})
}

func (s *HTTPServer) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	username, err := s.requireAuth(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if s.Store == nil {
		writeError(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	sessions := s.Store.ListSessions(username)
	writeJSON(w, http.StatusOK, sessions)
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
		writeError(w, http.StatusUnauthorized, err.Error())
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
		writeError(w, http.StatusUnauthorized, err.Error())
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
	_, err := s.requireAuth(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
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

type shareRevokeRequest struct {
	Token string `json:"token"`
}

func (s *HTTPServer) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_, err := s.requireAuth(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
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

func (s *HTTPServer) handleWSHost(w http.ResponseWriter, r *http.Request) {
	username, err := s.requireAuth(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	ctx := withConnectionContext(r.Context(), string(RoleHost))
	logger := s.loggerWithContext(ctx).With("role", "host")

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: false,
	})
	if err != nil {
		return
	}
	ws := newWSConn(newConnID(), RoleHost, "", ShareScopeControl, conn, logger)
	defer func() {
		_ = ws.Close(ctx, "closing")
		s.Hub.Unregister(ws)
	}()

	frame, err := readFrame(ctx, conn, wsReadLimit)
	if err != nil {
		logger.Debug("failed to read hello", "err", err)
		return
	}
	if frame.GetHello() == nil || frame.SessionId == "" {
		_ = ws.Send(ctx, frameError("missing hello"))
		return
	}
	ws.sessionID = frame.SessionId
	cols := int(frame.GetHello().Cols)
	rows := int(frame.GetHello().Rows)
	if s.Store != nil {
		now := time.Now().UTC()
		session, ok := s.Store.Sessions[frame.SessionId]
		if ok && session.Username != username {
			_ = ws.Send(ctx, frameError("session belongs to another user"))
			return
		}
		if !ok {
			session = Session{
				ID:           frame.SessionId,
				Username:     username,
				CreatedAt:    now,
				LastActiveAt: now,
				Status:       "active",
			}
		} else {
			session.LastActiveAt = now
			session.Status = "active"
		}
		s.Store.CreateSession(session)
		s.Store.SetActiveSession(ActiveSession{
			SessionID:  frame.SessionId,
			Cols:       cols,
			Rows:       rows,
			LastSeenAt: now,
		})
		if err := s.persist(); err != nil {
			_ = ws.Send(ctx, frameError("failed to persist state"))
			return
		}
	}
	if err := s.Hub.RegisterHost(ws, frame.SessionId, cols, rows); err != nil {
		_ = ws.Send(ctx, frameError(err.Error()))
		return
	}
	logger.Info("host connected", "session", frame.SessionId)

	s.serveWSLoop(ctx, ws)
}

func (s *HTTPServer) handleWSClient(w http.ResponseWriter, r *http.Request) {
	ctx := withConnectionContext(r.Context(), string(RoleClient))
	logger := s.loggerWithContext(ctx).With("role", "client")
	var sessionID string
	scope := ShareScopeControl
	var username string
	shareToken := r.URL.Query().Get("token")

	if shareToken != "" {
		if s.Store == nil {
			writeError(w, http.StatusInternalServerError, "store unavailable")
			return
		}
		share, ok := s.Store.GetShareToken(shareToken)
		if !ok || share.IsExpired(time.Now().UTC()) {
			writeError(w, http.StatusUnauthorized, "invalid share token")
			return
		}
		sessionID = share.SessionID
		scope = share.Scope
	} else {
		user, err := s.requireAuth(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		username = user
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
		logger.Debug("failed to read hello", "err", err)
		return
	}
	if frame.GetHello() == nil {
		_ = ws.Send(ctx, frameError("missing hello"))
		return
	}
	if frame.SessionId != "" {
		sessionID = frame.SessionId
	}
	if sessionID == "" {
		_ = ws.Send(ctx, frameError("missing session"))
		return
	}
	if shareToken == "" && s.Store != nil {
		if session, ok := s.Store.Sessions[sessionID]; ok && session.Username != username {
			_ = ws.Send(ctx, frameError("session belongs to another user"))
			return
		}
	}
	ws.sessionID = sessionID
	granted, holder, cols, rows := s.Hub.RegisterClient(ws, sessionID, frame.GetHello().ClientId, frame.GetHello().WantsControl)
	if !s.Hub.HasHost(sessionID) {
		_ = ws.Send(ctx, frameError("no host connected"))
		return
	}
	_ = ws.Send(ctx, frameWelcome(granted, cols, rows, holder, sessionID))
	if granted {
		s.Hub.BroadcastControl(ctx, sessionID)
	}
	_ = s.Hub.HandleClientFrame(ctx, ws, frame)

	s.serveWSLoop(ctx, ws)
}

func (s *HTTPServer) serveWSLoop(ctx context.Context, ws *wsConn) {
	pingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.pingLoop(pingCtx, ws)

	for {
		frame, err := readFrame(ctx, ws.conn, wsReadLimit)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return
			}
			return
		}
		frame.SessionId = ws.sessionID

		switch ws.role {
		case RoleHost:
			if err := s.Hub.HandleHostFrame(ctx, ws, frame); err != nil {
				_ = ws.Send(ctx, frameError(err.Error()))
			}
		case RoleClient:
			if err := s.Hub.HandleClientFrame(ctx, ws, frame); err != nil {
				_ = ws.Send(ctx, frameError(err.Error()))
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
			if err := conn.Ping(pingCtx); err != nil && conn.logger != nil {
				conn.logger.Debug("websocket ping failed", "err", err)
			}
			cancel()
		}
	}
}

func (s *HTTPServer) requireAuth(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("missing authorization")
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", errors.New("invalid authorization")
	}
	token := strings.TrimSpace(parts[1])
	if s.Store == nil {
		return "", errors.New("store unavailable")
	}
	access, err := s.Store.ValidateAccessToken(token, time.Now().UTC())
	if err != nil {
		return "", err
	}
	return access.Username, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
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
			s.Logger.Error("failed to persist relay state", "err", err)
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
			s.Logger.Error("failed to persist users", "err", err)
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
