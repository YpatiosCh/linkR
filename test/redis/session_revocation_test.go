package redis_test

import (
	"context"
	"testing"

	"linkMe/internal/redis"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
)

func newTestStore(t *testing.T) *redis.SessionRevocationStore {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(mr.Addr())
	return redis.NewSessionRevocationStore(client)
}

func TestSessionRevocationStore_NotRevokedByDefault(t *testing.T) {
	store := newTestStore(t)

	revoked, err := store.IsSessionRevoked(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if revoked {
		t.Error("a session that was never revoked should report revoked=false")
	}
}

func TestSessionRevocationStore_RevokeSession(t *testing.T) {
	store := newTestStore(t)
	sessionID := uuid.New()

	if err := store.RevokeSession(context.Background(), sessionID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	revoked, err := store.IsSessionRevoked(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !revoked {
		t.Error("expected the session to report revoked=true after RevokeSession")
	}

	// A different, never-revoked session must be unaffected.
	other, err := store.IsSessionRevoked(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if other {
		t.Error("an unrelated session must not be reported as revoked")
	}
}

func TestSessionRevocationStore_RevokeSessions(t *testing.T) {
	store := newTestStore(t)
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

	if err := store.RevokeSessions(context.Background(), ids); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for _, id := range ids {
		revoked, err := store.IsSessionRevoked(context.Background(), id)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !revoked {
			t.Errorf("expected session %s to report revoked=true", id)
		}
	}
}

func TestSessionRevocationStore_RevokeSessionsEmpty(t *testing.T) {
	store := newTestStore(t)

	// Must not error or panic on an empty slice (e.g. RevokeAllSessionsForUser
	// found nothing to revoke).
	if err := store.RevokeSessions(context.Background(), nil); err != nil {
		t.Fatalf("expected no error for an empty slice, got %v", err)
	}
}
