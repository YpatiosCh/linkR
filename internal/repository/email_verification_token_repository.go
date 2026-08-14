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

// emailVerificationTokenRepository implements EmailVerificationTokenRepository
// on top of sqlc-generated queries. It holds the base *db.Queries used
// outside a transaction.
type emailVerificationTokenRepository struct {
	queries *db.Queries
}

// NewEmailVerificationTokenRepository creates an
// emailVerificationTokenRepository bound to dbtx, which is either the
// connection pool or an active transaction.
func NewEmailVerificationTokenRepository(dbtx db.DBTX) *emailVerificationTokenRepository {
	return &emailVerificationTokenRepository{queries: db.New(dbtx)}
}

// querier returns db.Queries bound to the active transaction when ctx
// carries one (see injectTx), otherwise it falls back to the repository's
// base queries so calls participate in an ongoing transaction.
func (r *emailVerificationTokenRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := extractTx(ctx); ok {
		return db.New(tx)
	}
	return r.queries
}

// CreateEmailVerificationToken inserts a new token via the
// CreateEmailVerificationToken query and maps the returned row to a domain
// models.EmailVerificationToken. Any database error is wrapped with context
// and returned.
func (r *emailVerificationTokenRepository) CreateEmailVerificationToken(ctx context.Context, t models.EmailVerificationToken) (models.EmailVerificationToken, error) {
	q := r.querier(ctx)
	row, err := q.CreateEmailVerificationToken(ctx, db.CreateEmailVerificationTokenParams{
		ID:        t.ID,
		UserID:    t.UserID,
		TokenHash: t.TokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: t.ExpiresAt, Valid: true},
	})
	if err != nil {
		return models.EmailVerificationToken{}, fmt.Errorf("error creating email verification token: %w", err)
	}
	return dbEmailVerificationTokenToDomain(row), nil
}

// GetEmailVerificationTokenByHash returns the token whose token_hash matches
// the given hash, including already-used tokens so that callers can detect
// reuse. pgx.ErrNoRows is translated to msgs.ErrTokenInvalid; other errors
// are wrapped with context.
func (r *emailVerificationTokenRepository) GetEmailVerificationTokenByHash(ctx context.Context, tokenHash string) (models.EmailVerificationToken, error) {
	q := r.querier(ctx)
	row, err := q.GetEmailVerificationTokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.EmailVerificationToken{}, msgs.ErrTokenInvalid
		}
		return models.EmailVerificationToken{}, fmt.Errorf("getting email verification token by hash: %w", err)
	}
	return dbEmailVerificationTokenToDomain(row), nil
}

// MarkEmailVerificationTokenConsumed sets used_at = now() on the token with
// the given ID. Any database error is wrapped with context.
func (r *emailVerificationTokenRepository) MarkEmailVerificationTokenConsumed(ctx context.Context, id uuid.UUID) error {
	q := r.querier(ctx)
	if err := q.MarkEmailVerificationTokenConsumed(ctx, id); err != nil {
		return fmt.Errorf("marking email verification token consumed: %w", err)
	}
	return nil
}

// dbEmailVerificationTokenToDomain maps a sqlc db.EmailVerificationToken row
// to a domain models.EmailVerificationToken, converting the nullable UsedAt
// pgtype field into a nil-able pointer.
func dbEmailVerificationTokenToDomain(row db.EmailVerificationToken) models.EmailVerificationToken {
	t := models.EmailVerificationToken{
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
