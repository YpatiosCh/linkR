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

// authIdentityRepository implements AuthIdentityRepository on top of
// sqlc-generated queries. It holds the base *db.Queries used outside a
// transaction.
type authIdentityRepository struct {
	queries *db.Queries
}

// NewAuthIdentityRepository creates an authIdentityRepository bound to
// dbtx, which is either the connection pool or an active transaction.
func NewAuthIdentityRepository(dbtx db.DBTX) *authIdentityRepository {
	return &authIdentityRepository{queries: db.New(dbtx)}
}

// querier returns db.Queries bound to the active transaction when ctx
// carries one (see injectTx), otherwise it falls back to the repository's
// base queries so calls participate in an ongoing transaction.
func (r *authIdentityRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := extractTx(ctx); ok {
		return db.New(tx)
	}
	return r.queries
}

// CreateAuthIdentity inserts a new auth identity via the
// CreateAuthIdentity query and maps the returned row to a domain
// models.AuthIdentity. A nil PasswordHash is stored as SQL NULL. Any
// database error is wrapped with context and returned.
func (r *authIdentityRepository) CreateAuthIdentity(ctx context.Context, identity models.AuthIdentity) (models.AuthIdentity, error) {
	q := r.querier(ctx)
	var passwordHash pgtype.Text
	if identity.PasswordHash != nil {
		passwordHash = pgtype.Text{String: *identity.PasswordHash, Valid: true}
	}

	row, err := q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
		ID:              identity.ID,
		UserID:          identity.UserID,
		Provider:        identity.Provider,
		ProviderSubject: identity.ProviderSubject,
		PasswordHash:    passwordHash,
	})
	if err != nil {
		return models.AuthIdentity{}, fmt.Errorf("error creating auth identity: %w", err)
	}

	return dbAuthIdentityToDomain(row), nil
}

// GetAuthIdentityByProviderAndSubject runs the
// GetAuthIdentityByProviderAndSubject query for the given provider and
// provider subject. pgx.ErrNoRows is translated to msgs.ErrUserNotFound;
// other errors are wrapped and returned.
func (r *authIdentityRepository) GetAuthIdentityByProviderAndSubject(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
	q := r.querier(ctx)
	row, err := q.GetAuthIdentityByProviderAndSubject(ctx, db.GetAuthIdentityByProviderAndSubjectParams{
		Provider:        provider,
		ProviderSubject: subject,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		}
		return models.AuthIdentity{}, fmt.Errorf("error getting auth identity: %w", err)
	}
	return dbAuthIdentityToDomain(row), nil
}

// GetAuthIdentityByUserIDAndProvider returns the auth identity for the given
// user and provider. pgx.ErrNoRows is translated to msgs.ErrUserNotFound; other
// errors are wrapped and returned.
func (r *authIdentityRepository) GetAuthIdentityByUserIDAndProvider(ctx context.Context, userID uuid.UUID, provider string) (models.AuthIdentity, error) {
	q := r.querier(ctx)
	row, err := q.GetAuthIdentityByUserIDAndProvider(ctx, db.GetAuthIdentityByUserIDAndProviderParams{
		UserID:   userID,
		Provider: provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		}
		return models.AuthIdentity{}, fmt.Errorf("getting auth identity by user and provider: %w", err)
	}
	return dbAuthIdentityToDomain(row), nil
}

// UpdatePasswordHash replaces password_hash on the password identity for the
// given user. Any database error is wrapped with context.
func (r *authIdentityRepository) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, newHash string) error {
	q := r.querier(ctx)
	if err := q.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{
		UserID:       userID,
		PasswordHash: pgtype.Text{String: newHash, Valid: true},
	}); err != nil {
		return fmt.Errorf("updating password hash: %w", err)
	}
	return nil
}

// dbAuthIdentityToDomain maps a sqlc db.AuthIdentity row to a domain
// models.AuthIdentity, converting the nullable PasswordHash pgtype field
// into a nil-able pointer.
func dbAuthIdentityToDomain(row db.AuthIdentity) models.AuthIdentity {
	identity := models.AuthIdentity{
		ID:              row.ID,
		UserID:          row.UserID,
		Provider:        row.Provider,
		ProviderSubject: row.ProviderSubject,
	}
	if row.PasswordHash.Valid {
		identity.PasswordHash = &row.PasswordHash.String
	}
	return identity
}
