package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"linkMe/internal/models"
	"linkMe/internal/service"
	"linkMe/internal/utils/logctx"
	"linkMe/internal/utils/reqctx"

	"github.com/google/uuid"
)

// fakeAuditEventRepo is a spy repository.AuditEventRepository that records
// the last event passed to CreateAuditEvent and optionally injects an error.
type fakeAuditEventRepo struct {
	lastEvent models.AuditEvent
	calls     int
	err       error
}

func (f *fakeAuditEventRepo) CreateAuditEvent(ctx context.Context, e models.AuditEvent) (models.AuditEvent, error) {
	f.calls++
	f.lastEvent = e
	if f.err != nil {
		return models.AuditEvent{}, f.err
	}
	return e, nil
}

func TestAuditService_Record_PopulatesMetaFromContext(t *testing.T) {
	repo := &fakeAuditEventRepo{}
	svc := service.NewAuditService(repo)

	userID := uuid.New()
	ctx := reqctx.WithMeta(context.Background(), reqctx.Meta{
		IP:        "203.0.113.1",
		UserAgent: "test-agent/1.0",
		RequestID: "req-123",
	})

	svc.Record(ctx, models.AuditLoginSucceeded, &userID, map[string]any{"foo": "bar"})

	if repo.calls != 1 {
		t.Fatalf("expected exactly 1 CreateAuditEvent call, got %d", repo.calls)
	}
	e := repo.lastEvent
	if e.EventType != models.AuditLoginSucceeded {
		t.Errorf("event type: got %v, want %v", e.EventType, models.AuditLoginSucceeded)
	}
	if e.UserID == nil || *e.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, e.UserID)
	}
	if e.IPAddress == nil || *e.IPAddress != "203.0.113.1" {
		t.Errorf("expected IPAddress 203.0.113.1, got %v", e.IPAddress)
	}
	if e.UserAgent == nil || *e.UserAgent != "test-agent/1.0" {
		t.Errorf("expected UserAgent test-agent/1.0, got %v", e.UserAgent)
	}
	if e.Metadata["foo"] != "bar" {
		t.Errorf("expected metadata to preserve caller-supplied fields, got %v", e.Metadata)
	}
	if e.Metadata["request_id"] != "req-123" {
		t.Errorf("expected metadata to be merged with request_id, got %v", e.Metadata)
	}
}

func TestAuditService_Record_NilUserIDAndMetadata(t *testing.T) {
	repo := &fakeAuditEventRepo{}
	svc := service.NewAuditService(repo)

	svc.Record(context.Background(), models.AuditLoginFailed, nil, nil)

	if repo.calls != 1 {
		t.Fatalf("expected exactly 1 CreateAuditEvent call, got %d", repo.calls)
	}
	e := repo.lastEvent
	if e.UserID != nil {
		t.Errorf("expected nil UserID, got %v", e.UserID)
	}
	if e.IPAddress != nil {
		t.Errorf("expected nil IPAddress outside any injected reqctx, got %v", e.IPAddress)
	}
	// No request ID in context and no caller metadata: nothing to merge.
	if e.Metadata != nil {
		t.Errorf("expected nil metadata, got %v", e.Metadata)
	}
}

func TestAuditService_Record_RepoErrorIsLoggedAndSwallowed(t *testing.T) {
	repo := &fakeAuditEventRepo{err: errors.New("db unavailable")}
	svc := service.NewAuditService(repo)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := logctx.WithLogger(context.Background(), logger)

	// Must not panic and must not surface the error to the caller — Record
	// has no error return, so this call succeeding at all is the assertion
	// that a repo failure never blocks/fails the audited action.
	svc.Record(ctx, models.AuditAccountDeleted, nil, nil)

	if repo.calls != 1 {
		t.Fatalf("expected exactly 1 CreateAuditEvent call, got %d", repo.calls)
	}

	var logLine map[string]any
	logStr := strings.TrimSpace(buf.String())
	if logStr == "" {
		t.Fatal("expected a log line for the swallowed repo error")
	}
	if err := json.Unmarshal([]byte(logStr), &logLine); err != nil {
		t.Fatalf("invalid JSON log line %q: %v", logStr, err)
	}
	if logLine["level"] != "ERROR" {
		t.Errorf("expected ERROR level log, got %v", logLine["level"])
	}
}
