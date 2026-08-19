package redis

import (
	"context"
	"fmt"
	"time"
)

// loginAttemptLimiterName namespaces the email-keyed login rate-limit
// counter in the shared Redis instance, distinct from the per-IP "login"
// bucket used by the route-level middleware (NewRateLimiter).
const loginAttemptLimiterName = "login-email"

// LoginAttemptLimiter throttles login attempts per normalized email,
// independent of the per-IP rate limiting NewRateLimiter already applies to
// the login route — this closes the gap where an attacker distributes
// login attempts against one account across many IPs.
type LoginAttemptLimiter struct {
	client *Client
	limit  int
	window time.Duration
}

// NewLoginAttemptLimiter builds a LoginAttemptLimiter backed by client,
// allowing at most limit login attempts per normalized email within window.
func NewLoginAttemptLimiter(client *Client, limit int, window time.Duration) *LoginAttemptLimiter {
	return &LoginAttemptLimiter{client: client, limit: limit, window: window}
}

// Allow reports whether another login attempt for email (already
// normalized by the caller) is permitted right now, incrementing its
// attempt counter as a side effect — the same fixed-window INCR+EXPIRE
// scheme as NewRateLimiter, just keyed on email instead of client IP.
func (l *LoginAttemptLimiter) Allow(ctx context.Context, email string) (bool, error) {
	allowed, err := allow(ctx, l.client, loginAttemptLimiterName, email, l.limit, l.window)
	if err != nil {
		return false, fmt.Errorf("checking login attempt limit: %w", err)
	}
	return allowed, nil
}
