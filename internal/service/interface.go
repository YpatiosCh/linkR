package service

import (
	"context"
	"linkMe/internal/models"

	"github.com/google/uuid"
)

// Service is the top-level service interface exposing the sub-services of
// the service layer, such as the auth and user services.
type Service interface {
	// Auth returns the auth service used to handle authentication flows.
	Auth() AuthService
	// User returns the user service used to handle authenticated
	// current-user profile operations.
	User() UserService
	// Email returns the email service used to send transactional emails.
	Email() EmailService
}

// AuthService defines the authentication operations exposed by the service
// layer, such as user registration.
type AuthService interface {
	// Register creates a new user account: it validates the input, hashes the
	// password, persists the user with a password auth identity and a free
	// subscription, and returns the created user plus a models.TokenPair.
	Register(ctx context.Context, input models.RegisterInput) (models.User, models.TokenPair, error)
	// Login authenticates an email/password account: it validates the input,
	// verifies the password against the stored Argon2id hash, and on success
	// issues a new session, returning the authenticated user plus a
	// models.TokenPair. It returns the same msgs.ErrInvalidCredentials for
	// every authentication failure so callers cannot distinguish an unknown
	// email from a wrong password.
	Login(ctx context.Context, input models.LoginInput) (models.User, models.TokenPair, error)
	// Refresh validates the given raw refresh token, rotates it (consuming the
	// old session row and creating a new one in the same token family), and
	// issues a fresh models.TokenPair. If the token belongs to an
	// already-consumed session it returns msgs.ErrTokenReuseDetected and
	// revokes the entire token family. Expired or unknown tokens return
	// msgs.ErrTokenInvalid.
	Refresh(ctx context.Context, rawRefreshToken string) (models.User, models.TokenPair, error)
	// Logout revokes the session identified by sessionID, invalidating the
	// associated refresh token. Safe to call on an already-revoked session.
	Logout(ctx context.Context, sessionID uuid.UUID) error
	// LogoutAll revokes every active session belonging to userID, signing the
	// user out of all devices.
	LogoutAll(ctx context.Context, userID uuid.UUID) error
	// RequestEmailVerification issues a new email verification token for the
	// account with the given email and sends it via EmailService. It is a
	// silent no-op — no token, no email, no error — when no account matches
	// email or when the account's email is already verified, so the caller
	// can always respond with the same generic message regardless of outcome
	// (enumeration defense).
	RequestEmailVerification(ctx context.Context, email string) error
	// VerifyEmail consumes an email verification token: it validates the
	// token exists, is unexpired, and unused, marks the owning user's email
	// verified, consumes the token, and returns the user plus their active
	// subscription (the "public account representation" the auth spec
	// requires). Returns msgs.ErrTokenInvalid for an unknown or expired
	// token, msgs.ErrTokenAlreadyUsed for a token that was already consumed.
	VerifyEmail(ctx context.Context, token string) (models.User, models.Subscription, error)
	// RequestPasswordReset issues a new password reset token for the account
	// with the given email and sends it via EmailService. It is a silent
	// no-op — no token, no email, no error — when no account matches email,
	// so the caller can always respond with the same generic message
	// regardless of outcome (enumeration defense).
	RequestPasswordReset(ctx context.Context, email string) error
	// ResetPassword consumes a password reset token: it validates the token
	// exists, is unexpired, and unused, validates newPassword, replaces the
	// account's password hash, consumes the token, and revokes every active
	// session for the account. Returns msgs.ErrTokenInvalid for an unknown or
	// expired token, msgs.ErrTokenAlreadyUsed for a token that was already
	// consumed, msgs.ErrPasswordNotSet for an OAuth-only account, or
	// msgs.ErrInvalidCredentials if newPassword is weak.
	ResetPassword(ctx context.Context, token string, newPassword string) error
}

// UserService defines the operations exposed by the service layer for the
// authenticated current-user resource, such as profile retrieval and
// password changes.
type UserService interface {
	// GetMe returns the authenticated user and their active subscription.
	GetMe(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, error)
	// ChangePassword verifies currentPassword against the stored Argon2id hash,
	// replaces it with a hash of newPassword, and revokes every other active
	// session for the user so stolen refresh tokens are immediately invalidated.
	// Returns msgs.ErrPasswordNotSet for OAuth-only accounts, msgs.ErrInvalidCredentials
	// if currentPassword is wrong, or a validation error if newPassword is weak.
	ChangePassword(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error
}

// EmailService sends the transactional emails required by the authentication
// flows. SendVerificationEmail and SendPasswordResetEmail take only the
// recipient and the raw (unhashed) token; link construction is the
// implementation's responsibility.
type EmailService interface {
	// SendVerificationEmail sends an email to email containing a link that
	// lets the recipient confirm ownership of the address using token.
	SendVerificationEmail(ctx context.Context, email string, token string) error
	// SendPasswordResetEmail sends an email to email containing a link that
	// lets the recipient set a new password using token.
	SendPasswordResetEmail(ctx context.Context, email string, token string) error
}
