package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RepoManager is the concrete Repository implementation. It owns a
// pgxpool.Pool and the per-entity repository instances bound to it.
type RepoManager struct {
	pool                       *pgxpool.Pool
	userRepo                   UserRepository
	authIdentityRepo           AuthIdentityRepository
	subscriptionRepo           SubscriptionRepository
	sessionRepo                SessionRepository
	emailVerificationTokenRepo EmailVerificationTokenRepository
	passwordResetTokenRepo     PasswordResetTokenRepository
}

// NewRepoManager wires the pool into each entity repository and returns
// them behind the Repository interface.
func NewRepoManager(pool *pgxpool.Pool) Repository {
	return &RepoManager{
		pool:                       pool,
		userRepo:                   NewUserRepository(pool),
		authIdentityRepo:           NewAuthIdentityRepository(pool),
		subscriptionRepo:           NewSubscriptionRepository(pool),
		sessionRepo:                NewSessionRepository(pool),
		emailVerificationTokenRepo: NewEmailVerificationTokenRepository(pool),
		passwordResetTokenRepo:     NewPasswordResetTokenRepository(pool),
	}
}

// WithinTx begins a transaction on the pool, injects it into ctx (see
// injectTx), runs fn, and commits on success. Any error from fn or Commit
// causes the deferred Rollback to discard the transaction.
func (r *RepoManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	txCtx := injectTx(ctx, tx)

	if err := fn(txCtx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// User returns the user repository.
func (r *RepoManager) User() UserRepository {
	return r.userRepo
}

// AuthIdentity returns the auth identity repository.
func (r *RepoManager) AuthIdentity() AuthIdentityRepository {
	return r.authIdentityRepo
}

// Subscription returns the subscription repository.
func (r *RepoManager) Subscription() SubscriptionRepository {
	return r.subscriptionRepo
}

// Session returns the session repository.
func (r *RepoManager) Session() SessionRepository {
	return r.sessionRepo
}

// EmailVerificationToken returns the email verification token repository.
func (r *RepoManager) EmailVerificationToken() EmailVerificationTokenRepository {
	return r.emailVerificationTokenRepo
}

// PasswordResetToken returns the password reset token repository.
func (r *RepoManager) PasswordResetToken() PasswordResetTokenRepository {
	return r.passwordResetTokenRepo
}
