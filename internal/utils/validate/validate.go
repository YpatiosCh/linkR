package validate

import (
	"net/mail"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MinPasswordLength is the minimum accepted password length in bytes.
	MinPasswordLength = 12
	// MaxPasswordLength is the maximum accepted password length in bytes. Argon2id
	// has no practical input-length limit; 72 is simply a generous, fixed ceiling.
	MaxPasswordLength = 72
	// MaxEmailLength is the maximum accepted email address length in bytes
	// (RFC 5321's total reverse-path length limit).
	MaxEmailLength = 254
	// MaxNameLength is the maximum accepted name length in bytes.
	MaxNameLength = 100
	// MaxCompanyNameLength is the maximum accepted company name length in bytes.
	MaxCompanyNameLength = 150
	// MaxDescriptionLength is the maximum accepted profile description length in bytes.
	MaxDescriptionLength = 500
	// MaxSocialURLLength is the maximum accepted length in bytes for a profile
	// URL (avatar, website, or any social link).
	MaxSocialURLLength = 300
	// MaxCustomLinkLabelLength is the maximum accepted length in bytes for a
	// CustomSocialLink's label.
	MaxCustomLinkLabelLength = 40
)

// NormalizeEmail lowercases and trims surrounding whitespace from an email address so that
// equivalent addresses compare equal and are stored consistently.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Email reports whether the given string is a valid, bare email address
// (e.g. "user@example.com") no longer than MaxEmailLength bytes. It rejects
// the RFC 5322 "Display Name <addr>" form — net/mail.ParseAddress accepts
// that form, so the parsed address is compared back against the full input
// to ensure only a bare address was given.
func Email(email string) bool {
	if len(email) > MaxEmailLength {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return addr.Address == email
}

// Password reports whether password is within the accepted length range
// [MinPasswordLength, MaxPasswordLength] and contains at least one
// uppercase letter, one lowercase letter, one digit, and one special
// character (anything that is not a letter, digit, or whitespace).
func Password(password string) bool {
	length := len(password)
	if length < MinPasswordLength || length > MaxPasswordLength {
		return false
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case !unicode.IsSpace(r):
			hasSpecial = true
		}
	}
	return hasUpper && hasLower && hasDigit && hasSpecial
}

// Name reports whether the name is non-empty after trimming whitespace, no
// longer than MaxNameLength bytes, and free of control characters and
// Unicode bidi-override characters.
func Name(name string) bool {
	trimmed := strings.TrimSpace(name)
	return len(trimmed) > 0 && len(trimmed) <= MaxNameLength && !hasDangerousChars(trimmed, false)
}

// CompanyName reports whether the company name is within MaxCompanyNameLength
// bytes and free of control characters and Unicode bidi-override characters.
// Unlike Name, an empty string is valid — it means the field is being
// cleared.
func CompanyName(name string) bool {
	return len(name) <= MaxCompanyNameLength && !hasDangerousChars(name, false)
}

// Description reports whether the profile description is within
// MaxDescriptionLength bytes and free of control characters (a bare '\n'
// is allowed, for paragraph breaks in a bio) and Unicode bidi-override
// characters. An empty string is valid — it means the field is being
// cleared.
func Description(description string) bool {
	return len(description) <= MaxDescriptionLength && !hasDangerousChars(description, true)
}

// URL reports whether s is a valid http(s) URL no longer than
// MaxSocialURLLength bytes, free of control characters and Unicode
// bidi-override characters. An empty string is valid — it means the field
// is being cleared.
func URL(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > MaxSocialURLLength || hasDangerousChars(s, false) {
		return false
	}
	parsed, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// CustomLinkLabel reports whether a CustomSocialLink's label is non-empty
// after trimming whitespace, no longer than MaxCustomLinkLabelLength bytes,
// and free of control characters and Unicode bidi-override characters.
func CustomLinkLabel(label string) bool {
	trimmed := strings.TrimSpace(label)
	return len(trimmed) > 0 && len(trimmed) <= MaxCustomLinkLabelLength && !hasDangerousChars(trimmed, false)
}

// hasDangerousChars reports whether s contains invalid UTF-8, a control
// character, or a Unicode bidirectional-override character (the
// "Trojan Source" class — these can make displayed text render in a
// different order than its actual byte sequence). allowNewline permits
// '\n' specifically, for multi-line fields like Description; '\r' is
// never allowed regardless.
func hasDangerousChars(s string, allowNewline bool) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r == '\n' && allowNewline {
			continue
		}
		if unicode.IsControl(r) {
			return true
		}
		switch r {
		case '‪', '‫', '‬', '‭', '‮', // LRE, RLE, PDF, LRO, RLO
			'⁦', '⁧', '⁨', '⁩': // LRI, RLI, FSI, PDI
			return true
		}
	}
	return false
}
