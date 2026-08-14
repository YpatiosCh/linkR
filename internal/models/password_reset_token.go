package models

import (
	"time"

	"github.com/google/uuid"
)

// PasswordResetToken is a single-use, short-lived token authorizing a
// password reset. TokenHash stores the SHA-256 hash of the opaque token
// delivered by email — the raw token is never persisted. UsedAt is nil until
// the token is consumed by a successful reset.
type PasswordResetToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}
