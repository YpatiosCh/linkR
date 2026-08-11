package service

import (
	"context"
	"linkMe/internal/models"
)

// Service is the top-level service interface exposing the sub-services of
// the service layer, such as the auth service.
type Service interface {
	// Auth returns the auth service used to handle authentication flows.
	Auth() AuthService
}

// AuthService defines the authentication operations exposed by the service
// layer, such as user registration.
type AuthService interface {
	// Register creates a new user account: it validates the input, hashes the
	// password, persists the user with a password auth identity and a free
	// subscription, and returns the created user plus a session token.
	Register(ctx context.Context, input models.RegisterInput) (models.User, string, error)
}
