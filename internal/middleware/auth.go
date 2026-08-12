package middleware

import (
	"context"
	"linkMe/internal/utils/jwttoken"
	"linkMe/internal/utils/response"
	"net/http"
	"strings"
)

// authContextKey is the unexported type used to store auth claims in a
// request context, preventing collisions with other context values.
type authContextKey struct{}

// RequireAuth returns an HTTP middleware that enforces a valid access token on
// every request it wraps. It first checks the Authorization header for a
// Bearer token, then falls back to the access_token cookie. On a valid token
// it injects the parsed jwttoken.Claims into the request context (retrievable
// via AuthClaims); on a missing or invalid token it writes 401 UNAUTHORIZED
// and stops the chain.
func RequireAuth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := bearerToken(r)
			if tokenStr == "" {
				if cookie, err := r.Cookie("access_token"); err == nil {
					tokenStr = cookie.Value
				}
			}
			if tokenStr == "" {
				response.Error(w, http.StatusUnauthorized, response.CodeUnauthorized, "authentication required")
				return
			}

			claims, err := jwttoken.Verify(jwtSecret, tokenStr)
			if err != nil {
				response.Error(w, http.StatusUnauthorized, response.CodeUnauthorized, "authentication required")
				return
			}

			ctx := context.WithValue(r.Context(), authContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthClaims retrieves the jwttoken.Claims injected by RequireAuth from the
// request context. The second return value is false when called outside a
// protected route (i.e. RequireAuth was not in the middleware chain).
func AuthClaims(r *http.Request) (jwttoken.Claims, bool) {
	claims, ok := r.Context().Value(authContextKey{}).(jwttoken.Claims)
	return claims, ok
}

// bearerToken extracts the raw token from the Authorization header, stripping
// the "Bearer " prefix. It returns an empty string if the header is absent or
// does not follow the Bearer scheme.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
