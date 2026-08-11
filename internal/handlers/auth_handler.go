package handlers

import (
	"encoding/json"
	"linkMe/internal/models"
	"linkMe/internal/service"
	"linkMe/internal/utils/response"
	"net/http"
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

// registerRequest is the JSON body expected by the register endpoint.
type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// userResponse is the JSON body returned on a successful registration.
type userResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Register handles POST /auth/register: it decodes the JSON request body
// into a registerRequest, calls the auth service's Register with the
// decoded fields, and writes the created user as a 201 response.
// On a decode failure it responds 400 with CodeInvalidBody; on a service
// error it maps the error to the appropriate status via response.HandleError.
// On success it also sets an HttpOnly refresh_token cookie from the raw
// token returned by the service.
func (h *authHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, response.CodeInvalidBody, "request body is malformed")
		return
	}

	user, rawToken, err := h.Auth().Register(r.Context(), models.RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
	})
	if err != nil {
		response.HandleError(w, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    rawToken,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	response.JSON(w, http.StatusCreated, userResponse{
		ID:    user.ID.String(),
		Email: user.Email,
		Name:  user.Name,
	})
}
