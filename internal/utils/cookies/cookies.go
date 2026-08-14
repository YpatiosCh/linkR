// Package cookies writes and clears the HttpOnly authentication cookies
// shared by the auth handlers.
package cookies

import (
	"linkMe/internal/models"
	"linkMe/internal/utils/jwttoken"
	"net/http"
	"time"
)

const (
	// AccessTokenCookie is the cookie name carrying the short-lived JWT.
	AccessTokenCookie = "access_token"
	// RefreshTokenCookie is the cookie name carrying the opaque refresh token.
	RefreshTokenCookie = "refresh_token"

	refreshTokenLifetime = 30 * 24 * time.Hour
)

// SetTokenCookies writes both auth cookies to the response. The refresh_token
// is long-lived (30 days) and the access_token is short-lived (15 minutes).
// Both are HttpOnly + Secure + SameSite=Lax so they cannot be accessed by
// client-side JavaScript and are not sent on cross-site requests.
func SetTokenCookies(w http.ResponseWriter, pair models.TokenPair) {
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookie,
		Value:    pair.RawRefreshToken,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   int(refreshTokenLifetime.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     AccessTokenCookie,
		Value:    pair.AccessToken,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   int(jwttoken.AccessTokenDuration.Seconds()),
	})
}

// ClearTokenCookies expires both authentication cookies by setting MaxAge=-1,
// causing the browser to delete them immediately. Called during logout flows.
func ClearTokenCookies(w http.ResponseWriter) {
	for _, name := range []string{AccessTokenCookie, RefreshTokenCookie} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
			MaxAge:   -1,
		})
	}
}
