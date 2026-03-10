package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// GenerateVerifier creates a cryptographically-random PKCE code verifier (RFC 7636).
// The returned value is a 43-128 character base64url-encoded string (no padding).
func GenerateVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate pkce verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ComputeChallenge derives the S256 code challenge from a verifier.
// challenge = BASE64URL(SHA256(ASCII(verifier)))
func ComputeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
