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
	user := models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada")}
	sub := models.Subscription{PlanID: "free", Status: "active"}

	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		getMe: func(_ context.Context, uid uuid.UUID) (models.User, models.Subscription, bool, error) {
			if uid != userID {
				t.Errorf("GetMe called with userID %s, want %s", uid, userID)
			}
			return user, sub, true, nil
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
		Name          *string  `json:"name,omitempty"`
		HasPassword   bool     `json:"has_password"`
		Plan          planBody `json:"plan"`
	}
	type envelope struct {
		Data meBody `json:"data"`
	}

	body := decodeBody[envelope](t, rec)
	if body.Data.Email != user.Email {
		t.Errorf("email: got %q, want %q", body.Data.Email, user.Email)
	}
	if body.Data.Name == nil || user.Name == nil || *body.Data.Name != *user.Name {
		t.Errorf("name: got %v, want %v", body.Data.Name, user.Name)
	}
	if !body.Data.HasPassword {
		t.Error("has_password should be true (fake reports the account has a password)")
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
	user := models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada"), EmailVerifiedAt: &verifiedAt}

	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		getMe: func(_ context.Context, _ uuid.UUID) (models.User, models.Subscription, bool, error) {
			return user, models.Subscription{PlanID: "free", Status: "active"}, true, nil
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
		getMe: func(_ context.Context, _ uuid.UUID) (models.User, models.Subscription, bool, error) {
			t.Fatal("service should not be called without a valid token")
			return models.User{}, models.Subscription{}, false, nil
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

// --- UpdateProfile ---

func TestUpdateProfileHandler_Success(t *testing.T) {
	userID := uuid.New()
	var capturedInput models.UpdateProfileInput
	updated := models.User{
		ID:          userID,
		Email:       "ada@example.com",
		Name:        strPtr("Jane"),
		CompanyName: strPtr("Jane's Templates"),
	}

	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		updateProfile: func(_ context.Context, uid uuid.UUID, input models.UpdateProfileInput) (models.User, error) {
			if uid != userID {
				t.Errorf("UpdateProfile called with userID %s, want %s", uid, userID)
			}
			capturedInput = input
			return updated, nil
		},
		getMe: func(_ context.Context, _ uuid.UUID) (models.User, models.Subscription, bool, error) {
			return updated, models.Subscription{PlanID: "free", Status: "active"}, false, nil
		},
	}})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/me/profile",
		bodyJSON(t, map[string]any{"name": "Jane", "company_name": "Jane's Templates"}))
	req.Header.Set("Authorization", authHeader(t, userID, uuid.New()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.UpdateProfile)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedInput.Name == nil || *capturedInput.Name != "Jane" {
		t.Errorf("Name: got %v, want %q", capturedInput.Name, "Jane")
	}
	if capturedInput.CompanyName == nil || *capturedInput.CompanyName != "Jane's Templates" {
		t.Errorf("CompanyName: got %v, want %q", capturedInput.CompanyName, "Jane's Templates")
	}
	if capturedInput.AvatarURL != nil {
		t.Errorf("AvatarURL should be nil (omitted from request), got %v", capturedInput.AvatarURL)
	}

	type meBody struct {
		Data struct {
			Name        *string `json:"name,omitempty"`
			CompanyName *string `json:"company_name,omitempty"`
		} `json:"data"`
	}
	body := decodeBody[meBody](t, rec)
	if body.Data.Name == nil || *body.Data.Name != "Jane" {
		t.Errorf("response name: got %v, want %q", body.Data.Name, "Jane")
	}
}

func TestUpdateProfileHandler_MalformedBody(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		updateProfile: func(_ context.Context, _ uuid.UUID, _ models.UpdateProfileInput) (models.User, error) {
			t.Fatal("service should not be called when body is malformed")
			return models.User{}, nil
		},
	}})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/me/profile", bytes.NewBufferString("{bad json"))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.UpdateProfile)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestUpdateProfileHandler_InvalidInput(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		updateProfile: func(_ context.Context, _ uuid.UUID, _ models.UpdateProfileInput) (models.User, error) {
			return models.User{}, msgs.ErrInvalidInput
		},
	}})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/me/profile",
		bodyJSON(t, map[string]any{"social_links": map[string]any{"platforms": map[string]string{"diskord": "https://discord.gg/xyz"}}}))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.UpdateProfile)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid input, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT code, got %q", body.Error.Code)
	}
}

// --- SetPassword ---

