package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"linkMe/internal/middleware"
	"linkMe/internal/utils/jwttoken"

	"github.com/google/uuid"
)

const middlewareSecret = "middleware-test-secret-at-least-32-bytes!!"

// fakeSessionChecker is a fake middleware.SessionRevocationChecker. The zero
// value reports every session as not revoked with no error.
type fakeSessionChecker struct {
	revoked bool
	err     error
}

func (f *fakeSessionChecker) IsSessionRevoked(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	return f.revoked, f.err
}

// issueTestToken issues a real JWT using the jwttoken package so middleware
// tests exercise the full verification path rather than relying on stubs.
func issueTestToken(t *testing.T, userID, sessionID uuid.UUID, planKey string) string {
	t.Helper()
	tok, err := jwttoken.Issue(middlewareSecret, userID, sessionID, planKey)
	if err != nil {
		t.Fatalf("issueTestToken: %v", err)
	}
	return tok
}

// sentinelHandler records whether it was called and writes 200 OK.
// Used to verify that RequireAuth either blocks or passes through.
type sentinelHandler struct{ called bool }

func (s *sentinelHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.called = true
	w.WriteHeader(http.StatusOK)
}

func TestRequireAuth_WithBearerToken(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	tok := issueTestToken(t, userID, sessionID, "free")

	var capturedClaims jwttoken.Claims
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := middleware.AuthClaims(r)
		if !ok {
			t.Error("AuthClaims should return ok=true after RequireAuth passes")
		}
		capturedClaims = claims
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	middleware.RequireAuth(middlewareSecret, &fakeSessionChecker{})(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedClaims.UserID != userID {
		t.Errorf("claims.UserID: got %s, want %s", capturedClaims.UserID, userID)
	}
	if capturedClaims.SessionID != sessionID {
		t.Errorf("claims.SessionID: got %s, want %s", capturedClaims.SessionID, sessionID)
	}
	if capturedClaims.PlanKey != "free" {
		t.Errorf("claims.PlanKey: got %q, want %q", capturedClaims.PlanKey, "free")
	}
}

func TestRequireAuth_WithCookie(t *testing.T) {
	userID := uuid.New()
	tok := issueTestToken(t, userID, uuid.New(), "pro")

	sentinel := &sentinelHandler{}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: tok})
	rec := httptest.NewRecorder()
	middleware.RequireAuth(middlewareSecret, &fakeSessionChecker{})(sentinel).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with valid cookie token, got %d", rec.Code)
	}
	if !sentinel.called {
		t.Error("inner handler should have been called")
	}
}

func TestRequireAuth_BearerTakesPrecedenceOverCookie(t *testing.T) {
	// Valid Bearer + any cookie → Bearer wins; request passes through.
	tok := issueTestToken(t, uuid.New(), uuid.New(), "free")
	sentinel := &sentinelHandler{}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: "some-other-value"})
	rec := httptest.NewRecorder()
	middleware.RequireAuth(middlewareSecret, &fakeSessionChecker{})(sentinel).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 when valid Bearer is present, got %d", rec.Code)
	}
}

func TestRequireAuth_MissingToken(t *testing.T) {
	sentinel := &sentinelHandler{}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	middleware.RequireAuth(middlewareSecret, &fakeSessionChecker{})(sentinel).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no token is provided, got %d", rec.Code)
	}
	if sentinel.called {
		t.Error("inner handler must not be called when token is absent")
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	sentinel := &sentinelHandler{}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer this.is.not.a.valid.jwt")
	rec := httptest.NewRecorder()
	middleware.RequireAuth(middlewareSecret, &fakeSessionChecker{})(sentinel).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid JWT, got %d", rec.Code)
	}
	if sentinel.called {
		t.Error("inner handler must not be called for an invalid token")
	}
}

func TestRequireAuth_WrongSecret(t *testing.T) {
	tok := issueTestToken(t, uuid.New(), uuid.New(), "free")
	sentinel := &sentinelHandler{}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	// Verify with a different secret than the one used to sign.
	middleware.RequireAuth("different-secret-that-is-at-least-32-bytes!", &fakeSessionChecker{})(sentinel).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when token signed with wrong key, got %d", rec.Code)
	}
	if sentinel.called {
		t.Error("inner handler must not be called when signature verification fails")
	}
}

func TestRequireAuth_RevokedSession(t *testing.T) {
	tok := issueTestToken(t, uuid.New(), uuid.New(), "free")
	sentinel := &sentinelHandler{}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	middleware.RequireAuth(middlewareSecret, &fakeSessionChecker{revoked: true})(sentinel).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for a revoked session, got %d", rec.Code)
	}
	if sentinel.called {
		t.Error("inner handler must not be called for a revoked session")
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	var body errBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	if body.Error.Code != "SESSION_REVOKED" {
		t.Errorf("expected SESSION_REVOKED code, got %q", body.Error.Code)
	}
}

func TestRequireAuth_SessionCheckError(t *testing.T) {
	tok := issueTestToken(t, uuid.New(), uuid.New(), "free")
	sentinel := &sentinelHandler{}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	middleware.RequireAuth(middlewareSecret, &fakeSessionChecker{err: errors.New("redis unreachable")})(sentinel).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when the revocation check fails, got %d", rec.Code)
	}
	if sentinel.called {
		t.Error("inner handler must not be called when the revocation check fails")
	}
}

func TestAuthClaims_ReturnsFalseOutsideProtectedRoute(t *testing.T) {
	// AuthClaims called on a plain request (no middleware) returns ok=false.
	req := httptest.NewRequest(http.MethodGet, "/open", nil)
	_, ok := middleware.AuthClaims(req)
	if ok {
		t.Error("AuthClaims should return ok=false when RequireAuth was not in the chain")
	}
}

func TestRequireAuth_EmptyBearerHeader(t *testing.T) {
	// "Bearer " with no token value after the prefix — should be rejected.
	sentinel := &sentinelHandler{}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	middleware.RequireAuth(middlewareSecret, &fakeSessionChecker{})(sentinel).ServeHTTP(rec, req)

	// An empty string after stripping "Bearer " is still an empty token string
	// so the middleware treats it as missing → 401.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty Bearer value, got %d", rec.Code)
	}
}
