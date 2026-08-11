package repository

import (
	"context"
	"fmt"
	db "linkMe/internal/db/generated"
	"linkMe/internal/models"

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
