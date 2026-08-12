package handlers

import (
	"encoding/json"
	"linkMe/internal/middleware"
	"linkMe/internal/models"
	"linkMe/internal/service"
	"linkMe/internal/utils/response"
	"net/http"
)

// meHandler implements MeHandler by delegating to the embedded service layer.
type meHandler struct {
	service.Service
}

// NewMeHandler constructs a MeHandler backed by the given service.
func NewMeHandler(svc service.Service) MeHandler {
	return &meHandler{svc}
}

// ChangePassword handles POST /api/v1/me/password/change: it decodes the
// request body, extracts the authenticated user and session IDs from the JWT
// claims injected by RequireAuth, delegates to the auth service, and responds
// 204 No Content on success. Returns 400 PASSWORD_NOT_SET for OAuth-only
// accounts, 401 INVALID_CREDENTIALS for a wrong current password or a weak new
// password, and 400 INVALID_BODY for a malformed request.
func (h *meHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req models.PasswordChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidBody, "request body is malformed")
		return
	}

	claims, _ := middleware.AuthClaims(r)
	if err := h.Auth().ChangePassword(r.Context(), claims.UserID, claims.SessionID, req.CurrentPassword, req.NewPassword); err != nil {
		response.HandleError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetMe handles GET /api/v1/me: it reads the authenticated user ID from the
// JWT claims injected by RequireAuth, fetches the user and their active
// subscription from the service, and writes the profile as a 200 response
// wrapped under a "data" key per the API spec. Requires a valid access token.
func (h *meHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.AuthClaims(r)

	user, sub, err := h.Auth().GetMe(r.Context(), claims.UserID)
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
