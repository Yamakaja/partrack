package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const linkTokenDomain = "link-token:"

var (
	errTokenExpired = errors.New("TOKEN_EXPIRED")
	errInvalidToken = errors.New("INVALID_TOKEN")
)

// signLinkToken marshals payload to JSON, signs it with HMAC-SHA256, and
// returns "base64url(json).base64url(sig)".
func signLinkToken(payload any, secret string) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sig := computeLinkTokenSig(data, secret)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// verifyLinkToken parses a token produced by signLinkToken, verifies the
// signature and expiry, then unmarshals the payload into dest.
func verifyLinkToken(token string, secret string, dest any) error {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return errInvalidToken
	}

	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errInvalidToken
	}

	expected := computeLinkTokenSig(data, secret)
	if !hmac.Equal(sig, expected) {
		return errInvalidToken
	}

	// Check expiry before unmarshalling into dest.
	var expCheck struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(data, &expCheck); err != nil {
		return errInvalidToken
	}
	if time.Now().After(expCheck.ExpiresAt) {
		return errTokenExpired
	}

	return json.Unmarshal(data, dest)
}

func computeLinkTokenSig(data []byte, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(linkTokenDomain))
	mac.Write(data)
	return mac.Sum(nil)
}
