package redis

import (
	"context"
	"fmt"
	"linkMe/internal/utils/reqctx"
	"linkMe/internal/utils/response"
	"net/http"
	"time"
)

// NewRateLimiter returns a per-IP fixed-window rate limiting middleware,
// shared across every server instance via client: at most limit requests
// per window. name namespaces the counters in Redis — since the counter
// store is now shared rather than one isolated Go map per call site, each
// route needs an explicit identity. IP extraction is reqctx.ClientIP —
// X-Real-IP is preferred (set by a trusted reverse proxy); RemoteAddr is
// the fallback.
func NewRateLimiter(client *Client, name string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, err := allow(r.Context(), client, name, reqctx.ClientIP(r), limit, window)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, response.CodeInternalError, "something went wrong")
				return
			}
			if !allowed {
				response.Error(w, http.StatusTooManyRequests, response.CodeTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// allow increments the fixed-window counter for name+ip and reports whether
// the request is within limit. The window's expiry is set only on the
// first increment of a fresh window, mirroring the in-process
// count+resetAt logic this replaced.
func allow(ctx context.Context, client *Client, name, ip string, limit int, window time.Duration) (bool, error) {
	key := "ratelimit:" + name + ":" + ip

	count, err := client.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("incrementing rate limit counter: %w", err)
	}
	if count == 1 {
		if err := client.Expire(ctx, key, window).Err(); err != nil {
			return false, fmt.Errorf("setting rate limit window: %w", err)
		}
	}
	return count <= int64(limit), nil
}
