package middleware

import (
	"linkMe/internal/utils/logctx"
	"linkMe/internal/utils/response"
	"net/http"
	"runtime/debug"
)

// Recover returns an HTTP middleware that recovers any panic from a
// downstream handler, logs it (with a stack trace) via the request-scoped
// logger, and responds with a generic 500 INTERNAL_ERROR instead of
// dropping the connection — replacing net/http's default per-connection
// recovery (which returns no response and logs an unstructured trace to
// stderr, bypassing request-ID correlation).
//
// Recover must sit inside RequestLogger in router.SetupRoutes's middleware
// chain (RequestLogger → Recover → …) so the request-scoped logger is
// already in context when a panic is caught, and so RequestLogger's
// deferred access-log line still observes the resulting 500 status.
func Recover() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger := logctx.FromContext(r.Context())
					logger.Error("panic recovered",
						"panic", rec,
						"stack", string(debug.Stack()),
					)
					response.Error(w, http.StatusInternalServerError, response.CodeInternalError, "something went wrong")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
