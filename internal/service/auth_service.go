package service

import (
	"context"
	"errors"
	"fmt"
	"linkMe/config"
	"linkMe/internal/models"
	"linkMe/internal/msgs"
	"linkMe/internal/repository"
	"linkMe/internal/utils/jwttoken"
	"linkMe/internal/utils/token"
	"linkMe/internal/utils/validate"
	"linkMe/pkg/hash"
	"time"

	"github.com/google/uuid"
)

const sessionDuration = 30 * 24 * time.Hour // 30 days

// authService implements AuthService by combining the embedded shared
// repository with the business rules for authentication.
type authService struct {
	repository.Repository
	cfg config.Config
}

// NewAuthService builds an authService backed by the given repositories and
// application config, embedding the repository so auth flows can reach all
// entity repositories and WithinTx directly.
func NewAuthService(repos repository.Repository, cfg config.Config) *authService {
	return &authService{Repository: repos, cfg: cfg}
}

// Register handles user registration: it normalizes and validates the input,
// rejects already-taken emails, hashes the password, then inside a transaction
// creates the user, a password auth identity, and a free-plan subscription.
// On success it issues a TokenPair and returns the created user with it;
// failures return msgs.ErrInvalidCredentials, msgs.ErrEmailAlreadyExists, or
// the underlying repository error.
func (s *authService) Register(ctx context.Context, input models.RegisterInput) (models.User, TokenPair, error) {
	email := validate.NormalizeEmail(input.Email)

	if !validate.Email(email) {
		return models.User{}, TokenPair{}, msgs.ErrInvalidCredentials
	}
	if !validate.Password(input.Password) {
		return models.User{}, TokenPair{}, msgs.ErrInvalidCredentials
	}
	if !validate.Name(input.Name) {
		return models.User{}, TokenPair{}, msgs.ErrInvalidCredentials
	}

	_, err := s.User().GetUserByEmail(ctx, email)
	if err == nil {
		return models.User{}, TokenPair{}, msgs.ErrEmailAlreadyExists
	}
	if !errors.Is(err, msgs.ErrUserNotFound) {
		return models.User{}, TokenPair{}, err
	}

	passwordHash, err := hash.HashPassword(input.Password)
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	var createdUser models.User

	err = s.WithinTx(ctx, func(ctx context.Context) error {
		userID := uuid.New()

		user, err := s.User().CreateUser(ctx, models.User{
			ID:    userID,
			Email: email,
			Name:  input.Name,
		})
		if err != nil {
			return err
		}
		createdUser = user

		_, err = s.AuthIdentity().CreateAuthIdentity(ctx, models.AuthIdentity{
			ID:              uuid.New(),
			UserID:          userID,
			Provider:        "password",
			ProviderSubject: email,
			PasswordHash:    &passwordHash,
		})
		if err != nil {
			return err
		}

		plan := models.CreatePlan(models.FreePlan)

		_, err = s.Subscription().CreateUserSubscription(ctx, models.Subscription{
			ID:     uuid.New(),
			UserID: userID,
			PlanID: plan.Name,
			Status: "active",
		})
		return err
	})
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	// New registrations are always on the free plan — no extra query needed.
	pair, err := s.issueTokenPair(ctx, createdUser.ID, models.FreePlan.String())
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	return createdUser, pair, nil
}

// Login authenticates an email/password account: it normalizes and validates
// the input, looks up the password auth identity for the email, verifies the
// supplied password against the stored Argon2id hash, and on success issues a
// TokenPair, returning the authenticated user with it.
// Every authentication failure — unknown email, an account without a password
// (e.g. OAuth-only), or a wrong password — returns the same
// msgs.ErrInvalidCredentials so the response never reveals whether the email
// exists (account-enumeration defense). Unexpected repository or hashing
// failures are returned wrapped.
func (s *authService) Login(ctx context.Context, input models.LoginInput) (models.User, TokenPair, error) {
	email := validate.NormalizeEmail(input.Email)

	if !validate.Email(email) {
		return models.User{}, TokenPair{}, msgs.ErrInvalidCredentials
	}
	if !validate.Password(input.Password) {
		return models.User{}, TokenPair{}, msgs.ErrInvalidCredentials
	}

	identity, err := s.AuthIdentity().GetAuthIdentityByProviderAndSubject(ctx, "password", email)
	if err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return models.User{}, TokenPair{}, msgs.ErrInvalidCredentials
		}
		return models.User{}, TokenPair{}, err
	}

	// An identity without a password hash (e.g. an OAuth-only account) cannot
	// authenticate by password; return the generic error rather than
	// msgs.ErrPasswordNotSet so login never reveals that the account exists.
	if identity.PasswordHash == nil {
		return models.User{}, TokenPair{}, msgs.ErrInvalidCredentials
	}

	ok, err := hash.VerifyPassword(input.Password, *identity.PasswordHash)
	if err != nil {
		return models.User{}, TokenPair{}, fmt.Errorf("error verifying password: %w", err)
	}
	if !ok {
		return models.User{}, TokenPair{}, msgs.ErrInvalidCredentials
	}

	user, err := s.User().GetUserByID(ctx, identity.UserID)
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, user.ID)
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	pair, err := s.issueTokenPair(ctx, user.ID, sub.PlanID)
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	return user, pair, nil
}

