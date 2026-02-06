package relay

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/pquerna/otp/totp"
)

const totpIssuer = "Lingon"

func generatePassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func generateTOTP(username string) (string, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: username,
	})
	if err != nil {
		return "", "", err
	}
	secret := strings.TrimSpace(key.Secret())
	if secret == "" {
		return "", "", fmt.Errorf("totp secret missing")
	}
	return secret, key.URL(), nil
}
