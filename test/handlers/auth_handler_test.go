package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"linkMe/internal/handlers"
	"linkMe/internal/middleware"
	"linkMe/internal/models"
	"linkMe/internal/msgs"
	"linkMe/internal/service"
	"linkMe/internal/utils/jwttoken"

	"github.com/google/uuid"
)

// --- fake service layer ---

type fakeAuthSvc struct {
	register                 func(ctx context.Context, input models.RegisterInput) (models.User, models.TokenPair, error)
	login                    func(ctx context.Context, input models.LoginInput) (models.User, models.TokenPair, error)
	refresh                  func(ctx context.Context, rawRefreshToken string) (models.User, models.TokenPair, error)
	logout                   func(ctx context.Context, sessionID uuid.UUID) error
	logoutAll                func(ctx context.Context, userID uuid.UUID) error
	requestEmailVerification func(ctx context.Context, email string) error
	verifyEmail              func(ctx context.Context, token string) (models.User, models.Subscription, error)
	requestPasswordReset     func(ctx context.Context, email string) error
	resetPassword            func(ctx context.Context, token string, newPassword string) error
}

func (f *fakeAuthSvc) Register(ctx context.Context, input models.RegisterInput) (models.User, models.TokenPair, error) {
	return f.register(ctx, input)
}
func (f *fakeAuthSvc) Login(ctx context.Context, input models.LoginInput) (models.User, models.TokenPair, error) {
	return f.login(ctx, input)
}
func (f *fakeAuthSvc) Refresh(ctx context.Context, rawRefreshToken string) (models.User, models.TokenPair, error) {
	return f.refresh(ctx, rawRefreshToken)
}
func (f *fakeAuthSvc) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return f.logout(ctx, sessionID)
}
func (f *fakeAuthSvc) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	return f.logoutAll(ctx, userID)
}
func (f *fakeAuthSvc) RequestEmailVerification(ctx context.Context, email string) error {
	return f.requestEmailVerification(ctx, email)
}
func (f *fakeAuthSvc) VerifyEmail(ctx context.Context, token string) (models.User, models.Subscription, error) {
	return f.verifyEmail(ctx, token)
}
func (f *fakeAuthSvc) RequestPasswordReset(ctx context.Context, email string) error {
	return f.requestPasswordReset(ctx, email)
}
func (f *fakeAuthSvc) ResetPassword(ctx context.Context, token string, newPassword string) error {
	return f.resetPassword(ctx, token, newPassword)
}