// Refresh validates the given raw refresh token, rotates it (marking the old
// session consumed and creating a new one in the same token family), and
// issues a fresh TokenPair for the session's user.
// If the presented token belongs to an already-consumed session it returns
// msgs.ErrTokenReuseDetected and revokes the entire token family to limit
// the blast radius of a stolen token. Expired or unknown tokens return
// msgs.ErrTokenInvalid.
func (s *authService) Refresh(ctx context.Context, rawRefreshToken string) (models.User, TokenPair, error) {
	tokenHash := token.Hash(rawRefreshToken)

	session, err := s.Session().GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return models.User{}, TokenPair{}, err // msgs.ErrTokenInvalid from repo
	}

	// A revoked_at that is set means this session was already consumed via
	// rotation (or explicitly revoked). Presenting its token is a reuse
	// signal — revoke the whole token family defensively.
	if session.RevokedAt != nil {
		_ = s.Session().RevokeSessionFamily(ctx, session.TokenFamilyID)
		return models.User{}, TokenPair{}, msgs.ErrTokenReuseDetected
	}

	if time.Now().After(session.ExpiresAt) {
		return models.User{}, TokenPair{}, msgs.ErrTokenInvalid
	}

	user, err := s.User().GetUserByID(ctx, session.UserID)
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, user.ID)
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	// Rotate inside a transaction: consume the old session and create the new
	// one atomically so there is never a window with two valid tokens.
	var pair TokenPair
	err = s.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.Session().MarkSessionConsumed(ctx, session.ID); err != nil {
			return err
		}
		// Pass the existing family ID so the rotation lineage is traceable and
		// the whole family can be revoked if a consumed token is ever reused.
		p, err := s.issueTokenPairInFamily(ctx, user.ID, sub.PlanID, session.TokenFamilyID)
		if err != nil {
			return err
		}
		pair = p
		return nil
	})
	if err != nil {
		return models.User{}, TokenPair{}, err
	}

	return user, pair, nil
}

// Logout revokes the session with the given ID, invalidating its refresh token.
// It is idempotent: revoking an already-revoked session succeeds silently.
func (s *authService) Logout(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.Session().RevokeSession(ctx, sessionID); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// LogoutAll revokes every active session belonging to userID, signing the user
// out of all devices and invalidating all refresh-token families.
func (s *authService) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	if err := s.Session().RevokeAllSessionsForUser(ctx, userID); err != nil {
		return fmt.Errorf("logout all: %w", err)
	}
	return nil
}

// GetMe returns the user identified by userID together with their active
// subscription. It returns msgs.ErrUserNotFound when no user matches and
// msgs.ErrSubscriptionNotFound when the user has no active subscription.
func (s *authService) GetMe(ctx context.Context, userID uuid.UUID) (models.User, models.Subscription, error) {
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
func (s *authService) ChangePassword(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID, currentPassword, newPassword string) error {
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

// issueTokenPair generates a fresh token pair starting a new token family.
// Called at the end of registration and login.
func (s *authService) issueTokenPair(ctx context.Context, userID uuid.UUID, planKey string) (TokenPair, error) {
	return s.issueTokenPairInFamily(ctx, userID, planKey, uuid.New())
}

// issueTokenPairInFamily generates a new refresh token + session in the given
// token family, signs a short-lived JWT access token, and returns both as a
// TokenPair. familyID is uuid.New() on first issuance and the existing family
// UUID on rotation, preserving the revocation lineage across refreshes. The
// planKey is embedded in the JWT so protected handlers can authorize without
// a database round-trip.
func (s *authService) issueTokenPairInFamily(ctx context.Context, userID uuid.UUID, planKey string, familyID uuid.UUID) (TokenPair, error) {
	rawToken, err := token.Generate()
	if err != nil {
		return TokenPair{}, err
	}

	session, err := s.Session().CreateSession(ctx, models.Session{
		ID:               uuid.New(),
		UserID:           userID,
		RefreshTokenHash: token.Hash(rawToken),
		TokenFamilyID:    familyID,
		ExpiresAt:        time.Now().Add(sessionDuration),
	})
	if err != nil {
		return TokenPair{}, err
	}

	accessToken, err := jwttoken.Issue(s.cfg.JWTSecret, userID, session.ID, planKey)
	if err != nil {
		return TokenPair{}, err
	}

	return TokenPair{AccessToken: accessToken, RawRefreshToken: rawToken}, nil
}
