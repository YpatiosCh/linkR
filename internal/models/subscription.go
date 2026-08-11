package models

import (
	"time"

	"github.com/google/uuid"
)

// Subscription represents a user's plan subscription and its billing period.
// PlanID references the subscribed tier, Status tracks the subscription
// state, and EndsAt is nil while the subscription is ongoing.
type Subscription struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	PlanID    string
	Status    string
	StartedAt time.Time
	EndsAt    *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}
