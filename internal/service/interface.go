package service

import (
	"context"
	"linkMe/internal/models"

	"github.com/google/uuid"
)

// TokenPair holds the two credentials issued at the end of an authentication
// flow. AccessToken is a short-lived JWT for stateless request authorization;
// RawRefreshToken is an opaque token delivered as an HttpOnly cookie and used
// to obtain fresh token pairs without re-entering credentials.
type TokenPair struct {
	AccessToken     string
	RawRefreshToken string
}

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
	// subscription, and returns the created user plus a TokenPair.
	Register(ctx context.Context, input models.RegisterInput) (models.User, TokenPair, error)
	// Login authenticates an email/password account: it validates the input,
	// verifies the password against the stored Argon2id hash, and on success
	// issues a new session, returning the authenticated user plus a TokenPair.
	// It returns the same msgs.ErrInvalidCredentials for every authentication
	// failure so callers cannot distinguish an unknown email from a wrong
	// password.
	Login(ctx context.Context, input models.LoginInput) (models.User, TokenPair, error)
	// Refresh validates the given raw refresh token, rotates it (consuming the
	// old session row and creating a new one in the same token family), and
	// issues a fresh TokenPair. If the token belongs to an already-consumed
	// session it returns msgs.ErrTokenReuseDetected and revokes the entire
	// token family. Expired or unknown tokens return msgs.ErrTokenInvalid.
	Refresh(ctx context.Context, rawRefreshToken string) (models.User, TokenPair, error)
	// Logout revokes the session identified by sessionID, invalidating the
	// associated refresh token. Safe to call on an already-revoked session.
	Logout(ctx context.Context, sessionID uuid.UUID) error
	// LogoutAll revokes every active session belonging to userID, signing the
	// user out of all devices.
	LogoutAll(ctx context.Context, userID uuid.UUID) error
	// GetMe returns the authenticated user and their active subscription.
	GetMe(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, error)
	// ChangePassword verifies currentPassword against the stored Argon2id hash,
	// replaces it with a hash of newPassword, and revokes every other active
	// session for the user so stolen refresh tokens are immediately invalidated.
	// Returns msgs.ErrPasswordNotSet for OAuth-only accounts, msgs.ErrInvalidCredentials
	// if currentPassword is wrong, or a validation error if newPassword is weak.
	ChangePassword(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error
}
