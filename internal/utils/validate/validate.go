package validate

import (
	"net/mail"
	"strings"
)

const (
	// MinPasswordLength is the minimum accepted password length in bytes.
	MinPasswordLength = 8
	// MaxPasswordLength is the maximum accepted password length in bytes (matches bcrypt's 72-byte limit).
	MaxPasswordLength = 72
	// MaxNameLength is the maximum accepted name length in bytes.
	MaxNameLength = 100
)

// NormalizeEmail lowercases and trims surrounding whitespace from an email address so that
// equivalent addresses compare equal and are stored consistently.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Email reports whether the given string parses as a valid email address according to net/mail.
func Email(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}

// Password reports whether the password length in bytes is within the accepted range
// [MinPasswordLength, MaxPasswordLength].
func Password(password string) bool {
	length := len(password)
	return length >= MinPasswordLength && length <= MaxPasswordLength
}

// Name reports whether the name is non-empty after trimming whitespace and no longer than
// MaxNameLength bytes.
func Name(name string) bool {
	trimmed := strings.TrimSpace(name)
	return len(trimmed) > 0 && len(trimmed) <= MaxNameLength
}
