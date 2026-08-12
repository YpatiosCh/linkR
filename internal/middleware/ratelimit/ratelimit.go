package ratelimit

import (
	"linkMe/internal/utils/response"
	"net/http"
	"strings"
	"sync"
	"time"
)

type windowEntry struct {
	count   int
	resetAt time.Time
}

type limiter struct {
	mu      sync.Mutex
	entries map[string]*windowEntry
	limit   int
	window  time.Duration
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.After(e.resetAt) {
		l.entries[key] = &windowEntry{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if e.count >= l.limit {
		return false
	}
	e.count++
	return true
}

// New returns a per-IP fixed-window rate limiting middleware that allows at
// most limit requests per window. Each call to New creates an independent
// counter store, so each route gets its own limit with no shared state.
//
// IP extraction: X-Real-IP is preferred (set by a trusted reverse proxy);
// RemoteAddr is the fallback.
func New(limit int, window time.Duration) func(http.Handler) http.Handler {
	l := &limiter{
		entries: make(map[string]*windowEntry),
		limit:   limit,
		window:  window,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.allow(clientIP(r)) {
				response.Error(w, http.StatusTooManyRequests, response.CodeTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}
