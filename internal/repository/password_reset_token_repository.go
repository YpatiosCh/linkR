package repository

import (
	"context"
	"errors"
	"fmt"
	db "linkMe/internal/db/generated"
	"linkMe/internal/models"
	"linkMe/internal/msgs"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// passwordResetTokenRepository implements PasswordResetTokenRepository on
// top of sqlc-generated queries. It holds the base *db.Queries used outside
// a transaction.
type passwordResetTokenRepository struct {
	queries *db.Queries
}

// NewPasswordResetTokenRepository creates a passwordResetTokenRepository
// bound to dbtx, which is either the connection pool or an active
// transaction.
func NewPasswordResetTokenRepository(dbtx db.DBTX) *passwordResetTokenRepository {
	return &passwordResetTokenRepository{queries: db.New(dbtx)}
}

// querier returns db.Queries bound to the active transaction when ctx
// carries one (see injectTx), otherwise it falls back to the repository's
// base queries so calls participate in an ongoing transaction.
func (r *passwordResetTokenRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := extractTx(ctx); ok {
		return db.New(tx)
	}
	return r.queries
}

// CreatePasswordResetToken inserts a new token via the
// CreatePasswordResetToken query and maps the returned row to a domain
// models.PasswordResetToken. Any database error is wrapped with context and
// returned.
func (r *passwordResetTokenRepository) CreatePasswordResetToken(ctx context.Context, t models.PasswordResetToken) (models.PasswordResetToken, error) {
	q := r.querier(ctx)
	row, err := q.CreatePasswordResetToken(ctx, db.CreatePasswordResetTokenParams{
		ID:        t.ID,
		UserID:    t.UserID,
		TokenHash: t.TokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: t.ExpiresAt, Valid: true},
	})
	if err != nil {
		return models.PasswordResetToken{}, fmt.Errorf("error creating password reset token: %w", err)
	}
	return dbPasswordResetTokenToDomain(row), nil
}

// GetPasswordResetTokenByHash returns the token whose token_hash matches the
// given hash, including already-used tokens so that callers can detect
// reuse. pgx.ErrNoRows is translated to msgs.ErrTokenInvalid; other errors
// are wrapped with context.
func (r *passwordResetTokenRepository) GetPasswordResetTokenByHash(ctx context.Context, tokenHash string) (models.PasswordResetToken, error) {
	q := r.querier(ctx)
	row, err := q.GetPasswordResetTokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.PasswordResetToken{}, msgs.ErrTokenInvalid
		}
		return models.PasswordResetToken{}, fmt.Errorf("getting password reset token by hash: %w", err)
	}
	return dbPasswordResetTokenToDomain(row), nil
}

// MarkPasswordResetTokenConsumed sets used_at = now() on the token with the
// given ID. Any database error is wrapped with context.
func (r *passwordResetTokenRepository) MarkPasswordResetTokenConsumed(ctx context.Context, id uuid.UUID) error {
	q := r.querier(ctx)
	if err := q.MarkPasswordResetTokenConsumed(ctx, id); err != nil {
		return fmt.Errorf("marking password reset token consumed: %w", err)
	}
	return nil
}

// dbPasswordResetTokenToDomain maps a sqlc db.PasswordResetToken row to a
// domain models.PasswordResetToken, converting the nullable UsedAt pgtype
// field into a nil-able pointer.
func dbPasswordResetTokenToDomain(row db.PasswordResetToken) models.PasswordResetToken {
	t := models.PasswordResetToken{
		ID:        row.ID,
		UserID:    row.UserID,
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt.Time,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.UsedAt.Valid {
		t.UsedAt = &row.UsedAt.Time
	}
	return t
}
