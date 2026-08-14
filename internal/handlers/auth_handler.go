package handlers

import (
	"encoding/json"
	"linkMe/internal/middleware"
	"linkMe/internal/models"
	"linkMe/internal/service"
	"linkMe/internal/utils/cookies"
	"linkMe/internal/utils/jwttoken"
	"linkMe/internal/utils/response"
	"net/http"
	"time"
)

// authHandler implements AuthHandler by delegating to the embedded
// service layer for all authentication operations.
type authHandler struct {
	service.Service
}

// NewAuthHandler constructs an AuthHandler backed by the given service.
func NewAuthHandler(service service.Service) AuthHandler {
	return &authHandler{service}
}

// Register handles POST /api/v1/auth/register: it decodes the JSON request body
// into a registerRequest, calls the auth service's Register with the decoded
// fields, sets both token cookies, and writes the created user as a 201 response.
// On a decode failure it responds 400 with CodeInvalidBody; on a service error it
// maps the error to the appropriate status via response.HandleError.
func (h *authHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidBody, "request body is malformed")
		return
	}

	user, pair, err := h.Auth().Register(r.Context(), models.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
	})
	if err != nil {
		response.HandleError(w, err)
		return
	}

	cookies.SetTokenCookies(w, pair)
	response.JSON(w, http.StatusCreated, models.AuthResponse{
		ID:        user.ID.String(),
		Email:     user.Email,
		Name:      user.Name,
		ExpiresAt: time.Now().Add(jwttoken.AccessTokenDuration),
	})
}

// Login handles POST /api/v1/auth/login: it decodes the JSON request body into a
// loginRequest, calls the auth service's Login with the decoded credentials, sets
// both token cookies, and writes the authenticated user as a 200 response.
// On a decode failure it responds 400 with CodeInvalidBody; on a service error it
// maps the error to the appropriate status via response.HandleError (invalid
// credentials become 401 INVALID_CREDENTIALS).
func (h *authHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidBody, "request body is malformed")
		return
	}

	user, pair, err := h.Auth().Login(r.Context(), models.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		response.HandleError(w, err)
		return
	}

	cookies.SetTokenCookies(w, pair)
	response.JSON(w, http.StatusOK, models.AuthResponse{
		ID:        user.ID.String(),
		Email:     user.Email,
		Name:      user.Name,
		ExpiresAt: time.Now().Add(jwttoken.AccessTokenDuration),
	})
}

// Refresh handles POST /api/v1/auth/refresh: it reads the refresh_token cookie,
// calls the auth service to validate and rotate the token pair, sets new token
// cookies, and writes the new access token's expiry time.
// A missing cookie responds 401 TOKEN_INVALID; a reused consumed token responds
// 401 TOKEN_REUSE_DETECTED. All other service errors flow through
// response.HandleError.
func (h *authHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refresh_token")
	if err != nil {
		response.Error(w, http.StatusUnauthorized, response.CodeTokenInvalid, "refresh token missing")
		return
	}

	_, pair, err := h.Auth().Refresh(r.Context(), cookie.Value)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	cookies.SetTokenCookies(w, pair)
	response.JSON(w, http.StatusOK, models.RefreshResponse{
		ExpiresAt: time.Now().Add(jwttoken.AccessTokenDuration),
	})
}

