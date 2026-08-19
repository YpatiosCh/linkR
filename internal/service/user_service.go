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
	sessions SessionRevoker
	audit    AuditRecorder
}

// NewUserService builds a userService backed by the given repositories,
// session revoker, and audit recorder, embedding the repository so profile
// flows can reach all entity repositories and WithinTx directly.
func NewUserService(repos repository.Repository, sessions SessionRevoker, audit AuditRecorder) *userService {
	return &userService{Repository: repos, sessions: sessions, audit: audit}
}

// GetMe returns the user identified by userID together with their active
// subscription and whether they have a password identity (false for an
// OAuth-only account). It returns msgs.ErrUserNotFound when no user matches
// and msgs.ErrSubscriptionNotFound when the user has no active subscription.
func (s *userService) GetMe(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, bool, error) {
	user, err := s.User().GetUserByID(ctx, userID)
	if err != nil {
		return models.User{}, models.Subscription{}, false, err
	}
	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, userID)
	if err != nil {
		return models.User{}, models.Subscription{}, false, err
	}
	hasPassword, err := hasPasswordIdentity(ctx, s.Repository, userID)
	if err != nil {
		return models.User{}, models.Subscription{}, false, err
	}
	return user, sub, hasPassword, nil
}

// hasPasswordIdentity reports whether userID has a password auth identity.
// Shared by UserService.GetMe and AuthService.VerifyEmail, both of which
// feed the same MeResponse shape.
func hasPasswordIdentity(ctx context.Context, repos repository.Repository, userID uuid.UUID) (bool, error) {
	_, err := repos.AuthIdentity().GetAuthIdentityByUserIDAndProvider(ctx, userID, "password")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, msgs.ErrUserNotFound) {
		return false, nil
	}
	return false, err
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

	var revokedSessionIDs []uuid.UUID
	err = s.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.AuthIdentity().UpdatePasswordHash(ctx, userID, newHash); err != nil {
			return err
		}
		ids, err := s.Session().RevokeOtherSessionsForUser(ctx, userID, sessionID)
		if err != nil {
			return err
		}
		revokedSessionIDs = ids
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.sessions.RevokeSessions(ctx, revokedSessionIDs); err != nil {
		return err
	}
	s.audit.Record(ctx, models.AuditPasswordChanged, &userID, nil)
	return nil
}

// UpdateProfile applies a partial update to the caller's profile: each
// field in input that is non-nil is validated and replaces the current
// value; nil fields are left unchanged. SocialLinks, when non-nil, replaces
// the entire SocialLinks struct (its Platforms keys must all be known
// social platforms, and every URL — avatar, platform link, or custom link —
// must be a valid http(s) URL). All validation failures return
// msgs.ErrInvalidInput.
func (s *userService) UpdateProfile(ctx context.Context, userID uuid.UUID, input models.UpdateProfileInput) (models.User, error) {
	if input.Name != nil && !validate.Name(*input.Name) {
		return models.User{}, msgs.ErrInvalidInput
	}
	if input.AvatarURL != nil && !validate.URL(*input.AvatarURL) {
		return models.User{}, msgs.ErrInvalidInput
	}
	if input.CompanyName != nil && !validate.CompanyName(*input.CompanyName) {
		return models.User{}, msgs.ErrInvalidInput
	}
	if input.Description != nil && !validate.Description(*input.Description) {
		return models.User{}, msgs.ErrInvalidInput
	}
	if input.SocialLinks != nil {
		for platform, url := range input.SocialLinks.Platforms {
			if !platform.Valid() || !validate.URL(url) {
				return models.User{}, msgs.ErrInvalidInput
			}
		}
		if len(input.SocialLinks.Other) > models.MaxOtherSocialLinks {
			return models.User{}, msgs.ErrInvalidInput
		}
		for _, link := range input.SocialLinks.Other {
			if !validate.CustomLinkLabel(link.Label) || !validate.URL(link.URL) {
				return models.User{}, msgs.ErrInvalidInput
			}
		}
	}

	updated, err := s.User().UpdateProfile(ctx, userID, input)
	if err != nil {
		return models.User{}, err
	}

	var changedFields []string
	if input.Name != nil {
		changedFields = append(changedFields, "name")
	}
	if input.AvatarURL != nil {
		changedFields = append(changedFields, "avatar_url")
	}
	if input.CompanyName != nil {
		changedFields = append(changedFields, "company_name")
	}
	if input.Description != nil {
		changedFields = append(changedFields, "description")
	}
	if input.SocialLinks != nil {
		changedFields = append(changedFields, "social_links")
	}
	s.audit.Record(ctx, models.AuditProfileUpdated, &userID, map[string]any{"changed_fields": changedFields})

	return updated, nil
}

