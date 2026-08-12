package middleware

import "net/http"

// SecurityHeaders returns an HTTP middleware that sets defensive security
// headers on every response. HSTS is only added in production because it
// mandates HTTPS and would break local HTTP development.
//
// Since this server returns only JSON (never HTML), the CSP of "default-src
// 'none'" is safe and appropriate.
func SecurityHeaders(appEnv string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", "default-src 'none'")
			if appEnv == "production" {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}
