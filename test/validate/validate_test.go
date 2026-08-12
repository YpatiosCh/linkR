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
	invalid := []string{"", "not-an-email", "@example.com", "user@", "user example.com"}
	for _, e := range invalid {
		if validate.Email(e) {
			t.Errorf("Email(%q) = true, want false", e)
		}
	}
}

func TestPassword(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"too short", strings.Repeat("a", validate.MinPasswordLength-1), false},
		{"minimum length", strings.Repeat("a", validate.MinPasswordLength), true},
		{"maximum length", strings.Repeat("a", validate.MaxPasswordLength), true},
		{"too long", strings.Repeat("a", validate.MaxPasswordLength+1), false},
		{"empty", "", false},
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
	}
	for _, tc := range tests {
		if got := validate.Name(tc.in); got != tc.want {
			t.Errorf("%s: Name(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}
