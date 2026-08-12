package token_test

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"linkMe/internal/utils/token"
)

// tokenBytes mirrors the unexported constant in the token package: Generate
// reads 32 cryptographically random bytes. Kept here so the black-box test
// can assert the decoded length without reaching into package internals.
const tokenBytes = 32

func TestGenerateProducesUniqueDecodableTokens(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		tok, err := token.Generate()
		if err != nil {
			t.Fatalf("Generate() returned error: %v", err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("token %q is not valid base64url: %v", tok, err)
		}
		if len(raw) != tokenBytes {
			t.Fatalf("expected %d random bytes, got %d", tokenBytes, len(raw))
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("Generate() produced a duplicate token: %q", tok)
		}
		seen[tok] = struct{}{}
	}
}

func TestHashIsDeterministicAndSized(t *testing.T) {
	const raw = "some-opaque-token"
	first := token.Hash(raw)
	if first != token.Hash(raw) {
		t.Error("Hash is not deterministic for the same input")
	}
	// SHA-256 hex digest is 64 characters.
	if len(first) != hex.EncodedLen(32) {
		t.Errorf("expected %d-char hex digest, got %d", hex.EncodedLen(32), len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Errorf("digest is not valid hex: %v", err)
	}
	if token.Hash("different-token") == first {
		t.Error("Hash collided for distinct inputs")
	}
}
