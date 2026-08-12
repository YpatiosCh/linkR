package hash_test

import (
	"testing"

	"linkMe/pkg/hash"
)

func TestHashAndVerifyPassword(t *testing.T) {
	const password = "correct-horse-battery"

	encoded, err := hash.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if encoded == password {
		t.Fatal("HashPassword returned the plaintext password")
	}

	ok, err := hash.VerifyPassword(password, encoded)
	if err != nil {
		t.Fatalf("VerifyPassword returned error: %v", err)
	}
	if !ok {
		t.Error("VerifyPassword rejected the correct password")
	}

	ok, err = hash.VerifyPassword("wrong-password", encoded)
	if err != nil {
		t.Fatalf("VerifyPassword returned error for a wrong password: %v", err)
	}
	if ok {
		t.Error("VerifyPassword accepted an incorrect password")
	}
}

func TestHashPasswordProducesDistinctHashesForSamePassword(t *testing.T) {
	const password = "correct-horse-battery"

	first, err := hash.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	second, err := hash.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if first == second {
		t.Error("expected a random salt to make each hash unique")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	ok, err := hash.VerifyPassword("whatever", "not-a-valid-phc-hash")
	if err == nil {
		t.Error("expected an error for a malformed encoded hash")
	}
	if ok {
		t.Error("VerifyPassword must not report success on a malformed hash")
	}
}
