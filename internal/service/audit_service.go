package service

import (
	"context"
	"linkMe/internal/models"
	"linkMe/internal/repository"
	"linkMe/internal/utils/logctx"
	"linkMe/internal/utils/reqctx"

	"github.com/google/uuid"
)

// auditService implements AuditRecorder on top of the audit event
// repository. It takes only the one repository it needs, like
// emailService, rather than embedding the full repository.Repository
// aggregate.
type auditService struct {
	repo repository.AuditEventRepository
}

// NewAuditService builds an auditService backed by repo.
func NewAuditService(repo repository.AuditEventRepository) *auditService {
	return &auditService{repo: repo}
}

// Record implements AuditRecorder. It reads IP/User-Agent/request ID from
// ctx (see internal/utils/reqctx) and merges the request ID into metadata
// as "request_id" (only when non-empty, and only if the caller hasn't
// already set that key) so every row correlates with its structured log
// line. A write failure is logged at Error via the request-scoped logger
// and swallowed — see the AuditRecorder doc comment and
// ARCHITECTURE_AND_RULES.md's O1 exception for this file: audit-write
// failures never reach the HTTP boundary (Record never returns an error to
// propagate), so this is the one place they can be logged instead of
// silently lost, without ever blocking or failing the action being
// audited.
func (s *auditService) Record(ctx context.Context, eventType models.AuditEventType, userID *uuid.UUID, metadata map[string]any) {
	meta := reqctx.FromContext(ctx)

	merged := make(map[string]any, len(metadata)+1)
	for k, v := range metadata {
		merged[k] = v
	}
	if meta.RequestID != "" {
		if _, ok := merged["request_id"]; !ok {
			merged["request_id"] = meta.RequestID
		}
	}
	if len(merged) == 0 {
		merged = nil
	}

	event := models.AuditEvent{
		ID:        uuid.New(),
		UserID:    userID,
		EventType: eventType,
		Metadata:  merged,
	}
	if meta.IP != "" {
		event.IPAddress = &meta.IP
	}
	if meta.UserAgent != "" {
		event.UserAgent = &meta.UserAgent
	}

	if _, err := s.repo.CreateAuditEvent(ctx, event); err != nil {
		logctx.FromContext(ctx).Error("audit event write failed", "event_type", string(eventType), "error", err)
	}
}