// SetPassword sets an initial password on an account that doesn't have one
// yet (e.g. an OAuth-only account), then revokes all other active sessions
// so that a hijacked session silently adding a persistent password
// credential doesn't go unnoticed — the legitimate user's other sessions
// die and they'll see it. The current session (identified by sessionID) is
// kept alive. Unlike ChangePassword, this never verifies a current
// password, since none exists yet.
//
// Error cases:
//   - msgs.ErrPasswordAlreadySet — account already has a password identity; use ChangePassword instead
//   - msgs.ErrInvalidCredentials — newPassword fails validation
func (s *userService) SetPassword(ctx context.Context, userID, sessionID uuid.UUID, newPassword string) error {
	if !validate.Password(newPassword) {
		return msgs.ErrInvalidCredentials
	}

	_, err := s.AuthIdentity().GetAuthIdentityByUserIDAndProvider(ctx, userID, "password")
	if err == nil {
		return msgs.ErrPasswordAlreadySet
	}
	if !errors.Is(err, msgs.ErrUserNotFound) {
		return err
	}

	user, err := s.User().GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	newHash, err := hash.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hashing new password: %w", err)
	}

	var revokedSessionIDs []uuid.UUID
	err = s.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := s.AuthIdentity().CreateAuthIdentity(ctx, models.AuthIdentity{
			ID:              uuid.New(),
			UserID:          userID,
			Provider:        "password",
			ProviderSubject: user.Email,
			PasswordHash:    &newHash,
		}); err != nil {
			return err
		}
		ids, err := s.Session().RevokeOtherSessionsForUser(ctx, userID, sessionID)
		if err != nil {
			return err
		}
		revokedSessionIDs = ids
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.sessions.RevokeSessions(ctx, revokedSessionIDs); err != nil {
		return err
	}
	s.audit.Record(ctx, models.AuditPasswordSet, &userID, nil)
	return nil
}

// DeleteAccount soft-deletes the caller's account (sets deleted_at, hiding
// it from every lookup this repository performs) and revokes every active
// session. If the account has a password identity, currentPassword must
// match it first — a confirmation step for an irreversible, destructive
// action, mirroring ChangePassword's shape; currentPassword is ignored for
// an OAuth-only account, which has nothing to verify it against. The
// account and its email are retained (not purged) — see
// AuthService.reactivateAccount for how a later Register with the same
// email restores it instead of being rejected.
//
// Error cases:
//   - msgs.ErrInvalidCredentials — the account has a password and currentPassword doesn't match
func (s *userService) DeleteAccount(ctx context.Context, userID, sessionID uuid.UUID, currentPassword string) error {
	identity, err := s.AuthIdentity().GetAuthIdentityByUserIDAndProvider(ctx, userID, "password")
	if err != nil && !errors.Is(err, msgs.ErrUserNotFound) {
		return err
	}
	if err == nil && identity.PasswordHash != nil {
		ok, verr := hash.VerifyPassword(currentPassword, *identity.PasswordHash)
		if verr != nil {
			return fmt.Errorf("verifying current password: %w", verr)
		}
		if !ok {
			return msgs.ErrInvalidCredentials
		}
	}

	var revokedSessionIDs []uuid.UUID
	err = s.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.User().SoftDelete(ctx, userID); err != nil {
			return err
		}
		ids, err := s.Session().RevokeAllSessionsForUser(ctx, userID)
		if err != nil {
			return err
		}
		revokedSessionIDs = ids
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.sessions.RevokeSessions(ctx, revokedSessionIDs); err != nil {
		return err
	}
	s.audit.Record(ctx, models.AuditAccountDeleted, &userID, nil)
	return nil
}
