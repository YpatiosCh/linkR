package hash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 64 * 1024 // 64 MB
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLength  = 16
	argonKeyLength   = 32
)

// HashPassword hashes password with Argon2id using a freshly generated
// random salt of argonSaltLength bytes and the package's default memory,
// iteration, parallelism, and key-length parameters. It returns the hash
// encoded in the PHC string format ("$argon2id$v=...$m=...,t=...,p=...$salt$hash")
// suitable for storage, or an error if generating the salt fails.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		argonIterations,
		argonMemory,
		argonParallelism,
		argonKeyLength,
	)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)

	return encoded, nil
}

// VerifyPassword reports whether password matches the encodedHash
// produced by HashPassword. It parses the PHC-formatted hash, validates
// the Argon2 version, recomputes the key with the stored salt and
// parameters, and compares the result with a constant-time comparison to
// avoid timing side channels. It returns an error if the hash is
// malformed, has an incompatible version, or cannot be base64-decoded.
func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, fmt.Errorf("invalid hash format")
	}

	var version int
	var memory, iterations uint32
	var parallelism uint8

	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return false, fmt.Errorf("parsing version: %w", err)
	}
	if version != argon2.Version {
		return false, fmt.Errorf("incompatible argon2 version")
	}

	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return false, fmt.Errorf("parsing params: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decoding salt: %w", err)
	}

	storedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decoding hash: %w", err)
	}

	computedHash := argon2.IDKey(
		[]byte(password),
		salt,
		iterations,
		memory,
		parallelism,
		uint32(len(storedHash)),
	)

	match := subtle.ConstantTimeCompare(storedHash, computedHash) == 1
	return match, nil
}
