package relay

import (
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrUserExists indicates a duplicate username.
	ErrUserExists = errors.New("user already exists")
	// ErrUserNotFound indicates a missing user.
	ErrUserNotFound = errors.New("user not found")
	// ErrUsernameRequired indicates a missing username.
	ErrUsernameRequired = errors.New("username is required")
)

// UserCreateResult is returned when creating a user.
type UserCreateResult struct {
	User       User
	Password   string
	TOTPSecret string
	TOTPURL    string
}

// UserTOTPResult contains a rotated TOTP secret.
type UserTOTPResult struct {
	User       User
	TOTPSecret string
	TOTPURL    string
}

// UserPasswordResult contains a changed password.
type UserPasswordResult struct {
	User     User
	Password string
}

// CreateUser adds a new user with optional password generation.
func CreateUser(store *UserStore, username, password string, now time.Time) (UserCreateResult, error) {
	if store == nil {
		return UserCreateResult{}, errors.New("user store is nil")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return UserCreateResult{}, ErrUsernameRequired
	}
	if _, exists := store.Get(username); exists {
		return UserCreateResult{}, ErrUserExists
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if strings.TrimSpace(password) == "" {
		generated, err := generatePassword()
		if err != nil {
			return UserCreateResult{}, err
		}
		password = generated
	}
	secret, url, err := generateTOTP(username)
	if err != nil {
		return UserCreateResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return UserCreateResult{}, err
	}
	user := User{
		Username:     username,
		PasswordHash: string(hash),
		TOTPSecret:   secret,
		CreatedAt:    now,
	}
	store.Upsert(user)
	return UserCreateResult{
		User:       user,
		Password:   password,
		TOTPSecret: secret,
		TOTPURL:    url,
	}, nil
}

// RotateUserTOTP regenerates the TOTP secret for a user.
func RotateUserTOTP(store *UserStore, username string) (UserTOTPResult, error) {
	if store == nil {
		return UserTOTPResult{}, errors.New("user store is nil")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return UserTOTPResult{}, ErrUsernameRequired
	}
	user, ok := store.Get(username)
	if !ok {
		return UserTOTPResult{}, ErrUserNotFound
	}
	secret, url, err := generateTOTP(username)
	if err != nil {
		return UserTOTPResult{}, err
	}
	user.TOTPSecret = secret
	store.Upsert(user)
	return UserTOTPResult{
		User:       user,
		TOTPSecret: secret,
		TOTPURL:    url,
	}, nil
}

// ChangeUserPassword updates a user's password, generating one if empty.
func ChangeUserPassword(store *UserStore, username, password string) (UserPasswordResult, error) {
	if store == nil {
		return UserPasswordResult{}, errors.New("user store is nil")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return UserPasswordResult{}, ErrUsernameRequired
	}
	user, ok := store.Get(username)
	if !ok {
		return UserPasswordResult{}, ErrUserNotFound
	}
	if strings.TrimSpace(password) == "" {
		generated, err := generatePassword()
		if err != nil {
			return UserPasswordResult{}, err
		}
		password = generated
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return UserPasswordResult{}, err
	}
	user.PasswordHash = string(hash)
	store.Upsert(user)
	return UserPasswordResult{User: user, Password: password}, nil
}

// DeleteUser removes a user by username.
func DeleteUser(store *UserStore, username string) (User, error) {
	if store == nil {
		return User{}, errors.New("user store is nil")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, ErrUsernameRequired
	}
	user, ok := store.Delete(username)
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}
