package repository

import (
	"context"
	"encoding/json"
	"fmt"
	db "linkMe/internal/db/generated"
	"linkMe/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// auditEventRepository implements AuditEventRepository on top of
// sqlc-generated queries. It holds the base *db.Queries used outside a
// transaction.
type auditEventRepository struct {
	queries *db.Queries
}

// NewAuditEventRepository creates an auditEventRepository bound to dbtx,
// which is either the connection pool or an active transaction.
func NewAuditEventRepository(dbtx db.DBTX) *auditEventRepository {
	return &auditEventRepository{queries: db.New(dbtx)}
}

// querier returns db.Queries bound to the active transaction when ctx
// carries one (see injectTx), otherwise it falls back to the repository's
// base queries so calls participate in an ongoing transaction.
func (r *auditEventRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := extractTx(ctx); ok {
		return db.New(tx)
	}
	return r.queries
}

// CreateAuditEvent inserts a new audit event via the CreateAuditEvent
// query and maps the returned row to a domain models.AuditEvent. A nil
// UserID/IPAddress/UserAgent is stored as a SQL NULL; a nil Metadata is
// stored as a SQL NULL rather than an empty JSON object. Any database
// error is wrapped with context and returned.
func (r *auditEventRepository) CreateAuditEvent(ctx context.Context, e models.AuditEvent) (models.AuditEvent, error) {
	q := r.querier(ctx)

	var metadataJSON []byte
	if e.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(e.Metadata)
		if err != nil {
			return models.AuditEvent{}, fmt.Errorf("marshaling audit event metadata: %w", err)
		}
	}

	row, err := q.CreateAuditEvent(ctx, db.CreateAuditEventParams{
		ID:        e.ID,
		UserID:    uuidOrNull(e.UserID),
		EventType: string(e.EventType),
		IpAddress: textOrNull(e.IPAddress),
		UserAgent: textOrNull(e.UserAgent),
		Metadata:  metadataJSON,
	})
	if err != nil {
		return models.AuditEvent{}, fmt.Errorf("creating audit event: %w", err)
	}
	return dbAuditEventToDomain(row)
}

// uuidOrNull converts an optional uuid.UUID pointer to pgtype.UUID: nil
// becomes a SQL NULL, a non-nil pointer becomes a valid value.
func uuidOrNull(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

// dbAuditEventToDomain maps a sqlc db.AuditEvent row to a domain
// models.AuditEvent, converting nullable pgtype fields (UserID, IpAddress,
// UserAgent) into nil-able pointers and unmarshaling the Metadata JSONB
// column back into a map. An unmarshal failure can only mean the stored
// JSON is corrupt (this repository is the only writer, and it always
// writes via json.Marshal), so it's surfaced as an error rather than
// silently discarded.
func dbAuditEventToDomain(row db.AuditEvent) (models.AuditEvent, error) {
	e := models.AuditEvent{
		ID:        row.ID,
		EventType: models.AuditEventType(row.EventType),
		CreatedAt: row.CreatedAt.Time,
	}
	if row.UserID.Valid {
		id := uuid.UUID(row.UserID.Bytes)
		e.UserID = &id
	}
	if row.IpAddress.Valid {
		e.IPAddress = &row.IpAddress.String
	}
	if row.UserAgent.Valid {
		e.UserAgent = &row.UserAgent.String
	}
	if len(row.Metadata) > 0 {
		if err := json.Unmarshal(row.Metadata, &e.Metadata); err != nil {
			return models.AuditEvent{}, fmt.Errorf("unmarshaling audit event metadata: %w", err)
		}
	}
	return e, nil
}
