package handlers

import (
	"encoding/json"
	"linkMe/internal/middleware"
	"linkMe/internal/models"
	"linkMe/internal/service"
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

// clearTokenCookies expires both authentication cookies by setting MaxAge=-1,
// causing the browser to delete them immediately. Called during logout flows.
func clearTokenCookies(w http.ResponseWriter) {
	for _, name := range []string{"access_token", "refresh_token"} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
			Path:     "/",
			MaxAge:   -1,
		})
	}
}

// setTokenCookies writes both auth cookies to the response. The refresh_token
// is long-lived (30 days) and the access_token is short-lived (15 minutes).
// Both are HttpOnly + Secure + SameSite=Lax so they cannot be accessed by
// client-side JavaScript and are not sent on cross-site requests.
func setTokenCookies(w http.ResponseWriter, pair service.TokenPair) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    pair.RawRefreshToken,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    pair.AccessToken,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   int(jwttoken.AccessTokenDuration.Seconds()),
	})
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

	setTokenCookies(w, pair)
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

	setTokenCookies(w, pair)
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

	setTokenCookies(w, pair)
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
	clearTokenCookies(w)
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
	clearTokenCookies(w)
	w.WriteHeader(http.StatusNoContent)
}
