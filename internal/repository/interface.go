package repository

import (
	"context"

	"linkMe/internal/models"

	"github.com/google/uuid"
)

// Repository is the top-level data-access boundary. It exposes access to
// the individual entity repositories and allows running a unit of work
// inside a single database transaction.
type Repository interface {
	// WithinTx runs fn inside a database transaction. The context passed
	// to fn carries the active transaction, so all repository calls made
	// with it participate in the same transaction.
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
	// User returns the user repository.
	User() UserRepository
	// AuthIdentity returns the auth identity repository.
	AuthIdentity() AuthIdentityRepository
	// Subscription returns the subscription repository.
	Subscription() SubscriptionRepository
	// Session returns the session repository.
	Session() SessionRepository
}

// UserRepository defines the data-access operations for users.
type UserRepository interface {
	// CreateUser persists a new user and returns the stored user,
	// including database-assigned timestamps.
	CreateUser(ctx context.Context, user models.User) (models.User, error)
	// GetUserByEmail returns the non-deleted user with the given email,
	// or an error when no such user exists.
	GetUserByEmail(ctx context.Context, email string) (models.User, error)
	// GetUserByID returns the non-deleted user with the given ID, or an
	// error when no such user exists.
	GetUserByID(ctx context.Context, id uuid.UUID) (models.User, error)
}

// AuthIdentityRepository defines the data-access operations for auth
// identities, which link a user to a password-based or external provider.
type AuthIdentityRepository interface {
	// CreateAuthIdentity persists a new auth identity and returns the
	// stored identity.
	CreateAuthIdentity(ctx context.Context, identity models.AuthIdentity) (models.AuthIdentity, error)
	// GetAuthIdentityByProviderAndSubject returns the auth identity for
	// the given provider and provider subject, or msgs.ErrUserNotFound when
	// none exists.
	GetAuthIdentityByProviderAndSubject(ctx context.Context, provider, subject string) (models.AuthIdentity, error)
	// GetAuthIdentityByUserIDAndProvider returns the auth identity for
	// the given user and provider, or msgs.ErrUserNotFound when none exists.
	GetAuthIdentityByUserIDAndProvider(ctx context.Context, userID uuid.UUID, provider string) (models.AuthIdentity, error)
	// UpdatePasswordHash replaces the password_hash on the password identity
	// for the given user. The caller must ensure the identity exists first.
	UpdatePasswordHash(ctx context.Context, userID uuid.UUID, newHash string) error
}

// SubscriptionRepository defines the data-access operations for user
// subscriptions.
type SubscriptionRepository interface {
	// CreateUserSubscription persists a new subscription for a user and
	// returns the stored subscription.
	CreateUserSubscription(ctx context.Context, sub models.Subscription) (models.Subscription, error)
	// GetActiveSubscriptionByUserID returns the most recent active subscription
	// for the given user, or msgs.ErrSubscriptionNotFound when none exists.
	GetActiveSubscriptionByUserID(ctx context.Context, userID uuid.UUID) (models.Subscription, error)
}

// SessionRepository defines the data-access operations for refresh-token
// sessions.
type SessionRepository interface {
	// CreateSession persists a new session and returns the stored session.
	CreateSession(ctx context.Context, s models.Session) (models.Session, error)
	// GetSessionByTokenHash returns the session whose refresh_token_hash
	// matches the given hash. It returns any session — including revoked ones
	// — so callers can distinguish token reuse from a never-valid token.
	// Returns msgs.ErrTokenInvalid when no session matches.
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (models.Session, error)
	// MarkSessionConsumed sets revoked_at on the session with the given ID,
	// recording that it has been rotated and must not be used again.
	MarkSessionConsumed(ctx context.Context, id uuid.UUID) error
	// RevokeSessionFamily sets revoked_at on every non-revoked session in
	// the given token family, used when reuse of a consumed token is detected.
	RevokeSessionFamily(ctx context.Context, familyID uuid.UUID) error
	// RevokeSession sets revoked_at on the session with the given ID,
	// invalidating it. Idempotent — safe to call on an already-revoked session.
	RevokeSession(ctx context.Context, id uuid.UUID) error
	// RevokeAllSessionsForUser sets revoked_at on every non-revoked session
	// belonging to the given user, used during logout-all.
	RevokeAllSessionsForUser(ctx context.Context, userID uuid.UUID) error
	// RevokeOtherSessionsForUser sets revoked_at on every non-revoked session
	// belonging to the given user except the session with keepSessionID, used
	// during password change to keep the current session alive.
	RevokeOtherSessionsForUser(ctx context.Context, userID uuid.UUID, keepSessionID uuid.UUID) error
}
