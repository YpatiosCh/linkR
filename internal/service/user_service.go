package service

import (
	"context"
	"errors"
	"fmt"
	"linkMe/internal/models"
	"linkMe/internal/msgs"
	"linkMe/internal/repository"
	"linkMe/internal/utils/validate"
	"linkMe/pkg/hash"

	"github.com/google/uuid"
)

// userService implements UserService by combining the embedded shared
// repository with the business rules for the authenticated current-user
// resource.
type userService struct {
	repository.Repository
}

// NewUserService builds a userService backed by the given repositories,
// embedding the repository so profile flows can reach all entity
// repositories and WithinTx directly.
func NewUserService(repos repository.Repository) *userService {
	return &userService{Repository: repos}
}

// GetMe returns the user identified by userID together with their active
// subscription. It returns msgs.ErrUserNotFound when no user matches and
// msgs.ErrSubscriptionNotFound when the user has no active subscription.
func (s *userService) GetMe(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, error) {
	user, err := s.User().GetUserByID(ctx, userID)
	if err != nil {
		return models.User{}, models.Subscription{}, err
	}
	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, userID)
	if err != nil {
		return models.User{}, models.Subscription{}, err
	}
	return user, sub, nil
}

// ChangePassword verifies the caller's current password, replaces it with a
// hash of newPassword, and revokes all other active sessions so that any stolen
// refresh tokens are immediately invalidated. The current session (identified
// by sessionID) is kept alive so the caller does not need to re-authenticate.
//
// Error cases:
//   - msgs.ErrPasswordNotSet — account has no password identity (OAuth-only)
//   - msgs.ErrInvalidCredentials — currentPassword does not match the stored hash
//   - msgs.ErrInvalidCredentials — newPassword fails validation
func (s *userService) ChangePassword(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error {
	if !validate.Password(newPassword) {
		return msgs.ErrInvalidCredentials
	}

	identity, err := s.AuthIdentity().GetAuthIdentityByUserIDAndProvider(ctx, userID, "password")
	if err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return msgs.ErrPasswordNotSet
		}
		return err
	}

	if identity.PasswordHash == nil {
		return msgs.ErrPasswordNotSet
	}

	ok, err := hash.VerifyPassword(currentPassword, *identity.PasswordHash)
	if err != nil {
		return fmt.Errorf("verifying current password: %w", err)
	}
	if !ok {
		return msgs.ErrInvalidCredentials
	}

	newHash, err := hash.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hashing new password: %w", err)
	}

	return s.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.AuthIdentity().UpdatePasswordHash(ctx, userID, newHash); err != nil {
			return err
		}
		return s.Session().RevokeOtherSessionsForUser(ctx, userID, sessionID)
	})
}
