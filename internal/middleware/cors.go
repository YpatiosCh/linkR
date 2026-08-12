package middleware

import "net/http"

// CORS returns an HTTP middleware that enforces a CORS policy based on an
// explicit origin allowlist. For requests whose Origin header matches the list,
// it echoes that origin back (never "*") and sets credentials=true so the
// browser includes cookies. It also adds Vary: Origin so caches don't serve a
// cached preflight to a different origin.
//
// Preflight OPTIONS requests are answered with 204 and do not reach the next
// handler. Requests from origins not in the allowlist receive no CORS headers —
// the browser will block them.
//
// CSRF protection for this JSON-only API is provided by SameSite=Lax cookies,
// which prevent cross-origin requests from carrying credentials without needing
// a separate CSRF token.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Add("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				h.Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
