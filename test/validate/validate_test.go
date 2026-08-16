package validate_test

import (
	"strings"
	"testing"

	"linkMe/internal/utils/validate"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"user@example.com", "user@example.com"},
		{"  User@Example.com  ", "user@example.com"},
		{"MiXeD@CASE.IO", "mixed@case.io"},
		{"\tpadded@tabs.com\n", "padded@tabs.com"},
	}
	for _, tc := range tests {
		if got := validate.NormalizeEmail(tc.in); got != tc.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEmail(t *testing.T) {
	valid := []string{"user@example.com", "a.b+tag@sub.example.co"}
	for _, e := range valid {
		if !validate.Email(e) {
			t.Errorf("Email(%q) = false, want true", e)
		}
	}
	invalid := []string{
		"", "not-an-email", "@example.com", "user@", "user example.com",
		"Attacker Name <attacker@example.com>",                          // display-name form rejected
		"user@" + strings.Repeat("a", validate.MaxEmailLength) + ".com", // too long
	}
	for _, e := range invalid {
		if validate.Email(e) {
			t.Errorf("Email(%q) = true, want false", e)
		}
	}
}

// pwOfLen builds a password of exactly n bytes that satisfies the
// composition rule (at least one upper/lower/digit/special char): a fixed
// 4-char prefix covering all four classes, padded with lowercase 'a' to
// reach the target length. n must be >= 4.
func pwOfLen(n int) string {
	const prefix = "Aa1!"
	return prefix + strings.Repeat("a", n-len(prefix))
}

func TestPassword(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"too short", pwOfLen(validate.MinPasswordLength - 1), false},
		{"minimum length", pwOfLen(validate.MinPasswordLength), true},
		{"maximum length", pwOfLen(validate.MaxPasswordLength), true},
		{"too long", pwOfLen(validate.MaxPasswordLength + 1), false},
		{"empty", "", false},
		{"missing uppercase", "correct1!horse2!staple", false},
		{"missing lowercase", "CORRECT1!HORSE2!STAPLE", false},
		{"missing digit", "Correct-Horse-Staple!!", false},
		{"missing special", "Correct1Horse2Staple33", false},
		{"all four classes", "Correct-Horse1!Staple", true},
	}
	for _, tc := range tests {
		if got := validate.Password(tc.in); got != tc.want {
			t.Errorf("%s: Password(len=%d) = %v, want %v", tc.name, len(tc.in), got, tc.want)
		}
	}
}

func TestName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"simple", "Ada", true},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"maximum length", strings.Repeat("a", validate.MaxNameLength), true},
		{"too long", strings.Repeat("a", validate.MaxNameLength+1), false},
		{"control character", "Ada\x00Lovelace", false},
		{"bidi override", "Ada‮Lovelace", false},
		{"invalid UTF-8", "Ada\xffLovelace", false},
		{"newline not allowed (single-line field)", "Ada\nLovelace", false},
	}
	for _, tc := range tests {
		if got := validate.Name(tc.in); got != tc.want {
			t.Errorf("%s: Name(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestCompanyName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"simple", "Jane's Templates", true},
		{"empty is valid (clears the field)", "", true},
		{"maximum length", strings.Repeat("a", validate.MaxCompanyNameLength), true},
		{"too long", strings.Repeat("a", validate.MaxCompanyNameLength+1), false},
		{"control character", "Jane\x00's Templates", false},
		{"bidi override", "Jane‮'s Templates", false},
	}
	for _, tc := range tests {
		if got := validate.CompanyName(tc.in); got != tc.want {
			t.Errorf("%s: CompanyName(len=%d) = %v, want %v", tc.name, len(tc.in), got, tc.want)
		}
	}
}

func TestDescription(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"simple", "I make Notion templates.", true},
		{"empty is valid (clears the field)", "", true},
		{"maximum length", strings.Repeat("a", validate.MaxDescriptionLength), true},
		{"too long", strings.Repeat("a", validate.MaxDescriptionLength+1), false},
		{"bare newline allowed (multi-line field)", "Line one\nLine two", true},
		{"carriage return not allowed", "Line one\rLine two", false},
		{"control character", "Bio\x00here", false},
		{"bidi override", "Bio‮here", false},
	}
	for _, tc := range tests {
		if got := validate.Description(tc.in); got != tc.want {
			t.Errorf("%s: Description(len=%d) = %v, want %v", tc.name, len(tc.in), got, tc.want)
		}
	}
}

func TestURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"https", "https://discord.gg/xyz", true},
		{"http", "http://example.com", true},
		{"empty is valid (clears the field)", "", true},
		{"no scheme", "discord.gg/xyz", false},
		{"non-http scheme", "ftp://example.com", false},
		{"no host", "https://", false},
		{"too long", "https://example.com/" + strings.Repeat("a", validate.MaxSocialURLLength), false},
		{"control character", "https://example.com/\x00", false},
	}
	for _, tc := range tests {
		if got := validate.URL(tc.in); got != tc.want {
			t.Errorf("%s: URL(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestCustomLinkLabel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"simple", "Slack", true},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"maximum length", strings.Repeat("a", validate.MaxCustomLinkLabelLength), true},
		{"too long", strings.Repeat("a", validate.MaxCustomLinkLabelLength+1), false},
		{"control character", "Sla\x00ck", false},
		{"bidi override", "Sla‮ck", false},
	}
	for _, tc := range tests {
		if got := validate.CustomLinkLabel(tc.in); got != tc.want {
			t.Errorf("%s: CustomLinkLabel(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}
