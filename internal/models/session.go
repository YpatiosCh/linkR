package models

import (
	"time"

	"github.com/google/uuid"
)

// Session represents an authenticated session that supports refresh-token
// rotation. RefreshTokenHash stores the hash of the current refresh token,
// TokenFamilyID ties rotated tokens back to the same original login, and
// ExpiresAt/RevokedAt determine when the session stops being valid.
type Session struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
	TokenFamilyID    uuid.UUID
	CreatedAt        time.Time
	ExpiresAt        time.Time
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
	IPAddress        *string
	UserAgent        *string
}
