package relay

import (
	"errors"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// Default test credentials for early development.
const (
	DefaultTestUsername   = "test"
	DefaultTestPassword   = "test"
	DefaultTestTOTPSecret = "JBSWY3DPEHPK3PXP"
)

// ErrInvalidCredentials is returned when authentication fails.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Authenticator validates login credentials.
type Authenticator struct {
	Users *UserStore
}

// NewAuthenticator returns an Authenticator.
func NewAuthenticator(users *UserStore) *Authenticator {
	return &Authenticator{Users: users}
}

// Validate checks username/password/TOTP.
func (a *Authenticator) Validate(username, password, code string, now time.Time) (User, error) {
	if a.Users == nil {
		return User{}, ErrInvalidCredentials
	}
	user, ok := a.Users.Get(username)
	if !ok {
		return User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	valid, err := totp.ValidateCustom(code, user.TOTPSecret, now, totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil || !valid {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

// SeedTestUser ensures the test user exists in the store.
func SeedTestUser(store *UserStore) (User, error) {
	if store != nil {
		if user, ok := store.Get(DefaultTestUsername); ok {
			return user, nil
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(DefaultTestPassword), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	user := User{
		Username:     DefaultTestUsername,
		PasswordHash: string(hash),
		TOTPSecret:   DefaultTestTOTPSecret,
		CreatedAt:    time.Now().UTC(),
	}
	store.Upsert(user)
	return user, nil
}
