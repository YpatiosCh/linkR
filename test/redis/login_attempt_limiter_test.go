package redis_test

import (
	"context"
	"testing"
	"time"

	"linkMe/internal/redis"

	"github.com/alicebob/miniredis/v2"
)

func TestLoginAttemptLimiter_AllowsUnderLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	limiter := redis.NewLoginAttemptLimiter(client, 3, time.Minute)

	for i := 0; i < 3; i++ {
		allowed, err := limiter.Allow(context.Background(), "user@example.com")
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", i+1, err)
		}
		if !allowed {
			t.Fatalf("attempt %d: expected allowed, got denied", i+1)
		}
	}
}

func TestLoginAttemptLimiter_RejectsOverLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	limiter := redis.NewLoginAttemptLimiter(client, 2, time.Minute)

	for i := 0; i < 2; i++ {
		allowed, err := limiter.Allow(context.Background(), "user@example.com")
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", i+1, err)
		}
		if !allowed {
			t.Fatalf("attempt %d: expected allowed, got denied", i+1)
		}
	}

	allowed, err := limiter.Allow(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("expected denied once over limit")
	}
}

func TestLoginAttemptLimiter_DifferentEmailsAreIndependent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	limiter := redis.NewLoginAttemptLimiter(client, 1, time.Minute)

	allowed, err := limiter.Allow(context.Background(), "a@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected a@example.com's first attempt to be allowed")
	}

	// a@example.com is now at its limit, but b@example.com has its own counter.
	allowed, err = limiter.Allow(context.Background(), "b@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected b@example.com to be unaffected by a@example.com's limit")
	}
}

func TestLoginAttemptLimiter_WindowResets(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	limiter := redis.NewLoginAttemptLimiter(client, 1, time.Minute)

	allowed, err := limiter.Allow(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected first attempt to be allowed")
	}

	allowed, err = limiter.Allow(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected second attempt within the window to be denied")
	}

	mr.FastForward(time.Minute + time.Second)

	allowed, err = limiter.Allow(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("expected attempt after the window expired to be allowed")
	}
}
