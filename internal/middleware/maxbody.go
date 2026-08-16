package middleware

import "net/http"

// MaxRequestBodyBytes is the request body size cap applied globally via
// MaxBody. 1MB is generous for today's text-only JSON API — none of it
// carries file uploads (those will go through a presigned R2 URL directly,
// never through this server, once product uploads are built).
const MaxRequestBodyBytes = 1 << 20 // 1MB

// MaxBody returns an HTTP middleware that caps the request body to limit
// bytes via http.MaxBytesReader — reading past the limit fails with an
// error, which the JSON decoder surfaces as a normal decode failure
// (response.DecodeJSON's 400 INVALID_BODY path).
func MaxBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
