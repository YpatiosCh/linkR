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

// sessionRepository implements SessionRepository on top of sqlc-generated
// queries. It holds the base *db.Queries used outside a transaction.
type sessionRepository struct {
	queries *db.Queries
}

// NewSessionRepository creates a sessionRepository bound to dbtx, which is
// either the connection pool or an active transaction.
func NewSessionRepository(dbtx db.DBTX) *sessionRepository {
	return &sessionRepository{queries: db.New(dbtx)}
}

// querier returns db.Queries bound to the active transaction when ctx
// carries one (see injectTx), otherwise it falls back to the repository's
// base queries so calls participate in an ongoing transaction.
func (r *sessionRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := extractTx(ctx); ok {
		return db.New(tx)
	}
	return r.queries
}

// CreateSession inserts a new session via the CreateSession query and maps
// the returned row to a domain models.Session. Nil IPAddress and
// UserAgent are stored as SQL NULL. Any database error is wrapped with
// context and returned.
func (r *sessionRepository) CreateSession(ctx context.Context, s models.Session) (models.Session, error) {
	q := r.querier(ctx)

	var ip, ua pgtype.Text
	if s.IPAddress != nil {
		ip = pgtype.Text{String: *s.IPAddress, Valid: true}
	}
	if s.UserAgent != nil {
		ua = pgtype.Text{String: *s.UserAgent, Valid: true}
	}

	row, err := q.CreateSession(ctx, db.CreateSessionParams{
		ID:               s.ID,
		UserID:           s.UserID,
		RefreshTokenHash: s.RefreshTokenHash,
		TokenFamilyID:    s.TokenFamilyID,
		ExpiresAt:        pgtype.Timestamptz{Time: s.ExpiresAt, Valid: true},
		IpAddress:        ip,
		UserAgent:        ua,
	})
	if err != nil {
		return models.Session{}, fmt.Errorf("error creating session: %w", err)
	}

	return dbSessionToDomain(row), nil
}

// GetSessionByTokenHash returns the session whose refresh_token_hash matches
// the given hash, including revoked sessions so that callers can detect token
// reuse. pgx.ErrNoRows is translated to msgs.ErrTokenInvalid; other errors
// are wrapped with context.
func (r *sessionRepository) GetSessionByTokenHash(ctx context.Context, tokenHash string) (models.Session, error) {
	q := r.querier(ctx)
	row, err := q.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Session{}, msgs.ErrTokenInvalid
		}
		return models.Session{}, fmt.Errorf("getting session by token hash: %w", err)
	}
	return dbSessionToDomain(row), nil
}

// MarkSessionConsumed sets revoked_at = now() on the session with the given
// ID, recording that it has been rotated and must not be accepted again.
// Any database error is wrapped with context.
func (r *sessionRepository) MarkSessionConsumed(ctx context.Context, id uuid.UUID) error {
	q := r.querier(ctx)
	if err := q.MarkSessionConsumed(ctx, id); err != nil {
		return fmt.Errorf("marking session consumed: %w", err)
	}
	return nil
}

// RevokeSessionFamily sets revoked_at = now() on every non-revoked session
// belonging to the given token family. Used when a consumed refresh token is
// reused, signalling a possible token theft. Returns the IDs of the revoked
// sessions. Any database error is wrapped.
func (r *sessionRepository) RevokeSessionFamily(ctx context.Context, familyID uuid.UUID) ([]uuid.UUID, error) {
	q := r.querier(ctx)
	ids, err := q.RevokeSessionFamily(ctx, familyID)
	if err != nil {
		return nil, fmt.Errorf("revoking session family: %w", err)
	}
	return ids, nil
}

// RevokeSession sets revoked_at = now() on the session with the given ID,
// invalidating it for all future use. Idempotent — safe to call on an
// already-revoked session. Used during explicit logout.
func (r *sessionRepository) RevokeSession(ctx context.Context, id uuid.UUID) error {
	q := r.querier(ctx)
	if err := q.RevokeSession(ctx, id); err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	return nil
}

// RevokeAllSessionsForUser sets revoked_at = now() on every non-revoked
// session belonging to the given user, used during logout-all. Returns the
// IDs of the revoked sessions.
func (r *sessionRepository) RevokeAllSessionsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	q := r.querier(ctx)
	ids, err := q.RevokeAllSessionsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("revoking all sessions for user: %w", err)
	}
	return ids, nil
}

// RevokeOtherSessionsForUser sets revoked_at = now() on every non-revoked
// session belonging to the given user except keepSessionID, used during
// password change to keep the current session alive. Returns the IDs of the
// revoked sessions.
func (r *sessionRepository) RevokeOtherSessionsForUser(ctx context.Context, userID uuid.UUID, keepSessionID uuid.UUID) ([]uuid.UUID, error) {
	q := r.querier(ctx)
	ids, err := q.RevokeOtherSessionsForUser(ctx, db.RevokeOtherSessionsForUserParams{
		UserID: userID,
		ID:     keepSessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("revoking other sessions for user: %w", err)
	}
	return ids, nil
}

// dbSessionToDomain maps a sqlc db.Session row to a domain models.Session,
// converting nullable pgtype fields (LastUsedAt, RevokedAt, IpAddress,
// UserAgent) into nil-able pointers.
func dbSessionToDomain(row db.Session) models.Session {
	s := models.Session{
		ID:               row.ID,
		UserID:           row.UserID,
		RefreshTokenHash: row.RefreshTokenHash,
		TokenFamilyID:    row.TokenFamilyID,
		CreatedAt:        row.CreatedAt.Time,
		ExpiresAt:        row.ExpiresAt.Time,
	}
	if row.LastUsedAt.Valid {
		s.LastUsedAt = &row.LastUsedAt.Time
	}
	if row.RevokedAt.Valid {
		s.RevokedAt = &row.RevokedAt.Time
	}
	if row.IpAddress.Valid {
		s.IPAddress = &row.IpAddress.String
	}
	if row.UserAgent.Valid {
		s.UserAgent = &row.UserAgent.String
	}
	return s
}
