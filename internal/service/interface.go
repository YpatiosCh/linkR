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
	// email from a wrong password. Returns msgs.ErrTooManyLoginAttempts if
	// the normalized email has exceeded its login attempt limit, independent
	// of the caller's IP.
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
	// verified, consumes the token, and returns the user, their active
	// subscription, and whether they have a password identity (the "public
	// account representation" the auth spec requires). Returns
	// msgs.ErrTokenInvalid for an unknown or expired token,
	// msgs.ErrTokenAlreadyUsed for a token that was already consumed.
	VerifyEmail(ctx context.Context, token string) (models.User, models.Subscription, bool, error)
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
	// GoogleAuthURL returns the URL to redirect the user to Google's consent
	// screen, plus a freshly generated CSRF state value the caller must
	// persist (e.g. in a signed cookie) and present back to GoogleCallback.
	GoogleAuthURL(ctx context.Context) (authURL string, state string, err error)
	// GoogleCallback exchanges the authorization code for Google's verified
	// profile and signs the user in: an existing google identity signs in
	// as-is; no google identity but a matching verified email attaches a new
	// google identity to that existing account; no match at all creates a
	// new user, google identity, and free subscription. Returns
	// msgs.ErrOAuthEmailNotVerified if Google reports the email unverified.
	GoogleCallback(ctx context.Context, code string) (models.User, models.TokenPair, error)
}

// LoginAttemptLimiter guards against distributed credential-stuffing
// attacks against a single account: unlike the per-IP rate limiter
// wrapping the login route, this is keyed on the normalized email, so an
// attacker spreading attempts across many IPs against one account is still
// throttled. Satisfied by *redis.LoginAttemptLimiter (linkMe/internal/redis)
// — declared here, not imported from there, mirroring SessionRevoker.
type LoginAttemptLimiter interface {
	// Allow reports whether another login attempt for the given normalized
	// email is permitted right now, incrementing its attempt counter as a
	// side effect.
	Allow(ctx context.Context, email string) (bool, error)
}

// UserService defines the operations exposed by the service layer for the
// authenticated current-user resource, such as profile retrieval and
// password changes.
type UserService interface {
	// GetMe returns the authenticated user, their active subscription, and
	// whether they have a password identity (false for an OAuth-only account).
	GetMe(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, bool, error)
	// ChangePassword verifies currentPassword against the stored Argon2id hash,
	// replaces it with a hash of newPassword, and revokes every other active
	// session for the user so stolen refresh tokens are immediately invalidated.
	// Returns msgs.ErrPasswordNotSet for OAuth-only accounts, msgs.ErrInvalidCredentials
	// if currentPassword is wrong, or a validation error if newPassword is weak.
	ChangePassword(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error
	// SetPassword sets an initial password on an account with no password
	// identity yet (e.g. an OAuth-only account), then revokes every other
	// active session. Returns msgs.ErrPasswordAlreadySet if the account
	// already has a password identity, or a validation error if newPassword
	// is weak.
	SetPassword(ctx context.Context, userID, sessionID uuid.UUID, newPassword string) error
	// UpdateProfile applies a partial update to the authenticated user's
	// profile (name, avatar, company name, description, social links) and
	// returns the updated user. Nil fields in input are left unchanged;
	// validation failures return msgs.ErrInvalidInput.
	UpdateProfile(ctx context.Context, userID uuid.UUID, input models.UpdateProfileInput) (models.User, error)
	// DeleteAccount soft-deletes the authenticated user's account and
	// revokes every active session. currentPassword is required and
	// verified only if the account has a password identity; returns
	// msgs.ErrInvalidCredentials if it doesn't match.
	DeleteAccount(ctx context.Context, userID, sessionID uuid.UUID, currentPassword string) error
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

// SessionRevoker marks sessions as revoked in the shared, fast-path
// revocation store (Redis) that middleware.RequireAuth checks on every
// request, so a revoked session's access token stops working immediately
// rather than lingering until its natural JWT expiry. Called by AuthService
// and UserService right after the corresponding repository.SessionRepository
// revoke call succeeds. Satisfied by *redis.SessionRevocationStore
// (linkMe/internal/redis) — declared here, not imported from there, so this
// package stays decoupled from Redis and easily testable with a fake.
type SessionRevoker interface {
	// RevokeSession marks a single session as revoked.
	RevokeSession(ctx context.Context, sessionID uuid.UUID) error
	// RevokeSessions marks every session in sessionIDs as revoked.
	RevokeSessions(ctx context.Context, sessionIDs []uuid.UUID) error
}
