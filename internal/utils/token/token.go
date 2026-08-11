package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// tokenBytes is the number of cryptographically random bytes each generated token contains.
const tokenBytes = 32

// Generate returns a new opaque auth token as a base64url string without padding.
// It reads tokenBytes (32) random bytes from crypto/rand and encodes them with
// base64.RawURLEncoding, yielding a URL-safe token; it returns the token, or an error
// if the random source fails.
func Generate() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Hash returns the hex-encoded SHA-256 digest of the raw token.
// Tokens are stored only in hashed form so that a database leak does not expose usable
// tokens; the hash is deterministic, so lookups compare hashed input against stored hashes.
func Hash(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}
