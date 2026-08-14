package handlers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"linkMe/internal/handlers"
	"linkMe/internal/models"
	"linkMe/internal/msgs"

	"github.com/google/uuid"
)

// --- GetMe ---

func TestGetMeHandler_Success(t *testing.T) {
	userID := uuid.New()
	user := models.User{ID: userID, Email: "ada@example.com", Name: "Ada"}
	sub := models.Subscription{PlanID: "free", Status: "active"}

	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
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

	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
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
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
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

	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
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
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
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
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
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
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
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
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "PASSWORD_NOT_SET" {
		t.Errorf("expected PASSWORD_NOT_SET code, got %q", body.Error.Code)
	}
}

func TestChangePasswordHandler_MissingToken(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
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
