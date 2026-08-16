package middleware

import (
	"linkMe/internal/utils/logctx"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// RequestLogger returns an HTTP middleware that assigns each request a
// unique request ID, injects a logger enriched with that ID into the
// request context (retrievable via LoggerFromContext, or by any layer via
// logctx.FromContext), sets the ID on the X-Request-ID response header for
// client-side correlation, and emits one structured access-log line per
// request after it completes, recording method, path, status, and
// duration.
//
// RequestLogger should be the outermost middleware in router.SetupRoutes
// (wrapping SecurityHeaders/CORS/the mux) so it observes every request,
// including CORS preflights, requests rejected by auth or rate limiting,
// and unmatched routes — and so the request ID exists before anything else
// runs.
func RequestLogger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			w.Header().Set("X-Request-ID", requestID)

			logger := base.With("request_id", requestID)
			ctx := logctx.WithLogger(r.Context(), logger)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))

			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// LoggerFromContext retrieves the request-scoped logger injected by
// RequestLogger, given the *http.Request — the same calling convention as
// AuthClaims. It never returns nil; see logctx.FromContext for the
// fallback behavior when called outside the middleware chain.
func LoggerFromContext(r *http.Request) *slog.Logger {
	return logctx.FromContext(r.Context())
}

// statusRecorder wraps http.ResponseWriter to capture the status code
// written by the handler, which net/http does not otherwise expose after
// the fact. It defaults to 200 OK, matching net/http's own behavior when a
// handler calls Write without first calling WriteHeader.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (rec *statusRecorder) WriteHeader(status int) {
	if rec.wroteHeader {
		return
	}
	rec.status = status
	rec.wroteHeader = true
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	if !rec.wroteHeader {
		rec.WriteHeader(http.StatusOK)
	}
	return rec.ResponseWriter.Write(b)
}
