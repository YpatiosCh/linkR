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
	register       func(ctx context.Context, input models.RegisterInput) (models.User, service.TokenPair, error)
	login          func(ctx context.Context, input models.LoginInput) (models.User, service.TokenPair, error)
	refresh        func(ctx context.Context, rawRefreshToken string) (models.User, service.TokenPair, error)
	logout         func(ctx context.Context, sessionID uuid.UUID) error
	logoutAll      func(ctx context.Context, userID uuid.UUID) error
	getMe          func(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, error)
	changePassword func(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error
}

func (f *fakeAuthSvc) Register(ctx context.Context, input models.RegisterInput) (models.User, service.TokenPair, error) {
	return f.register(ctx, input)
}
func (f *fakeAuthSvc) Login(ctx context.Context, input models.LoginInput) (models.User, service.TokenPair, error) {
	return f.login(ctx, input)
}
func (f *fakeAuthSvc) Refresh(ctx context.Context, rawRefreshToken string) (models.User, service.TokenPair, error) {
	return f.refresh(ctx, rawRefreshToken)
}
func (f *fakeAuthSvc) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return f.logout(ctx, sessionID)
}
func (f *fakeAuthSvc) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	return f.logoutAll(ctx, userID)
}
func (f *fakeAuthSvc) GetMe(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, error) {
	return f.getMe(ctx, userID)
}
func (f *fakeAuthSvc) ChangePassword(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error {
	return f.changePassword(ctx, userID, sessionID, currentPassword, newPassword)
}

type fakeSvc struct{ auth *fakeAuthSvc }

func (f *fakeSvc) Auth() service.AuthService { return f.auth }

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
func stubTokenPair() service.TokenPair {
	return service.TokenPair{
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
		register: func(_ context.Context, input models.RegisterInput) (models.User, service.TokenPair, error) {
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

	type userBody struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	body := decodeBody[userBody](t, rec)
	if body.Email != user.Email {
		t.Errorf("email: got %q, want %q", body.Email, user.Email)
	}
	if body.Name != user.Name {
		t.Errorf("name: got %q, want %q", body.Name, user.Name)
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
		register: func(_ context.Context, _ models.RegisterInput) (models.User, service.TokenPair, error) {
			t.Fatal("service should not be called when body is malformed")
			return models.User{}, service.TokenPair{}, nil
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
		Error struct{ Code string `json:"code"` } `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "INVALID_BODY" {
		t.Errorf("expected INVALID_BODY code, got %q", body.Error.Code)
	}
}

func TestRegisterHandler_EmailAlreadyExists(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		register: func(_ context.Context, _ models.RegisterInput) (models.User, service.TokenPair, error) {
			return models.User{}, service.TokenPair{}, msgs.ErrEmailAlreadyExists
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.Register), "/api/v1/auth/register", map[string]string{
		"email": "taken@example.com", "password": "correct-horse-battery", "name": "Ada",
	})

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate email, got %d", rec.Code)
	}
	type errBody struct {
		Error struct{ Code string `json:"code"` } `json:"error"`
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
		login: func(_ context.Context, input models.LoginInput) (models.User, service.TokenPair, error) {
			return user, pair, nil
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.Login), "/api/v1/auth/login", map[string]string{
		"email": "ada@example.com", "password": "correct-horse-battery",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}

	type userBody struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	body := decodeBody[userBody](t, rec)
	if body.Email != user.Email {
		t.Errorf("email: got %q, want %q", body.Email, user.Email)
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
		login: func(_ context.Context, _ models.LoginInput) (models.User, service.TokenPair, error) {
			t.Fatal("service should not be called when body is malformed")
			return models.User{}, service.TokenPair{}, nil
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
		login: func(_ context.Context, _ models.LoginInput) (models.User, service.TokenPair, error) {
			return models.User{}, service.TokenPair{}, msgs.ErrInvalidCredentials
		},
	}})

	rec := postJSON(t, http.HandlerFunc(h.Login), "/api/v1/auth/login", map[string]string{
		"email": "ada@example.com", "password": "wrong",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid credentials, got %d", rec.Code)
	}
	type errBody struct {
		Error struct{ Code string `json:"code"` } `json:"error"`
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
		refresh: func(_ context.Context, rawRefreshToken string) (models.User, service.TokenPair, error) {
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
		refresh: func(_ context.Context, _ string) (models.User, service.TokenPair, error) {
			t.Fatal("service should not be called when cookie is missing")
			return models.User{}, service.TokenPair{}, nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	rec := httptest.NewRecorder()
	h.Refresh(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when refresh_token cookie is absent, got %d", rec.Code)
	}
	type errBody struct {
		Error struct{ Code string `json:"code"` } `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "TOKEN_INVALID" {
		t.Errorf("expected TOKEN_INVALID code, got %q", body.Error.Code)
	}
}

func TestRefreshHandler_ReuseDetected(t *testing.T) {
	h := handlers.NewAuthHandler(&fakeSvc{auth: &fakeAuthSvc{
		refresh: func(_ context.Context, _ string) (models.User, service.TokenPair, error) {
			return models.User{}, service.TokenPair{}, msgs.ErrTokenReuseDetected
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
		Error struct{ Code string `json:"code"` } `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "TOKEN_REUSE_DETECTED" {
		t.Errorf("expected TOKEN_REUSE_DETECTED code, got %q", body.Error.Code)
	}
}

// handlerWithAuth wraps h in the RequireAuth middleware using the handler test
// secret, so authenticated handler tests exercise the full middleware + handler chain.
const handlerTestSecret = "handler-test-secret-at-least-32-bytes!!"

func handlerWithAuth(h http.Handler) http.Handler {
	return middleware.RequireAuth(handlerTestSecret)(h)
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

// --- GetMe ---

func TestGetMeHandler_Success(t *testing.T) {
	userID := uuid.New()
	user := models.User{ID: userID, Email: "ada@example.com", Name: "Ada"}
	sub := models.Subscription{PlanID: "free", Status: "active"}

	h := handlers.NewMeHandler(&fakeSvc{auth: &fakeAuthSvc{
		getMe: func(_ context.Context, uid uuid.UUID) (models.User, models.Subscription, error) {
			if uid != userID {
				t.Errorf("GetMe called with userID %s, want %s", uid, userID)
			}
			return user, sub, nil
		},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", authHeader(t, userID, uuid.New()))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.GetMe)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}

	type planBody struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	type meBody struct {
		ID            string   `json:"id"`
		Email         string   `json:"email"`
		EmailVerified bool     `json:"email_verified"`
		Name          string   `json:"name"`
		Plan          planBody `json:"plan"`
	}
	type envelope struct {
		Data meBody `json:"data"`
	}

	body := decodeBody[envelope](t, rec)
	if body.Data.Email != user.Email {
		t.Errorf("email: got %q, want %q", body.Data.Email, user.Email)
	}
	if body.Data.Name != user.Name {
		t.Errorf("name: got %q, want %q", body.Data.Name, user.Name)
	}
	if body.Data.EmailVerified {
		t.Error("email_verified should be false when EmailVerifiedAt is nil")
	}
	if body.Data.Plan.ID != "free" {
		t.Errorf("plan.id: got %q, want %q", body.Data.Plan.ID, "free")
	}
	if body.Data.Plan.Status != "active" {
		t.Errorf("plan.status: got %q, want %q", body.Data.Plan.Status, "active")
	}
}

func TestGetMeHandler_WithVerifiedEmail(t *testing.T) {
	userID := uuid.New()
	verifiedAt := time.Now().Add(-24 * time.Hour)
	user := models.User{ID: userID, Email: "ada@example.com", Name: "Ada", EmailVerifiedAt: &verifiedAt}

	h := handlers.NewMeHandler(&fakeSvc{auth: &fakeAuthSvc{
		getMe: func(_ context.Context, _ uuid.UUID) (models.User, models.Subscription, error) {
			return user, models.Subscription{PlanID: "free", Status: "active"}, nil
		},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", authHeader(t, userID, uuid.New()))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.GetMe)).ServeHTTP(rec, req)

	type meBody struct {
		Data struct {
			EmailVerified bool `json:"email_verified"`
		} `json:"data"`
	}
	body := decodeBody[meBody](t, rec)
	if !body.Data.EmailVerified {
		t.Error("email_verified should be true when EmailVerifiedAt is set")
	}
}

func TestGetMeHandler_MissingToken(t *testing.T) {
	h := handlers.NewMeHandler(&fakeSvc{auth: &fakeAuthSvc{
		getMe: func(_ context.Context, _ uuid.UUID) (models.User, models.Subscription, error) {
			t.Fatal("service should not be called without a valid token")
			return models.User{}, models.Subscription{}, nil
		},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.GetMe)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when token is missing, got %d", rec.Code)
	}
}

// --- ChangePassword ---

func TestChangePasswordHandler_Success(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	var capturedUserID, capturedSessionID uuid.UUID
	var capturedCurrent, capturedNew string

	h := handlers.NewMeHandler(&fakeSvc{auth: &fakeAuthSvc{
		changePassword: func(_ context.Context, uid, sid uuid.UUID, cur, nw string) error {
			capturedUserID = uid
			capturedSessionID = sid
			capturedCurrent = cur
			capturedNew = nw
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/change",
		bodyJSON(t, map[string]string{"current_password": "old-password", "new_password": "new-password"}))
	req.Header.Set("Authorization", authHeader(t, userID, sessionID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.ChangePassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedUserID != userID {
		t.Errorf("userID: got %s, want %s", capturedUserID, userID)
	}
	if capturedSessionID != sessionID {
		t.Errorf("sessionID: got %s, want %s", capturedSessionID, sessionID)
	}
	if capturedCurrent != "old-password" {
		t.Errorf("currentPassword: got %q, want %q", capturedCurrent, "old-password")
	}
	if capturedNew != "new-password" {
		t.Errorf("newPassword: got %q, want %q", capturedNew, "new-password")
	}
}

func TestChangePasswordHandler_MalformedBody(t *testing.T) {
	h := handlers.NewMeHandler(&fakeSvc{auth: &fakeAuthSvc{
		changePassword: func(_ context.Context, _, _ uuid.UUID, _, _ string) error {
			t.Fatal("service should not be called when body is malformed")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/change", bytes.NewBufferString("{bad json"))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.ChangePassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestChangePasswordHandler_WrongCurrentPassword(t *testing.T) {
	h := handlers.NewMeHandler(&fakeSvc{auth: &fakeAuthSvc{
		changePassword: func(_ context.Context, _, _ uuid.UUID, _, _ string) error {
			return msgs.ErrInvalidCredentials
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/change",
		bodyJSON(t, map[string]string{"current_password": "wrong", "new_password": "new-password"}))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.ChangePassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong current password, got %d", rec.Code)
	}
}

func TestChangePasswordHandler_OAuthOnlyAccount(t *testing.T) {
	h := handlers.NewMeHandler(&fakeSvc{auth: &fakeAuthSvc{
		changePassword: func(_ context.Context, _, _ uuid.UUID, _, _ string) error {
			return msgs.ErrPasswordNotSet
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/change",
		bodyJSON(t, map[string]string{"current_password": "any", "new_password": "new-password"}))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.ChangePassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for OAuth-only account, got %d", rec.Code)
	}
	type errBody struct {
		Error struct{ Code string `json:"code"` } `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "PASSWORD_NOT_SET" {
		t.Errorf("expected PASSWORD_NOT_SET code, got %q", body.Error.Code)
	}
}

func TestChangePasswordHandler_MissingToken(t *testing.T) {
	h := handlers.NewMeHandler(&fakeSvc{auth: &fakeAuthSvc{
		changePassword: func(_ context.Context, _, _ uuid.UUID, _, _ string) error {
			t.Fatal("service should not be called without a valid token")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/change", nil)
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.ChangePassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when token is missing, got %d", rec.Code)
	}
}