// fakeUserSvc is the fake service.UserService used by user-handler tests
// (see user_handler_test.go).
type fakeUserSvc struct {
	getMe          func(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, error)
	changePassword func(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error
}

func (f *fakeUserSvc) GetMe(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, error) {
	return f.getMe(ctx, userID)
}
func (f *fakeUserSvc) ChangePassword(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error {
	return f.changePassword(ctx, userID, sessionID, currentPassword, newPassword)
}

type fakeSvc struct {
	auth *fakeAuthSvc
	user *fakeUserSvc
}

func (f *fakeSvc) Auth() service.AuthService   { return f.auth }
func (f *fakeSvc) User() service.UserService   { return f.user }
func (f *fakeSvc) Email() service.EmailService { return nil }

// authHeader returns an Authorization: Bearer header with a real JWT signed
// with the handler test secret so tests that need RequireAuth can pass through.
func authHeader(t *testing.T, userID, sessionID uuid.UUID) string {
	t.Helper()
	tok, err := jwttoken.Issue(handlerTestSecret, userID, sessionID, "free")
	if err != nil {
		t.Fatalf("issuing test JWT: %v", err)
	}
	return "Bearer " + tok
}

// stubTokenPair returns a minimal valid-looking token pair for handler-level tests
// that don't care about JWT contents.
func stubTokenPair() models.TokenPair {
	return models.TokenPair{
		AccessToken:     "eyJ.stub.access",
		RawRefreshToken: "stub-refresh-token",
	}
}

// stubUser returns a minimal user for handler-level tests.
func stubUser() models.User {
	return models.User{ID: uuid.New(), Email: "ada@example.com", Name: "Ada"}
}

// --- helpers ---

// bodyJSON encodes v as JSON and returns it as an io.ReadCloser suitable for
// req.Body. Unlike postJSON it does not set Content-Type — callers do that if
// needed — so it can be used with an already-constructed *http.Request.
func bodyJSON(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("bodyJSON marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

func postJSON(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return v
}

func cookieMap(rec *httptest.ResponseRecorder) map[string]string {
	m := make(map[string]string)
	for _, c := range rec.Result().Cookies() {
		m[c.Name] = c.Value
	}
	return m
}

// --- Register ---

func TestRegisterHandler_Success(t *testing.T) {
	user := stubUser()
	pair := stubTokenPair()

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		register: func(_ context.Context, input models.RegisterInput) (models.User, models.TokenPair, error) {
			if input.Email != "ada@example.com" {
				t.Errorf("unexpected email in service call: %q", input.Email)
			}
			return user, pair, nil
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.Register), "/api/v1/auth/register", map[string]string{
		"email": "ada@example.com", "password": "correct-horse-battery", "name": "Ada",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body)
	}

	type authBody struct {
		ID        string    `json:"id"`
		Email     string    `json:"email"`
		Name      string    `json:"name"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	body := decodeBody[authBody](t, rec)
	if body.Email != user.Email {
		t.Errorf("email: got %q, want %q", body.Email, user.Email)
	}
	if body.Name != user.Name {
		t.Errorf("name: got %q, want %q", body.Name, user.Name)
	}
	if body.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at in register response")
	}

	cookies := cookieMap(rec)
	if cookies["access_token"] != pair.AccessToken {
		t.Errorf("access_token cookie: got %q, want %q", cookies["access_token"], pair.AccessToken)
	}
	if cookies["refresh_token"] != pair.RawRefreshToken {
		t.Errorf("refresh_token cookie: got %q, want %q", cookies["refresh_token"], pair.RawRefreshToken)
	}
}

func TestRegisterHandler_MalformedBody(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		register: func(_ context.Context, _ models.RegisterInput) (models.User, models.TokenPair, error) {
			t.Fatal("service should not be called when body is malformed")
			return models.User{}, models.TokenPair{}, nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString("not json{{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Register(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "INVALID_BODY" {
		t.Errorf("expected INVALID_BODY code, got %q", body.Error.Code)
	}
}

func TestRegisterHandler_EmailAlreadyExists(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		register: func(_ context.Context, _ models.RegisterInput) (models.User, models.TokenPair, error) {
			return models.User{}, models.TokenPair{}, msgs.ErrEmailAlreadyExists
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.Register), "/api/v1/auth/register", map[string]string{
		"email": "taken@example.com", "password": "correct-horse-battery", "name": "Ada",
	})

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate email, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "EMAIL_ALREADY_EXISTS" {
		t.Errorf("expected EMAIL_ALREADY_EXISTS code, got %q", body.Error.Code)
	}
}

// --- Login ---

func TestLoginHandler_Success(t *testing.T) {
	user := stubUser()
	pair := stubTokenPair()

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		login: func(_ context.Context, input models.LoginInput) (models.User, models.TokenPair, error) {
			return user, pair, nil
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.Login), "/api/v1/auth/login", map[string]string{
		"email": "ada@example.com", "password": "correct-horse-battery",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}

	type authBody struct {
		Email     string    `json:"email"`
		Name      string    `json:"name"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	body := decodeBody[authBody](t, rec)
	if body.Email != user.Email {
		t.Errorf("email: got %q, want %q", body.Email, user.Email)
	}
	if body.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at in login response")
	}

	cookies := cookieMap(rec)
	if cookies["access_token"] != pair.AccessToken {
		t.Errorf("access_token cookie not set correctly")
	}
	if cookies["refresh_token"] != pair.RawRefreshToken {
		t.Errorf("refresh_token cookie not set correctly")
	}
}

func TestLoginHandler_MalformedBody(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		login: func(_ context.Context, _ models.LoginInput) (models.User, models.TokenPair, error) {
			t.Fatal("service should not be called when body is malformed")
			return models.User{}, models.TokenPair{}, nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestLoginHandler_InvalidCredentials(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		login: func(_ context.Context, _ models.LoginInput) (models.User, models.TokenPair, error) {
			return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.Login), "/api/v1/auth/login", map[string]string{
		"email": "ada@example.com", "password": "wrong",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid credentials, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "INVALID_CREDENTIALS" {
		t.Errorf("expected INVALID_CREDENTIALS code, got %q", body.Error.Code)
	}
}

// --- Refresh ---

func TestRefreshHandler_Success(t *testing.T) {
	user := stubUser()
	pair := stubTokenPair()

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		refresh: func(_ context.Context, rawRefreshToken string) (models.User, models.TokenPair, error) {
			if rawRefreshToken != "old-refresh-token" {
				t.Errorf("unexpected raw refresh token: %q", rawRefreshToken)
			}
			return user, pair, nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "old-refresh-token"})
	rec := httptest.NewRecorder()
	h.Refresh(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}

	type refreshBody struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	body := decodeBody[refreshBody](t, rec)
	if body.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt in refresh response")
	}

	cookies := cookieMap(rec)
	if cookies["access_token"] != pair.AccessToken {
		t.Errorf("access_token cookie not set correctly")
	}
	if cookies["refresh_token"] != pair.RawRefreshToken {
		t.Errorf("refresh_token cookie not set correctly")
	}
}

func TestRefreshHandler_MissingCookie(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		refresh: func(_ context.Context, _ string) (models.User, models.TokenPair, error) {
			t.Fatal("service should not be called when cookie is missing")
			return models.User{}, models.TokenPair{}, nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	rec := httptest.NewRecorder()
	h.Refresh(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when refresh_token cookie is absent, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "TOKEN_INVALID" {
		t.Errorf("expected TOKEN_INVALID code, got %q", body.Error.Code)
	}
}

func TestRefreshHandler_ReuseDetected(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		refresh: func(_ context.Context, _ string) (models.User, models.TokenPair, error) {
			return models.User{}, models.TokenPair{}, msgs.ErrTokenReuseDetected
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "consumed-token"})
	rec := httptest.NewRecorder()
	h.Refresh(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on token reuse, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "TOKEN_REUSE_DETECTED" {
		t.Errorf("expected TOKEN_REUSE_DETECTED code, got %q", body.Error.Code)
	}
}

// handlerWithAuth wraps h in the RequireAuth middleware using the handler test
// secret, so authenticated handler tests exercise the full middleware + handler chain.
const handlerTestSecret = "handler-test-secret-at-least-32-bytes!!"

// notRevokedChecker is a fake middleware.SessionRevocationChecker that
// always reports every session as not revoked.
type notRevokedChecker struct{}

func (notRevokedChecker) IsSessionRevoked(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	return false, nil
}

func handlerWithAuth(h http.Handler) http.Handler {
	return middleware.RequireAuth(handlerTestSecret, notRevokedChecker{})(h)
}

// --- Logout ---

func TestLogoutHandler_Success(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	var capturedSessionID uuid.UUID

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		logout: func(_ context.Context, sid uuid.UUID) error {
			capturedSessionID = sid
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", authHeader(t, userID, sessionID))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.Logout)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedSessionID != sessionID {
		t.Errorf("service.Logout called with sessionID %s, want %s", capturedSessionID, sessionID)
	}
	cookies := cookieMap(rec)
	if cookies["access_token"] != "" {
		t.Error("access_token cookie should be cleared on logout")
	}
}

func TestLogoutHandler_ServiceError(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		logout: func(_ context.Context, _ uuid.UUID) error {
			return msgs.ErrUserNotFound // any unexpected error → 500
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.Logout)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestLogoutHandler_MissingToken(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		logout: func(_ context.Context, _ uuid.UUID) error {
			t.Fatal("service should not be called without a valid token")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.Logout)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when token is missing, got %d", rec.Code)
	}
}

// --- LogoutAll ---

func TestLogoutAllHandler_Success(t *testing.T) {
	userID := uuid.New()
	var capturedUserID uuid.UUID

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		logoutAll: func(_ context.Context, uid uuid.UUID) error {
			capturedUserID = uid
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-all", nil)
	req.Header.Set("Authorization", authHeader(t, userID, uuid.New()))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.LogoutAll)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedUserID != userID {
		t.Errorf("service.LogoutAll called with userID %s, want %s", capturedUserID, userID)
	}
}

// --- RequestEmailVerification ---

func TestRequestEmailVerificationHandler_Success(t *testing.T) {
	var capturedEmail string

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		requestEmailVerification: func(_ context.Context, email string) error {
			capturedEmail = email
			return nil
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.RequestEmailVerification), "/api/v1/auth/email/verification/request", map[string]string{
		"email": "ada@example.com",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedEmail != "ada@example.com" {
		t.Errorf("service called with email %q, want %q", capturedEmail, "ada@example.com")
	}

	type envelope struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	body := decodeBody[envelope](t, rec)
	if body.Data.Message == "" {
		t.Error("expected a non-empty generic message")
	}
}

func TestRequestEmailVerificationHandler_MalformedBody(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		requestEmailVerification: func(_ context.Context, _ string) error {
			t.Fatal("service should not be called when body is malformed")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/email/verification/request", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RequestEmailVerification(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestRequestEmailVerificationHandler_UnknownEmailStillReturnsGenericMessage(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		requestEmailVerification: func(_ context.Context, _ string) error {
			return nil // service silently no-ops for unknown emails
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.RequestEmailVerification), "/api/v1/auth/email/verification/request", map[string]string{
		"email": "unknown@example.com",
	})

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 regardless of whether the email exists, got %d", rec.Code)
	}
}

// --- VerifyEmail ---

func TestVerifyEmailHandler_Success(t *testing.T) {
	user := models.User{ID: uuid.New(), Email: "ada@example.com", Name: "Ada"}
	sub := models.Subscription{PlanID: "free", Status: "active"}
	var capturedToken string

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		verifyEmail: func(_ context.Context, tok string) (models.User, models.Subscription, error) {
			capturedToken = tok
			return user, sub, nil
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.VerifyEmail), "/api/v1/auth/email/verification/verify", map[string]string{
		"token": "raw-token",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedToken != "raw-token" {
		t.Errorf("service called with token %q, want %q", capturedToken, "raw-token")
	}

	type meBody struct {
		Data struct {
			Email string `json:"email"`
			Plan  struct {
				ID string `json:"id"`
			} `json:"plan"`
		} `json:"data"`
	}
	body := decodeBody[meBody](t, rec)
	if body.Data.Email != user.Email {
		t.Errorf("email: got %q, want %q", body.Data.Email, user.Email)
	}
	if body.Data.Plan.ID != "free" {
		t.Errorf("plan.id: got %q, want %q", body.Data.Plan.ID, "free")
	}
}

func TestVerifyEmailHandler_TokenInvalid(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		verifyEmail: func(_ context.Context, _ string) (models.User, models.Subscription, error) {
			return models.User{}, models.Subscription{}, msgs.ErrTokenInvalid
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.VerifyEmail), "/api/v1/auth/email/verification/verify", map[string]string{
		"token": "bad-token",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for an invalid token, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "TOKEN_INVALID" {
		t.Errorf("expected TOKEN_INVALID code, got %q", body.Error.Code)
	}
}

func TestVerifyEmailHandler_TokenAlreadyUsed(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		verifyEmail: func(_ context.Context, _ string) (models.User, models.Subscription, error) {
			return models.User{}, models.Subscription{}, msgs.ErrTokenAlreadyUsed
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.VerifyEmail), "/api/v1/auth/email/verification/verify", map[string]string{
		"token": "consumed-token",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for an already-used token, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "TOKEN_ALREADY_USED" {
		t.Errorf("expected TOKEN_ALREADY_USED code, got %q", body.Error.Code)
	}
}

// --- RequestPasswordReset ---

func TestRequestPasswordResetHandler_Success(t *testing.T) {
	var capturedEmail string

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		requestPasswordReset: func(_ context.Context, email string) error {
			capturedEmail = email
			return nil
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.RequestPasswordReset), "/api/v1/auth/password/reset/request", map[string]string{
		"email": "ada@example.com",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedEmail != "ada@example.com" {
		t.Errorf("service called with email %q, want %q", capturedEmail, "ada@example.com")
	}

	type envelope struct {
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	body := decodeBody[envelope](t, rec)
	if body.Data.Message != "If an account exists, a password reset email has been sent." {
		t.Errorf("unexpected message: %q", body.Data.Message)
	}
}

func TestRequestPasswordResetHandler_MalformedBody(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		requestPasswordReset: func(_ context.Context, _ string) error {
			t.Fatal("service should not be called when body is malformed")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/request", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RequestPasswordReset(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
}

// --- ResetPassword ---

func TestResetPasswordHandler_Success(t *testing.T) {
	var capturedToken, capturedNewPassword string

	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		resetPassword: func(_ context.Context, tok string, newPassword string) error {
			capturedToken = tok
			capturedNewPassword = newPassword
			return nil
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.ResetPassword), "/api/v1/auth/password/reset/confirm", map[string]string{
		"token": "raw-token", "new_password": "new-correct-battery",
	})

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedToken != "raw-token" {
		t.Errorf("token: got %q, want %q", capturedToken, "raw-token")
	}
	if capturedNewPassword != "new-correct-battery" {
		t.Errorf("newPassword: got %q, want %q", capturedNewPassword, "new-correct-battery")
	}
}

func TestResetPasswordHandler_MalformedBody(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		resetPassword: func(_ context.Context, _, _ string) error {
			t.Fatal("service should not be called when body is malformed")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/confirm", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ResetPassword(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestResetPasswordHandler_TokenInvalid(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		resetPassword: func(_ context.Context, _, _ string) error {
			return msgs.ErrTokenInvalid
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.ResetPassword), "/api/v1/auth/password/reset/confirm", map[string]string{
		"token": "bad-token", "new_password": "new-correct-battery",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for an invalid token, got %d", rec.Code)
	}
}

func TestResetPasswordHandler_OAuthOnlyAccount(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		resetPassword: func(_ context.Context, _, _ string) error {
			return msgs.ErrPasswordNotSet
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.ResetPassword), "/api/v1/auth/password/reset/confirm", map[string]string{
		"token": "raw-token", "new_password": "new-correct-battery",
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an OAuth-only account, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "PASSWORD_NOT_SET" {
		t.Errorf("expected PASSWORD_NOT_SET code, got %q", body.Error.Code)
	}
}
