package handlers

import "net/http"

// Handler is the top-level HTTP handler contract that exposes the
// individual handler groups an HTTP server can register on its routes.
type Handler interface {
	// Auth returns the AuthHandler, which handles all authentication
	// related HTTP endpoints such as registration.
	Auth() AuthHandler
	// Me returns the MeHandler, which handles authenticated current-user endpoints.
	Me() MeHandler
}

// AuthHandler defines the HTTP endpoints for user authentication.
type AuthHandler interface {
	// Register handles POST /api/v1/auth/register: decodes the request body,
	// calls the auth service, and writes the standardized response.
	Register(w http.ResponseWriter, r *http.Request)
	// Login handles POST /api/v1/auth/login: decodes the request body, calls the
	// auth service, sets the refresh-token cookie, and writes the
	// standardized response.
	Login(w http.ResponseWriter, r *http.Request)
	// Refresh handles POST /api/v1/auth/refresh: reads the refresh_token cookie,
	// calls the auth service to rotate the token pair, sets new cookies, and
	// writes the token expiry.
	Refresh(w http.ResponseWriter, r *http.Request)
	// Logout handles POST /api/v1/auth/logout: revokes the current session and
	// clears authentication cookies. Requires a valid access token.
	Logout(w http.ResponseWriter, r *http.Request)
	// LogoutAll handles POST /api/v1/auth/logout-all: revokes all sessions for
	// the authenticated user and clears authentication cookies.
	LogoutAll(w http.ResponseWriter, r *http.Request)
}

// MeHandler defines the HTTP endpoints for the authenticated current-user resource.
type MeHandler interface {
	// GetMe handles GET /api/v1/me: returns the authenticated user's profile
	// and active plan. Requires a valid access token.
	GetMe(w http.ResponseWriter, r *http.Request)
	// ChangePassword handles POST /api/v1/me/password/change: verifies the
	// current password, replaces it, and revokes all other sessions. Requires a
	// valid access token.
	ChangePassword(w http.ResponseWriter, r *http.Request)
}
