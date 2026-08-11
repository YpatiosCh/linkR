package handlers

import "net/http"

// Handler is the top-level HTTP handler contract that exposes the
// individual handler groups an HTTP server can register on its routes.
type Handler interface {
	// Auth returns the AuthHandler, which handles all authentication
	// related HTTP endpoints such as registration.
	Auth() AuthHandler
}

// AuthHandler defines the HTTP endpoints for user authentication.
type AuthHandler interface {
	// Register handles POST /auth/register: decodes the request body,
	// calls the auth service, and writes the standardized response.
	Register(w http.ResponseWriter, r *http.Request)
}
