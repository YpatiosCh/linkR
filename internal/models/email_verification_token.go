package models

import (
	"time"

	"github.com/google/uuid"
)

// EmailVerificationToken is a single-use, short-lived token proving
// ownership of a user's email address. TokenHash stores the SHA-256 hash of
// the opaque token delivered by email — the raw token is never persisted.
// UsedAt is nil until the token is consumed by a successful verification.
type EmailVerificationToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}