// Logout handles POST /api/v1/auth/logout: it reads the session ID from the
// verified JWT claims injected by RequireAuth, revokes that session, and clears
// both authentication cookies. Responds 204 No Content on success. Requires a
// valid access token (enforced by RequireAuth in the route registration).
func (h *authHandler) Logout(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.AuthClaims(r)
	if err := h.Auth().Logout(r.Context(), claims.SessionID); err != nil {
		response.HandleError(w, err)
		return
	}
	cookies.ClearTokenCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

// LogoutAll handles POST /api/v1/auth/logout-all: it reads the user ID from the
// verified JWT claims injected by RequireAuth, revokes every active session for
// that user, and clears the current authentication cookies. Responds 204 No
// Content on success. Requires a valid access token.
func (h *authHandler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.AuthClaims(r)
	if err := h.Auth().LogoutAll(r.Context(), claims.UserID); err != nil {
		response.HandleError(w, err)
		return
	}
	cookies.ClearTokenCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

// requestEmailVerificationMessage is the fixed generic message returned by
// RequestEmailVerification regardless of whether the email exists or is
// already verified (enumeration defense).
const requestEmailVerificationMessage = "If an account with that email exists and is unverified, a verification email has been sent."

// RequestEmailVerification handles POST /api/v1/auth/email/verification/request:
// it decodes the request body, calls the auth service, and always writes the
// same generic 200 message regardless of outcome. On a decode failure it
// responds 400 with CodeInvalidBody; unexpected service errors flow through
// response.HandleError.
func (h *authHandler) RequestEmailVerification(w http.ResponseWriter, r *http.Request) {
	var req models.RequestEmailVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidBody, "request body is malformed")
		return
	}

	if err := h.Auth().RequestEmailVerification(r.Context(), req.Email); err != nil {
		response.HandleError(w, err)
		return
	}

	response.JSON(w, http.StatusOK, struct {
		Data models.MessageResponse `json:"data"`
	}{
		Data: models.MessageResponse{Message: requestEmailVerificationMessage},
	})
}

// VerifyEmail handles POST /api/v1/auth/email/verification/verify: it
// decodes the request body, calls the auth service to consume the token and
// mark the account's email verified, and writes the account's public
// representation as a 200 response wrapped under a "data" key, identical to
// GET /api/v1/me. On a decode failure it responds 400 with CodeInvalidBody;
// an invalid/expired token responds 401 TOKEN_INVALID, an already-used token
// responds 401 TOKEN_ALREADY_USED.
func (h *authHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req models.VerifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidBody, "request body is malformed")
		return
	}

	user, sub, err := h.Auth().VerifyEmail(r.Context(), req.Token)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.JSON(w, http.StatusOK, struct {
		Data models.MeResponse `json:"data"`
	}{
		Data: models.MeResponse{
			ID:            user.ID.String(),
			Email:         user.Email,
			EmailVerified: user.EmailVerifiedAt != nil,
			Name:          user.Name,
			AvatarURL:     user.AvatarURL,
			Plan: models.MePlanResponse{
				ID:     sub.PlanID,
				Status: sub.Status,
			},
		},
	})
}

// requestPasswordResetMessage is the fixed generic message returned by
// RequestPasswordReset regardless of whether the email exists (enumeration
// defense), matching the auth spec's example response verbatim.
const requestPasswordResetMessage = "If an account exists, a password reset email has been sent."

// RequestPasswordReset handles POST /api/v1/auth/password/reset/request: it
// decodes the request body, calls the auth service, and always writes the
// same generic 200 message regardless of outcome. On a decode failure it
// responds 400 with CodeInvalidBody; unexpected service errors flow through
// response.HandleError.
func (h *authHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req models.RequestPasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidBody, "request body is malformed")
		return
	}

	if err := h.Auth().RequestPasswordReset(r.Context(), req.Email); err != nil {
		response.HandleError(w, err)
		return
	}

	response.JSON(w, http.StatusOK, struct {
		Data models.MessageResponse `json:"data"`
	}{
		Data: models.MessageResponse{Message: requestPasswordResetMessage},
	})
}

// ResetPassword handles POST /api/v1/auth/password/reset/confirm: it decodes
// the request body, calls the auth service to consume the token, replace the
// account's password, and revoke every active session, and responds 204 No
// Content on success. On a decode failure it responds 400 with
// CodeInvalidBody; an invalid/expired token responds 401 TOKEN_INVALID, an
// already-used token responds 401 TOKEN_ALREADY_USED, a weak new password
// responds 401 INVALID_CREDENTIALS, and an OAuth-only account responds 400
// PASSWORD_NOT_SET.
func (h *authHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req models.ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidBody, "request body is malformed")
		return
	}

	if err := h.Auth().ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		response.HandleError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
