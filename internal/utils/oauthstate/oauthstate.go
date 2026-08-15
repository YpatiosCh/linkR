// Package oauthstate signs and verifies the short-lived CSRF `state` value
// used by the Google OAuth redirect flow, and carries it in an HttpOnly
// cookie between the start and callback requests. It is unrelated to the
// application's login session (see internal/utils/cookies for that).
package oauthstate

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const (
	// CookieName is the cookie carrying the signed state value.
	CookieName = "oauth_state"

	cookieTTL = 10 * time.Minute
)

// Sign returns an HMAC-SHA256-signed cookie value binding raw to secret, as
// "<raw>.<hex hmac>", so a forged cookie can't validate without secret.
func Sign(secret, raw string) string {
	return raw + "." + hex.EncodeToString(mac(secret, raw))
}

// Verify reports whether cookieValue is a validly-signed state (per Sign)
// whose raw value equals queryState. Comparisons are constant-time.
func Verify(secret, cookieValue, queryState string) bool {
	raw, sig, ok := strings.Cut(cookieValue, ".")
	if !ok || raw == "" || sig == "" {
		return false
	}

	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	if !hmac.Equal(sigBytes, mac(secret, raw)) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(raw), []byte(queryState)) == 1
}

// SetCookie writes the signed state cookie: HttpOnly + Secure + SameSite=Lax,
// scoped to cookieTTL, mirroring internal/utils/cookies' pattern.
func SetCookie(w http.ResponseWriter, signedValue string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    signedValue,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   int(cookieTTL.Seconds()),
	})
}

// ClearCookie expires the state cookie immediately, mirroring
// cookies.ClearTokenCookies.
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
	})
}

// mac computes the HMAC-SHA256 of raw keyed by secret.
func mac(secret, raw string) []byte {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(raw))
	return h.Sum(nil)
}
