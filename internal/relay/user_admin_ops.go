package relay

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// UserMutationRequest describes one local administrative operation.
type UserMutationRequest struct {
	Action   string `json:"action"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

// UserMutationResult contains only the values needed by the administrative CLI.
type UserMutationResult struct {
	Username   string    `json:"username"`
	CreatedAt  time.Time `json:"created_at"`
	Password   string    `json:"password,omitempty"`
	TOTPSecret string    `json:"totp_secret,omitempty"`
	TOTPURL    string    `json:"totp_url,omitempty"`
}

// UserMutator applies administrative changes to an authoritative user store.
type UserMutator interface {
	MutateUser(context.Context, UserMutationRequest) (UserMutationResult, error)
}

// MutateUser persists a user change and invalidates authority through this relay.
func (s *HTTPServer) MutateUser(ctx context.Context, req UserMutationRequest) (UserMutationResult, error) {
	if s == nil || s.Users == nil || s.Store == nil {
		return UserMutationResult{}, errStoreUnavailable
	}
	s.Users.mutationMu.Lock()
	defer s.Users.mutationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return UserMutationResult{}, err
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		return UserMutationResult{}, ErrUsernameRequired
	}
	var result UserMutationResult
	var err error
	switch req.Action {
	case "add":
		var created UserCreateResult
		created, err = CreateUser(s.Users, username, req.Password, time.Now().UTC())
		result = UserMutationResult{Username: created.User.Username, CreatedAt: created.User.CreatedAt, Password: created.Password, TOTPSecret: created.TOTPSecret, TOTPURL: created.TOTPURL}
	case "chpasswd":
		var changed UserPasswordResult
		changed, err = ChangeUserPassword(s.Users, username, req.Password)
		result = UserMutationResult{Username: changed.User.Username, CreatedAt: changed.User.CreatedAt, Password: changed.Password}
	case "rotate-totp":
		var changed UserTOTPResult
		changed, err = RotateUserTOTP(s.Users, username)
		result = UserMutationResult{Username: changed.User.Username, CreatedAt: changed.User.CreatedAt, TOTPSecret: changed.TOTPSecret, TOTPURL: changed.TOTPURL}
	case "delete":
		var removed User
		removed, err = DeleteUser(s.Users, username)
		result = UserMutationResult{Username: removed.Username, CreatedAt: removed.CreatedAt}
	default:
		return UserMutationResult{}, fmt.Errorf("unsupported user action")
	}
	if err != nil {
		return UserMutationResult{}, err
	}
	if req.Action == "add" {
		if err := s.persistUsers(); err != nil {
			return UserMutationResult{}, err
		}
		return result, nil
	}
	// Persist revocations before changed credentials so a restart between the
	// two atomic writes cannot combine new credentials with old authority.
	shares, sessions := s.invalidateUserAuth(username, time.Now().UTC())
	reason := "credentials changed"
	if req.Action == "delete" {
		reason = "user deleted"
	}
	defer s.disconnectUserAuth(username, shares, sessions, reason)
	if err := s.persist(); err != nil {
		return UserMutationResult{}, err
	}
	if err := s.persistUsers(); err != nil {
		return UserMutationResult{}, err
	}
	return result, nil
}
