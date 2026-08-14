package service_test

import (
	"context"
	"errors"
	"testing"

	"linkMe/internal/models"
	"linkMe/internal/msgs"
	"linkMe/internal/service"

	"github.com/google/uuid"
)

// fakeRepo, fakeUserRepo, fakeIdentityRepo, fakeSessionRepo, fakeSubscriptionRepo,
// defaultSub, and passwordIdentity are shared fixtures defined in auth_service_test.go.

func TestGetMeSuccess(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepo{
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{ID: userID, Email: "ada@example.com", Name: "Ada"}, nil
		}},
		identity: &fakeIdentityRepo{},
		session:  &fakeSessionRepo{},
		sub:      defaultSub(),
	}

	user, sub, err := service.NewUserService(repo).GetMe(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != userID {
		t.Errorf("user.ID: got %s, want %s", user.ID, userID)
	}
	if sub.PlanID != models.FreePlan.String() {
		t.Errorf("sub.PlanID: got %q, want %q", sub.PlanID, models.FreePlan.String())
	}
}

func TestGetMeUserNotFound(t *testing.T) {
	repo := &fakeRepo{
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{}, msgs.ErrUserNotFound
		}},
		identity: &fakeIdentityRepo{},
		session:  &fakeSessionRepo{},
		sub:      defaultSub(),
	}

	_, _, err := service.NewUserService(repo).GetMe(context.Background(), uuid.New())
	if !errors.Is(err, msgs.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestChangePasswordSuccess(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	identity := passwordIdentity(t, userID, "old-correct-battery")
	sessions := &fakeSessionRepo{}
	identities := &fakeIdentityRepo{
		getByUserAndProv: func(_ context.Context, id uuid.UUID, provider string) (models.AuthIdentity, error) {
			return identity, nil
		},
	}

	repo := &fakeRepo{
		user:     &fakeUserRepo{},
		identity: identities,
		session:  sessions,
		sub:      defaultSub(),
	}

	err := service.NewUserService(repo).ChangePassword(
		context.Background(), userID, sessionID, "old-correct-battery", "new-correct-battery",
	)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if identities.updatedHash == "" {
		t.Error("expected password hash to be updated")
	}
	if sessions.revokedOtherForUser != userID {
		t.Errorf("expected other sessions for user %s to be revoked, got %v", userID, sessions.revokedOtherForUser)
	}
	if sessions.keptSessionID != sessionID {
		t.Errorf("expected current session %s to be kept, got %v", sessionID, sessions.keptSessionID)
	}
}

func TestChangePasswordInvalidCredentials(t *testing.T) {
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "correct-battery")

	repo := &fakeRepo{
		user: &fakeUserRepo{},
		identity: &fakeIdentityRepo{
			getByUserAndProv: func(_ context.Context, _ uuid.UUID, _ string) (models.AuthIdentity, error) {
				return identity, nil
			},
		},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	err := service.NewUserService(repo).ChangePassword(
		context.Background(), userID, uuid.New(), "wrong-password", "new-correct-battery",
	)
	if !errors.Is(err, msgs.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for wrong current password, got %v", err)
	}
}

func TestChangePasswordOAuthOnlyAccount(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepo{
		user: &fakeUserRepo{},
		// getByUserAndProv not set → defaults to ErrUserNotFound → OAuth-only
		identity: &fakeIdentityRepo{},
		session:  &fakeSessionRepo{},
		sub:      defaultSub(),
	}

	err := service.NewUserService(repo).ChangePassword(
		context.Background(), userID, uuid.New(), "any", "new-correct-battery",
	)
	if !errors.Is(err, msgs.ErrPasswordNotSet) {
		t.Fatalf("expected ErrPasswordNotSet for OAuth-only account, got %v", err)
	}
}

func TestChangePasswordWeakNewPassword(t *testing.T) {
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "correct-battery")
	repo := &fakeRepo{
		user: &fakeUserRepo{},
		identity: &fakeIdentityRepo{
			getByUserAndProv: func(_ context.Context, _ uuid.UUID, _ string) (models.AuthIdentity, error) {
				return identity, nil
			},
		},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	err := service.NewUserService(repo).ChangePassword(
		context.Background(), userID, uuid.New(), "correct-battery", "weak",
	)
	if !errors.Is(err, msgs.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for weak new password, got %v", err)
	}
}
