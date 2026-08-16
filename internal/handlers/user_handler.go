package handlers

import (
	"linkMe/internal/middleware"
	"linkMe/internal/models"
	"linkMe/internal/service"
	"linkMe/internal/utils/cookies"
	"linkMe/internal/utils/response"
	"net/http"
)

// userHandler implements UserHandler by delegating to the embedded service layer.
type userHandler struct {
	service.Service
}

// NewUserHandler constructs a UserHandler backed by the given service.
func NewUserHandler(svc service.Service) UserHandler {
	return &userHandler{svc}
}

// ChangePassword handles POST /api/v1/me/password/change: it decodes the
// request body, extracts the authenticated user and session IDs from the JWT
// claims injected by RequireAuth, delegates to the user service, and responds
// 204 No Content on success. Returns 400 PASSWORD_NOT_SET for OAuth-only
// accounts, 401 INVALID_CREDENTIALS for a wrong current password or a weak new
// password, and 400 INVALID_BODY for a malformed request.
func (h *userHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req models.PasswordChangeRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}

	claims, _ := middleware.AuthClaims(r)
	if err := h.User().ChangePassword(r.Context(), claims.UserID, claims.SessionID, req.CurrentPassword, req.NewPassword); err != nil {
		response.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SetPassword handles POST /api/v1/me/password/set: it decodes the request
// body, extracts the authenticated user and session IDs from the JWT claims
// injected by RequireAuth, delegates to the user service, and responds 204
// No Content on success. Unlike ChangePassword, this sets an *initial*
// password (e.g. for an OAuth-only account) and never verifies a current
// password. Returns 409 PASSWORD_ALREADY_SET if the account already has a
// password identity, 401 INVALID_CREDENTIALS for a weak new password, and
// 400 INVALID_BODY for a malformed request.
func (h *userHandler) SetPassword(w http.ResponseWriter, r *http.Request) {
	var req models.SetPasswordRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}

	claims, _ := middleware.AuthClaims(r)
	if err := h.User().SetPassword(r.Context(), claims.UserID, claims.SessionID, req.NewPassword); err != nil {
		response.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetMe handles GET /api/v1/me: it reads the authenticated user ID from the
// JWT claims injected by RequireAuth, fetches the user, their active
// subscription, and whether they have a password identity from the service,
// and writes the profile as a 200 response wrapped under a "data" key per
// the API spec. Requires a valid access token.
func (h *userHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.AuthClaims(r)

	user, sub, hasPassword, err := h.User().GetMe(r.Context(), claims.UserID)
	if err != nil {
		response.HandleError(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, struct {
		Data models.MeResponse `json:"data"`
	}{Data: toMeResponse(user, sub, hasPassword)})
}

// UpdateProfile handles PATCH /api/v1/me/profile: it decodes a partial
// profile update, extracts the authenticated user ID from the JWT claims
// injected by RequireAuth, delegates to the user service, and responds 200
// with the same {"data": MeResponse} shape GetMe uses. Fields omitted from
// the request body are left unchanged; validation failures respond 400
// INVALID_INPUT.
func (h *userHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateProfileRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}

	claims, _ := middleware.AuthClaims(r)
	if _, err := h.User().UpdateProfile(r.Context(), claims.UserID, models.UpdateProfileInput{
		Name:        req.Name,
		AvatarURL:   req.AvatarURL,
		CompanyName: req.CompanyName,
		Description: req.Description,
		SocialLinks: req.SocialLinks,
	}); err != nil {
		response.HandleError(w, r, err)
		return
	}

	user, sub, hasPassword, err := h.User().GetMe(r.Context(), claims.UserID)
	if err != nil {
		response.HandleError(w, r, err)
		return
	}

	response.JSON(w, http.StatusOK, struct {
		Data models.MeResponse `json:"data"`
	}{Data: toMeResponse(user, sub, hasPassword)})
}

// DeleteAccount handles DELETE /api/v1/me: it decodes the request body,
// extracts the authenticated user and session IDs from the JWT claims
// injected by RequireAuth, delegates to the user service, clears the auth
// cookies (the session no longer exists), and responds 204 No Content on
// success. Returns 401 INVALID_CREDENTIALS if the account has a password
// and current_password doesn't match, and 400 INVALID_BODY for a malformed
// request.
func (h *userHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	var req models.DeleteAccountRequest
	if !response.DecodeJSON(w, r, &req) {
		return
	}

	claims, _ := middleware.AuthClaims(r)
	if err := h.User().DeleteAccount(r.Context(), claims.UserID, claims.SessionID, req.CurrentPassword); err != nil {
		response.HandleError(w, r, err)
		return
	}

	cookies.ClearTokenCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

// toMeResponse builds the shared MeResponse shape returned by GetMe,
// UpdateProfile, and (in auth_handler.go) VerifyEmail.
func toMeResponse(user models.User, sub models.Subscription, hasPassword bool) models.MeResponse {
	return models.MeResponse{
		ID:            user.ID.String(),
		Email:         user.Email,
		EmailVerified: user.EmailVerifiedAt != nil,
		Name:          user.Name,
		AvatarURL:     user.AvatarURL,
		CompanyName:   user.CompanyName,
		Description:   user.Description,
		SocialLinks:   user.SocialLinks,
		HasPassword:   hasPassword,
		Plan: models.MePlanResponse{
			ID:     sub.PlanID,
			Status: sub.Status,
		},
	}
}
