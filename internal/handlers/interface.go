package handlers

import "net/http"

// Handler is the top-level HTTP handler contract that exposes the
// individual handler groups an HTTP server can register on its routes.
type Handler interface {
	// Auth returns the AuthHandler, which handles all authentication
	// related HTTP endpoints such as registration.
	Auth() AuthHandler
	// User returns the UserHandler, which handles authenticated current-user endpoints.
	User() UserHandler
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
	// RequestEmailVerification handles POST /api/v1/auth/email/verification/request:
	// decodes the request body and always responds with a generic message,
	// regardless of whether the email exists.
	RequestEmailVerification(w http.ResponseWriter, r *http.Request)
	// VerifyEmail handles POST /api/v1/auth/email/verification/verify:
	// consumes the token, marks the account's email verified, and writes the
	// account's public representation.
	VerifyEmail(w http.ResponseWriter, r *http.Request)
	// RequestPasswordReset handles POST /api/v1/auth/password/reset/request:
	// decodes the request body and always responds with a generic message,
	// regardless of whether the email exists.
	RequestPasswordReset(w http.ResponseWriter, r *http.Request)
	// ResetPassword handles POST /api/v1/auth/password/reset/confirm:
	// consumes the token, replaces the account's password, and revokes every
	// active session for the account.
	ResetPassword(w http.ResponseWriter, r *http.Request)
	// GoogleStart handles GET /api/v1/auth/google: generates a CSRF state
	// value, signs it into a short-lived cookie, and redirects to Google's
	// consent screen.
	GoogleStart(w http.ResponseWriter, r *http.Request)
	// GoogleCallback handles GET /api/v1/auth/google/callback: validates the
	// state cookie against the query parameter, exchanges the authorization
	// code, signs the user in, sets the auth cookies, and redirects to the
	// frontend.
	GoogleCallback(w http.ResponseWriter, r *http.Request)
}

// UserHandler defines the HTTP endpoints for the authenticated current-user resource.
type UserHandler interface {
	// GetMe handles GET /api/v1/me: returns the authenticated user's profile
	// and active plan. Requires a valid access token.
	GetMe(w http.ResponseWriter, r *http.Request)
	// ChangePassword handles POST /api/v1/me/password/change: verifies the
	// current password, replaces it, and revokes all other sessions. Requires a
	// valid access token.
	ChangePassword(w http.ResponseWriter, r *http.Request)
	// SetPassword handles POST /api/v1/me/password/set: sets an initial
	// password on an account with no password identity yet (e.g. an
	// OAuth-only account), then revokes all other sessions. Requires a
	// valid access token.
	SetPassword(w http.ResponseWriter, r *http.Request)
	// UpdateProfile handles PATCH /api/v1/me/profile: applies a partial
	// update to the authenticated user's profile (name, avatar, company
	// name, description, social links) and returns the updated profile.
	// Requires a valid access token.
	UpdateProfile(w http.ResponseWriter, r *http.Request)
	// DeleteAccount handles DELETE /api/v1/me: soft-deletes the
	// authenticated user's account and revokes every active session.
	// Requires a valid access token.
	DeleteAccount(w http.ResponseWriter, r *http.Request)
}
