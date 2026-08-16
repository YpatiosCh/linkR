package msgs

import "errors"

// Sentinel errors returned by the service layer. Callers match them with
// errors.Is to turn failures into user-facing API responses.
var (
	// ErrInvalidCredentials is returned when the email or password does not match a known account.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrEmailAlreadyExists is returned when registration uses an email that already has an account.
	ErrEmailAlreadyExists = errors.New("account already exists")
	// ErrUserNotFound is returned when no account matches the requested identifier.
	ErrUserNotFound = errors.New("user not found")
	// ErrPasswordNotSet is returned when the account has no password, such as OAuth-only accounts.
	ErrPasswordNotSet = errors.New("password not configured for this account")
	// ErrTokenInvalid is returned when a token is malformed or expired.
	ErrTokenInvalid = errors.New("token is invalid or expired")
	// ErrTokenAlreadyUsed is returned when a single-use token is presented a second time.
	ErrTokenAlreadyUsed = errors.New("token has already been used")
	// ErrSessionRevoked is returned when the session has been explicitly revoked.
	ErrSessionRevoked = errors.New("session has been revoked")
	// ErrTokenReuseDetected is returned when an already-rotated refresh token is used again.
	ErrTokenReuseDetected = errors.New("refresh token reuse detected")
	// ErrSubscriptionNotFound is returned when no active subscription exists for the user.
	ErrSubscriptionNotFound = errors.New("no active subscription found")
	// ErrOAuthEmailNotVerified is returned when a Google account's email claim
	// is not verified; such accounts are rejected rather than trusted.
	ErrOAuthEmailNotVerified = errors.New("google account email is not verified")
	// ErrInvalidInput is returned when a request's fields fail validation
	// outside of a credentials context (e.g. profile updates) — unlike
	// ErrInvalidCredentials, it doesn't imply anything about authentication.
	ErrInvalidInput = errors.New("invalid input")
	// ErrPasswordAlreadySet is returned when SetPassword is called on an
	// account that already has a password identity — use ChangePassword instead.
	ErrPasswordAlreadySet = errors.New("password already configured for this account")
)
