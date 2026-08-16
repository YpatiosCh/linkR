package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents a registered application user.
// It is the domain model shared across the repository, service, and handler
// layers. Name, AvatarURL, CompanyName, Description, and EmailVerifiedAt are
// pointers because those fields are optional and nil until set — profile
// details are filled in as a second phase, after registration, via
// UserService.UpdateProfile. SocialLinks is never nil; its zero value means
// no links have been set. DeletedAt is nil for every normal lookup (they all
// filter WHERE deleted_at IS NULL) — it's only ever non-nil on a row
// returned by UserRepository.GetUserByEmailIncludingDeleted, used by
// Register to detect and reactivate a previously soft-deleted account.
type User struct {
	ID              uuid.UUID
	Email           string
	Name            *string
	AvatarURL       *string
	CompanyName     *string
	Description     *string
	SocialLinks     SocialLinks
	EmailVerifiedAt *time.Time
	DeletedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// RegisterInput carries the credentials submitted when creating a new account.
// The plain-text Password is used only during registration and is never stored
// on the User model. Registration intentionally collects only Email and
// Password — profile details (name, avatar, etc.) are set afterward via
// UserService.UpdateProfile.
type RegisterInput struct {
	Email    string
	Password string
}

// UpdateProfileInput carries a partial update to a user's profile — every
// field is a pointer so that a nil field means "leave unchanged" and only
// the fields the caller actually provided are applied. SocialLinks, when
// non-nil, replaces the entire SocialLinks struct rather than merging.
type UpdateProfileInput struct {
	Name        *string
	AvatarURL   *string
	CompanyName *string
	Description *string
	SocialLinks *SocialLinks
}

// LoginInput carries the credentials submitted when authenticating with an
// existing email/password account. The plain-text Password is used only to
// verify against the stored hash and is never stored on the User model.
type LoginInput struct {
	Email    string
	Password string
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
