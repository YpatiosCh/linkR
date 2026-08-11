package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a registered application user.
// It is the domain model shared across the repository, service, and handler
// layers. AvatarURL and EmailVerifiedAt are pointers because those fields are
// optional and nil until set.
type User struct {
	ID              uuid.UUID
	Email           string
	Name            string
	AvatarURL       *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// RegisterInput carries the credentials submitted when creating a new account.
// The plain-text Password is used only during registration and is never stored
// on the User model.
type RegisterInput struct {
	Email    string
	Password string
	Name     string
}

// AuthIdentity binds a user to an authentication credential. PasswordHash is
// set only for email/password identities and is nil for accounts that sign in
// through an external provider.
type AuthIdentity struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	Provider        string
	ProviderSubject string
	PasswordHash    *string
}
