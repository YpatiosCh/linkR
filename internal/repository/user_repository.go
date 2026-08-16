package repository

import (
	"context"
	"encoding/json"
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
// returned row to a domain models.User. A nil Name or AvatarURL is stored
// as a SQL NULL — registration intentionally leaves Name unset; it's filled
// in later via UpdateProfile. Any database error is wrapped with context
// and returned.
func (r *userRepository) CreateUser(ctx context.Context, u models.User) (models.User, error) {
	q := r.querier(ctx)

	row, err := q.CreateUser(ctx, db.CreateUserParams{
		ID:        u.ID,
		Email:     u.Email,
		Name:      textOrNull(u.Name),
		AvatarUrl: textOrNull(u.AvatarURL),
	})
	if err != nil {
		return models.User{}, fmt.Errorf("error creating user: %w", err)
	}

	return dbUserToDomain(row)
}

// UpdateProfile applies a partial update to the user's profile via the
// UpdateUserProfile query: a nil field in input leaves the corresponding
// column unchanged (COALESCE against a SQL NULL parameter); a non-nil field
// replaces it, including a non-nil pointer to an empty string clearing a
// text field. pgx.ErrNoRows (the user doesn't exist, or is soft-deleted) is
// translated to msgs.ErrUserNotFound; other errors are wrapped and
// returned.
func (r *userRepository) UpdateProfile(ctx context.Context, userID uuid.UUID, input models.UpdateProfileInput) (models.User, error) {
	q := r.querier(ctx)

	var socialLinks []byte
	if input.SocialLinks != nil {
		var err error
		socialLinks, err = json.Marshal(input.SocialLinks)
		if err != nil {
			return models.User{}, fmt.Errorf("marshaling social links: %w", err)
		}
	}

	row, err := q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
		ID:          userID,
		Name:        textOrNull(input.Name),
		AvatarUrl:   textOrNull(input.AvatarURL),
		CompanyName: textOrNull(input.CompanyName),
		Description: textOrNull(input.Description),
		SocialLinks: socialLinks,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.User{}, msgs.ErrUserNotFound
		}
		return models.User{}, fmt.Errorf("error updating user profile: %w", err)
	}

	return dbUserToDomain(row)
}

// textOrNull converts an optional string pointer to pgtype.Text: nil
// becomes a SQL NULL (leave unchanged, under UpdateUserProfile's COALESCE
// pattern, or simply absent on insert), a non-nil pointer (including one to
// an empty string) becomes a valid value.
func textOrNull(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
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
	return dbUserToDomain(row)
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
	return dbUserToDomain(row)
}

// UpdateEmailVerifiedAt sets email_verified_at = now() on the user with the
// given ID, marking their email as verified. Any database error is wrapped
// with context.
func (r *userRepository) UpdateEmailVerifiedAt(ctx context.Context, id uuid.UUID) error {
	q := r.querier(ctx)
	if err := q.UpdateEmailVerifiedAt(ctx, id); err != nil {
		return fmt.Errorf("updating email verified at: %w", err)
	}
	return nil
}

// GetUserByEmailIncludingDeleted runs the GetUserByEmailIncludingDeleted
// query (SELECT ... WHERE email = $1, no deleted_at filter) — unlike every
// other read in this repository, it returns a soft-deleted row too, so
// callers can distinguish "no account with this email" from "an account
// with this email exists but was deleted." Used only by Register, to
// detect and offer to reactivate a previously deleted account. pgx.ErrNoRows
// is translated to msgs.ErrUserNotFound; other errors are wrapped and
// returned.
func (r *userRepository) GetUserByEmailIncludingDeleted(ctx context.Context, email string) (models.User, error) {
	q := r.querier(ctx)
	row, err := q.GetUserByEmailIncludingDeleted(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.User{}, msgs.ErrUserNotFound
		}
		return models.User{}, fmt.Errorf("error getting user by email including deleted: %w", err)
	}
	return dbUserToDomain(row)
}

// SoftDelete sets deleted_at = now() on the user with the given ID. Any
// database error is wrapped with context.
func (r *userRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	q := r.querier(ctx)
	if err := q.SoftDeleteUser(ctx, id); err != nil {
		return fmt.Errorf("soft-deleting user: %w", err)
	}
	return nil
}

// Reactivate clears deleted_at on the user with the given ID. Any database
// error is wrapped with context.
func (r *userRepository) Reactivate(ctx context.Context, id uuid.UUID) error {
	q := r.querier(ctx)
	if err := q.ReactivateUser(ctx, id); err != nil {
		return fmt.Errorf("reactivating user: %w", err)
	}
	return nil
}

// dbUserToDomain maps a sqlc db.User row to a domain models.User,
// converting nullable pgtype fields (Name, AvatarUrl, CompanyName,
// Description, EmailVerifiedAt) into nil-able pointers, and unmarshaling
// the SocialLinks JSONB column into models.SocialLinks (its zero value if
// the column is empty or absent). An unmarshal failure can only mean the
// stored JSON is corrupt (this repository is the only writer, and it
// always writes via json.Marshal), so it's surfaced as an error rather than
// silently discarded.
func dbUserToDomain(row db.User) (models.User, error) {
	u := models.User{
		ID:        row.ID,
		Email:     row.Email,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if row.Name.Valid {
		u.Name = &row.Name.String
	}
	if row.AvatarUrl.Valid {
		u.AvatarURL = &row.AvatarUrl.String
	}
	if row.CompanyName.Valid {
		u.CompanyName = &row.CompanyName.String
	}
	if row.Description.Valid {
		u.Description = &row.Description.String
	}
	if row.EmailVerifiedAt.Valid {
		u.EmailVerifiedAt = &row.EmailVerifiedAt.Time
	}
	if row.DeletedAt.Valid {
		u.DeletedAt = &row.DeletedAt.Time
	}
	if len(row.SocialLinks) > 0 {
		if err := json.Unmarshal(row.SocialLinks, &u.SocialLinks); err != nil {
			return models.User{}, fmt.Errorf("unmarshaling social links: %w", err)
		}
	}

	return u, nil
}
