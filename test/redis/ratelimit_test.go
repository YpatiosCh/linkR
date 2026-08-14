package redis_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"linkMe/internal/redis"

	"github.com/alicebob/miniredis/v2"
)

// sentinelHandler records whether it was called and writes 200 OK.
type sentinelHandler struct{ calls int }

func (s *sentinelHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.calls++
	w.WriteHeader(http.StatusOK)
}

func newRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.RemoteAddr = "203.0.113.1:54321"
	return req
}

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	sentinel := &sentinelHandler{}
	handler := redis.NewRateLimiter(client, "test-route", 3, time.Minute)(sentinel)

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, newRequest())
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
	if sentinel.calls != 3 {
		t.Errorf("expected inner handler called 3 times, got %d", sentinel.calls)
	}
}

func TestRateLimiter_RejectsOverLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	sentinel := &sentinelHandler{}
	handler := redis.NewRateLimiter(client, "test-route", 2, time.Minute)(sentinel)

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, newRequest())
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequest())
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 once over limit, got %d", rec.Code)
	}
	if sentinel.calls != 2 {
		t.Errorf("inner handler must not be called once over limit, got %d calls", sentinel.calls)
	}
}

func TestRateLimiter_DifferentNamesAreIndependent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	sentinel := &sentinelHandler{}
	routeA := redis.NewRateLimiter(client, "route-a", 1, time.Minute)(sentinel)
	routeB := redis.NewRateLimiter(client, "route-b", 1, time.Minute)(sentinel)

	rec := httptest.NewRecorder()
	routeA.ServeHTTP(rec, newRequest())
	if rec.Code != http.StatusOK {
		t.Fatalf("route-a first request: expected 200, got %d", rec.Code)
	}

	// route-a is now at its limit, but route-b has its own counter.
	rec = httptest.NewRecorder()
	routeB.ServeHTTP(rec, newRequest())
	if rec.Code != http.StatusOK {
		t.Errorf("route-b should be unaffected by route-a's limit, got %d", rec.Code)
	}
}

func TestRateLimiter_WindowResets(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	sentinel := &sentinelHandler{}
	handler := redis.NewRateLimiter(client, "test-route", 1, time.Minute)(sentinel)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequest())
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequest())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request within the window: expected 429, got %d", rec.Code)
	}

	mr.FastForward(time.Minute + time.Second)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequest())
	if rec.Code != http.StatusOK {
		t.Errorf("request after the window expired: expected 200, got %d", rec.Code)
	}
}
