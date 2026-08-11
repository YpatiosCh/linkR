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

// userRepository implements UserRepository on top of sqlc-generated
// queries. It holds the base *db.Queries used outside a transaction.
type userRepository struct {
	queries *db.Queries
}

// NewUserRepository creates a userRepository bound to dbtx, which is
// either the connection pool or an active transaction.
func NewUserRepository(dbtx db.DBTX) *userRepository {
	return &userRepository{queries: db.New(dbtx)}
}

// querier returns db.Queries bound to the active transaction when ctx
// carries one (see injectTx), otherwise it falls back to the repository's
// base queries so calls participate in an ongoing transaction.
func (r *userRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := extractTx(ctx); ok {
		return db.New(tx)
	}
	return r.queries
}

// CreateUser inserts a new user via the CreateUser query and maps the
// returned row to a domain models.User. A nil AvatarURL is stored as a
// SQL NULL. Any database error is wrapped with context and returned.
func (r *userRepository) CreateUser(ctx context.Context, u models.User) (models.User, error) {
	q := r.querier(ctx)
	var avatarURL pgtype.Text
	if u.AvatarURL != nil {
		avatarURL = pgtype.Text{String: *u.AvatarURL, Valid: true}
	}

	row, err := q.CreateUser(ctx, db.CreateUserParams{
		ID:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		AvatarUrl: avatarURL,
	})
	if err != nil {
		return models.User{}, fmt.Errorf("error creating user: %w", err)
	}

	return dbUserToDomain(row), nil
}

// GetUserByEmail runs the GetUserByEmail query (SELECT ... WHERE
// email = $1 AND deleted_at IS NULL). pgx.ErrNoRows is translated to
// msgs.ErrUserNotFound; other errors are wrapped and returned.
func (r *userRepository) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	q := r.querier(ctx)
	row, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.User{}, msgs.ErrUserNotFound
		}
		return models.User{}, fmt.Errorf("error getting user by email: %w", err)
	}
	return dbUserToDomain(row), nil
}

// GetUserByID runs the GetUserByID query (SELECT ... WHERE id = $1 AND
// deleted_at IS NULL). pgx.ErrNoRows is translated to
// msgs.ErrUserNotFound; other errors are wrapped and returned.
func (r *userRepository) GetUserByID(ctx context.Context, id uuid.UUID) (models.User, error) {
	q := r.querier(ctx)
	row, err := q.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.User{}, msgs.ErrUserNotFound
		}
		return models.User{}, fmt.Errorf("error getting user by ID: %w", err)
	}
	return dbUserToDomain(row), nil
}

// dbUserToDomain maps a sqlc db.User row to a domain models.User,
// converting nullable pgtype fields (AvatarUrl, EmailVerifiedAt) into
// nil-able pointers.
func dbUserToDomain(row db.User) models.User {
	u := models.User{
		ID:        row.ID,
		Email:     row.Email,
		Name:      row.Name,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if row.AvatarUrl.Valid {
		u.AvatarURL = &row.AvatarUrl.String
	}
	if row.EmailVerifiedAt.Valid {
		u.EmailVerifiedAt = &row.EmailVerifiedAt.Time
	}

	return u
}
