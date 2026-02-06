package relay

import (
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestAuthenticatorValidate(t *testing.T) {
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}

	code, err := totp.GenerateCodeCustom(user.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	auth := NewAuthenticator(users)
	if _, err := auth.Validate(user.Username, DefaultTestPassword, code, time.Now()); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if _, err := auth.Validate(user.Username, "bad", code, time.Now()); err == nil {
		t.Fatalf("expected error for bad password")
	}
}
