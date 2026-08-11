package service

import (
	"context"
	"errors"
	"linkMe/internal/models"
	"linkMe/internal/msgs"
	"linkMe/internal/repository"
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
}

// NewAuthService builds an authService backed by the given repositories,
// embedding them so auth flows can reach users, sessions, and more.
func NewAuthService(repos repository.Repository) *authService {
	return &authService{
		repos,
	}
}

// Register handles user registration: it normalizes and validates the input,
// rejects already-taken emails, hashes the password, then inside a transaction
// creates the user, a password auth identity, and a free-plan subscription.
// On success it issues a session token and returns the created user along with
// the raw token; failures return msgs.ErrInvalidCredentials,
// msgs.ErrEmailAlreadyExists, or the underlying repository error.
func (s *authService) Register(ctx context.Context, input models.RegisterInput) (models.User, string, error) {
	email := validate.NormalizeEmail(input.Email)

	if !validate.Email(email) {
		return models.User{}, "", msgs.ErrInvalidCredentials
	}
	if !validate.Password(input.Password) {
		return models.User{}, "", msgs.ErrInvalidCredentials
	}
	if !validate.Name(input.Name) {
		return models.User{}, "", msgs.ErrInvalidCredentials
	}

	_, err := s.User().GetUserByEmail(ctx, email)
	if err == nil {
		return models.User{}, "", msgs.ErrEmailAlreadyExists
	}
	if !errors.Is(err, msgs.ErrUserNotFound) {
		return models.User{}, "", err
	}

	passwordHash, err := hash.HashPassword(input.Password)
	if err != nil {
		return models.User{}, "", err
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
		return models.User{}, "", err
	}

	_, rawToken, err := s.issueSession(ctx, createdUser.ID)
	if err != nil {
		return models.User{}, "", err
	}

	return createdUser, rawToken, nil
}

// issueSession generates a fresh refresh token, stores its hash in a new
// session row that starts its own token family (rotation lineage) with the
// session duration, and returns the created session along with the raw token.
func (s *authService) issueSession(ctx context.Context, userID uuid.UUID) (models.Session, string, error) {
	rawToken, err := token.Generate()
	if err != nil {
		return models.Session{}, "", err
	}

	session, err := s.Session().CreateSession(ctx, models.Session{
		ID:               uuid.New(),
		UserID:           userID,
		RefreshTokenHash: token.Hash(rawToken),
		TokenFamilyID:    uuid.New(), // new session = new family, starting its own rotation lineage
		ExpiresAt:        time.Now().Add(sessionDuration),
	})
	if err != nil {
		return models.Session{}, "", err
	}

	return session, rawToken, nil
}
