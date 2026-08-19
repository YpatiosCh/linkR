package models

import (
	"time"

	"github.com/google/uuid"
)

// AuditEventType identifies the kind of security/account event an
// AuditEvent records. Values are developer-controlled constants, never
// derived from user input, so — unlike SocialPlatform — there is no
// runtime Valid() check; the audit_events.event_type column itself is
// unconstrained TEXT.
type AuditEventType string

const (
	// AuditUserRegistered is recorded when a brand-new account is created,
	// whether via Register or GoogleCallback — metadata's "provider" field
	// ("password" or "google") distinguishes which.
	AuditUserRegistered AuditEventType = "USER_REGISTERED"
	// AuditAccountReactivated is recorded when Register restores a
	// previously soft-deleted account instead of creating a new one.
	AuditAccountReactivated AuditEventType = "ACCOUNT_REACTIVATED"
	// AuditPasswordIdentityAttached is recorded when Register attaches a
	// new password identity to an existing Google-only account.
	AuditPasswordIdentityAttached AuditEventType = "PASSWORD_IDENTITY_ATTACHED"
	// AuditLoginSucceeded is recorded on a successful Login.
	AuditLoginSucceeded AuditEventType = "LOGIN_SUCCEEDED"
	// AuditLoginFailed is recorded on any Login credential failure (unknown
	// email, wrong password, OAuth-only account, deleted account).
	AuditLoginFailed AuditEventType = "LOGIN_FAILED"
	// AuditLoginRateLimited is recorded when the email-keyed login attempt
	// limiter denies a Login attempt.
	AuditLoginRateLimited AuditEventType = "LOGIN_RATE_LIMITED"
	// AuditLogout is recorded when a single session is revoked via Logout.
	AuditLogout AuditEventType = "LOGOUT"
	// AuditLogoutAll is recorded when every session for a user is revoked
	// via LogoutAll.
	AuditLogoutAll AuditEventType = "LOGOUT_ALL"
	// AuditRefreshTokenReuseDetected is recorded when Refresh detects an
	// already-consumed refresh token being reused.
	AuditRefreshTokenReuseDetected AuditEventType = "REFRESH_TOKEN_REUSE_DETECTED"
	// AuditPasswordChanged is recorded on a successful ChangePassword.
	AuditPasswordChanged AuditEventType = "PASSWORD_CHANGED"
	// AuditPasswordSet is recorded on a successful SetPassword (an
	// OAuth-only account configuring its first password).
	AuditPasswordSet AuditEventType = "PASSWORD_SET"
	// AuditPasswordResetRequested is recorded when RequestPasswordReset
	// actually issues a token (not on its silent unknown-email no-op).
	AuditPasswordResetRequested AuditEventType = "PASSWORD_RESET_REQUESTED"
	// AuditPasswordResetCompleted is recorded on a successful ResetPassword.
	AuditPasswordResetCompleted AuditEventType = "PASSWORD_RESET_COMPLETED"
	// AuditEmailVerificationRequested is recorded when
	// RequestEmailVerification actually issues a token (not on its silent
	// unknown-email/already-verified no-op).
	AuditEmailVerificationRequested AuditEventType = "EMAIL_VERIFICATION_REQUESTED"
	// AuditEmailVerified is recorded on a successful VerifyEmail.
	AuditEmailVerified AuditEventType = "EMAIL_VERIFIED"
	// AuditGoogleLogin is recorded when GoogleCallback signs in via an
	// existing google identity.
	AuditGoogleLogin AuditEventType = "GOOGLE_LOGIN"
	// AuditGoogleLinked is recorded when GoogleCallback attaches a new
	// google identity to an existing password account.
	AuditGoogleLinked AuditEventType = "GOOGLE_LINKED"
	// AuditProfileUpdated is recorded on a successful UpdateProfile;
	// metadata lists which fields changed (names only, never values).
	AuditProfileUpdated AuditEventType = "PROFILE_UPDATED"
	// AuditAccountDeleted is recorded on a successful DeleteAccount.
	AuditAccountDeleted AuditEventType = "ACCOUNT_DELETED"
)

// AuditEvent is a single row in the audit_events table: an immutable
// record of a security/account-relevant action. UserID is nil for
// pre-authentication events where no account is known (e.g. a failed login
// against an unrecognized email). Metadata never contains secrets
// (passwords, tokens, authorization codes) — see the Never-log list in
// authentication_v1_backend_spec.md §31.
type AuditEvent struct {
	ID        uuid.UUID
	UserID    *uuid.UUID
	EventType AuditEventType
	IPAddress *string
	UserAgent *string
	Metadata  map[string]any
	CreatedAt time.Time
}
