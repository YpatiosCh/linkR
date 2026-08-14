package redis

import (
	"context"
	"errors"
	"fmt"
	"linkMe/internal/utils/jwttoken"

	goredis "github.com/redis/go-redis/v9"

	"github.com/google/uuid"
)

// revokedSessionKeyPrefix namespaces session-revocation keys in the shared
// Redis instance.
const revokedSessionKeyPrefix = "revoked-session:"

// SessionRevocationStore tracks revoked sessions in Redis so RequireAuth can
// reject an access token immediately after logout, without a lookup against
// the primary database on every authenticated request.
type SessionRevocationStore struct {
	client *Client
}

// NewSessionRevocationStore builds a SessionRevocationStore backed by client.
func NewSessionRevocationStore(client *Client) *SessionRevocationStore {
	return &SessionRevocationStore{client: client}
}

// IsSessionRevoked reports whether sessionID has been revoked.
func (s *SessionRevocationStore) IsSessionRevoked(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	n, err := s.client.Exists(ctx, revokedSessionKey(sessionID)).Result()
	if err != nil {
		return false, fmt.Errorf("checking session revocation: %w", err)
	}
	return n > 0, nil
}

// RevokeSession marks sessionID as revoked. The key expires after
// jwttoken.AccessTokenDuration — the longest an access token referencing
// this session could still be valid — so Redis self-cleans and no manual
// eviction is needed.
func (s *SessionRevocationStore) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.client.Set(ctx, revokedSessionKey(sessionID), "1", jwttoken.AccessTokenDuration).Err(); err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	return nil
}

// RevokeSessions marks every session in sessionIDs as revoked, in a single
// pipelined round trip.
func (s *SessionRevocationStore) RevokeSessions(ctx context.Context, sessionIDs []uuid.UUID) error {
	if len(sessionIDs) == 0 {
		return nil
	}

	pipe := s.client.Pipeline()
	for _, id := range sessionIDs {
		pipe.Set(ctx, revokedSessionKey(id), "1", jwttoken.AccessTokenDuration)
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, goredis.Nil) {
		return fmt.Errorf("revoking sessions: %w", err)
	}
	return nil
}

func revokedSessionKey(sessionID uuid.UUID) string {
	return revokedSessionKeyPrefix + sessionID.String()
}