func TestSetPasswordHandler_Success(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	var capturedUserID, capturedSessionID uuid.UUID
	var capturedNew string

	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		setPassword: func(_ context.Context, uid, sid uuid.UUID, nw string) error {
			capturedUserID = uid
			capturedSessionID = sid
			capturedNew = nw
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/set",
		bodyJSON(t, map[string]string{"new_password": "Correct-Horse1!"}))
	req.Header.Set("Authorization", authHeader(t, userID, sessionID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.SetPassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedUserID != userID {
		t.Errorf("userID: got %s, want %s", capturedUserID, userID)
	}
	if capturedSessionID != sessionID {
		t.Errorf("sessionID: got %s, want %s", capturedSessionID, sessionID)
	}
	if capturedNew != "Correct-Horse1!" {
		t.Errorf("newPassword: got %q, want %q", capturedNew, "Correct-Horse1!")
	}
}

func TestSetPasswordHandler_MalformedBody(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		setPassword: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			t.Fatal("service should not be called when body is malformed")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/set", bytes.NewBufferString("{bad json"))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.SetPassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestSetPasswordHandler_AlreadySet(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		setPassword: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			return msgs.ErrPasswordAlreadySet
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/set",
		bodyJSON(t, map[string]string{"new_password": "Correct-Horse1!"}))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.SetPassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for already-set password, got %d", rec.Code)
	}
	type errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	body := decodeBody[errBody](t, rec)
	if body.Error.Code != "PASSWORD_ALREADY_SET" {
		t.Errorf("expected PASSWORD_ALREADY_SET code, got %q", body.Error.Code)
	}
}

func TestSetPasswordHandler_MissingToken(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		setPassword: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			t.Fatal("service should not be called without a valid token")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/password/set", nil)
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.SetPassword)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when token is missing, got %d", rec.Code)
	}
}

func TestUpdateProfileHandler_MissingToken(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		updateProfile: func(_ context.Context, _ uuid.UUID, _ models.UpdateProfileInput) (models.User, error) {
			t.Fatal("service should not be called without a valid token")
			return models.User{}, nil
		},
	}})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/me/profile", nil)
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.UpdateProfile)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when token is missing, got %d", rec.Code)
	}
}

// --- DeleteAccount ---

func TestDeleteAccountHandler_Success(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	var capturedUserID, capturedSessionID uuid.UUID
	var capturedPassword string

	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		deleteAccount: func(_ context.Context, uid, sid uuid.UUID, pw string) error {
			capturedUserID = uid
			capturedSessionID = sid
			capturedPassword = pw
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/me",
		bodyJSON(t, map[string]string{"current_password": "Correct-Battery1!"}))
	req.Header.Set("Authorization", authHeader(t, userID, sessionID))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.DeleteAccount)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", rec.Code, rec.Body)
	}
	if capturedUserID != userID {
		t.Errorf("userID: got %s, want %s", capturedUserID, userID)
	}
	if capturedSessionID != sessionID {
		t.Errorf("sessionID: got %s, want %s", capturedSessionID, sessionID)
	}
	if capturedPassword != "Correct-Battery1!" {
		t.Errorf("currentPassword: got %q, want %q", capturedPassword, "Correct-Battery1!")
	}
	if cookies := cookieMap(rec); cookies["access_token"] != "" || cookies["refresh_token"] != "" {
		t.Error("expected auth cookies to be cleared")
	}
}

func TestDeleteAccountHandler_MalformedBody(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		deleteAccount: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			t.Fatal("service should not be called when body is malformed")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/me", bytes.NewBufferString("{bad json"))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.DeleteAccount)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed body, got %d", rec.Code)
	}
}

func TestDeleteAccountHandler_WrongPassword(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		deleteAccount: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			return msgs.ErrInvalidCredentials
		},
	}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/me",
		bodyJSON(t, map[string]string{"current_password": "wrong"}))
	req.Header.Set("Authorization", authHeader(t, uuid.New(), uuid.New()))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.DeleteAccount)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong password, got %d", rec.Code)
	}
}

func TestDeleteAccountHandler_MissingToken(t *testing.T) {
	h := handlers.NewUserHandler(&fakeSvc{user: &fakeUserSvc{
		deleteAccount: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			t.Fatal("service should not be called without a valid token")
			return nil
		},
	}})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	handlerWithAuth(http.HandlerFunc(h.DeleteAccount)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when token is missing, got %d", rec.Code)
	}
}
