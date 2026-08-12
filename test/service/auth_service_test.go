package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"linkMe/config"
	"linkMe/internal/models"
	"linkMe/internal/msgs"
	"linkMe/internal/repository"
	"linkMe/internal/service"
	"linkMe/pkg/hash"

	"github.com/google/uuid"
)

// testCfg is the fixed application config used in all service tests.
var testCfg = config.Config{JWTSecret: "test-secret-at-least-32-bytes-long!!"}

// --- fake repository ---
//
// The auth service embeds repository.Repository, so its business logic can be
// exercised without a database by wiring it to a fake that returns canned
// results. Only the methods each test flow touches carry non-trivial behavior;
// the rest satisfy the interface with safe no-op defaults.

type fakeRepo struct {
	user     *fakeUserRepo
	identity *fakeIdentityRepo
	session  *fakeSessionRepo
	sub      *fakeSubscriptionRepo
}

func (f *fakeRepo) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
func (f *fakeRepo) User() repository.UserRepository                 { return f.user }
func (f *fakeRepo) AuthIdentity() repository.AuthIdentityRepository { return f.identity }
func (f *fakeRepo) Subscription() repository.SubscriptionRepository { return f.sub }
func (f *fakeRepo) Session() repository.SessionRepository           { return f.session }

// defaultSub returns a minimal fakeSubscriptionRepo that always reports
// the free plan — suitable for tests that don't care about the plan.
func defaultSub() *fakeSubscriptionRepo {
	return &fakeSubscriptionRepo{planID: models.FreePlan.String()}
}

type fakeUserRepo struct {
	getByID    func(ctx context.Context, id uuid.UUID) (models.User, error)
	getByEmail func(ctx context.Context, email string) (models.User, error)
}

func (f *fakeUserRepo) CreateUser(ctx context.Context, u models.User) (models.User, error) {
	return u, nil
}
func (f *fakeUserRepo) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	if f.getByEmail != nil {
		return f.getByEmail(ctx, email)
	}
	return models.User{}, msgs.ErrUserNotFound
}
func (f *fakeUserRepo) GetUserByID(ctx context.Context, id uuid.UUID) (models.User, error) {
	return f.getByID(ctx, id)
}

type fakeIdentityRepo struct {
	get              func(ctx context.Context, provider, subject string) (models.AuthIdentity, error)
	getByUserAndProv func(ctx context.Context, userID uuid.UUID, provider string) (models.AuthIdentity, error)
	updatedHash      string
}

func (f *fakeIdentityRepo) CreateAuthIdentity(ctx context.Context, i models.AuthIdentity) (models.AuthIdentity, error) {
	return i, nil
}
func (f *fakeIdentityRepo) GetAuthIdentityByProviderAndSubject(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
	return f.get(ctx, provider, subject)
}
func (f *fakeIdentityRepo) GetAuthIdentityByUserIDAndProvider(ctx context.Context, userID uuid.UUID, provider string) (models.AuthIdentity, error) {
	if f.getByUserAndProv != nil {
		return f.getByUserAndProv(ctx, userID, provider)
	}
	return models.AuthIdentity{}, msgs.ErrUserNotFound
}
func (f *fakeIdentityRepo) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, newHash string) error {
	f.updatedHash = newHash
	return nil
}

type fakeSessionRepo struct {
	created             int
	getByHash           func(ctx context.Context, h string) (models.Session, error)
	consumedIDs         []uuid.UUID
	revokedFamilies     []uuid.UUID
	revokedSessions     []uuid.UUID
	revokedAllForUser   uuid.UUID
	revokedOtherForUser uuid.UUID
	keptSessionID       uuid.UUID
}

func (f *fakeSessionRepo) CreateSession(ctx context.Context, s models.Session) (models.Session, error) {
	f.created++
	return s, nil
}
func (f *fakeSessionRepo) GetSessionByTokenHash(ctx context.Context, h string) (models.Session, error) {
	if f.getByHash != nil {
		return f.getByHash(ctx, h)
	}
	return models.Session{}, msgs.ErrTokenInvalid
}
func (f *fakeSessionRepo) MarkSessionConsumed(ctx context.Context, id uuid.UUID) error {
	f.consumedIDs = append(f.consumedIDs, id)
	return nil
}
func (f *fakeSessionRepo) RevokeSessionFamily(ctx context.Context, familyID uuid.UUID) error {
	f.revokedFamilies = append(f.revokedFamilies, familyID)
	return nil
}
func (f *fakeSessionRepo) RevokeSession(ctx context.Context, id uuid.UUID) error {
	f.revokedSessions = append(f.revokedSessions, id)
	return nil
}
func (f *fakeSessionRepo) RevokeAllSessionsForUser(ctx context.Context, userID uuid.UUID) error {
	f.revokedAllForUser = userID
	return nil
}
func (f *fakeSessionRepo) RevokeOtherSessionsForUser(ctx context.Context, userID uuid.UUID, keepSessionID uuid.UUID) error {
	f.revokedOtherForUser = userID
	f.keptSessionID = keepSessionID
	return nil
}

type fakeSubscriptionRepo struct {
	planID string
}

func (f *fakeSubscriptionRepo) CreateUserSubscription(ctx context.Context, s models.Subscription) (models.Subscription, error) {
	return s, nil
}
func (f *fakeSubscriptionRepo) GetActiveSubscriptionByUserID(ctx context.Context, userID uuid.UUID) (models.Subscription, error) {
	return models.Subscription{PlanID: f.planID}, nil
}

// passwordIdentity builds a password auth identity whose stored hash matches
// the given plaintext password, for a user with the given id.
func passwordIdentity(t *testing.T, userID uuid.UUID, password string) models.AuthIdentity {
	t.Helper()
	h, err := hash.HashPassword(password)
	if err != nil {
		t.Fatalf("hashing password: %v", err)
	}
	return models.AuthIdentity{
		ID:              uuid.New(),
		UserID:          userID,
		Provider:        "password",
		ProviderSubject: "user@example.com",
		PasswordHash:    &h,
	}
}

func TestLoginSuccess(t *testing.T) {
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "correct-horse-battery")
	sessions := &fakeSessionRepo{}

	repo := &fakeRepo{
		identity: &fakeIdentityRepo{
			get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				if provider != "password" {
					t.Fatalf("expected provider %q, got %q", "password", provider)
				}
				if subject != "user@example.com" {
					t.Fatalf("expected normalized subject %q, got %q", "user@example.com", subject)
				}
				return identity, nil
			},
		},
		user: &fakeUserRepo{
			getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
				if id != userID {
					t.Fatalf("expected lookup for user %s, got %s", userID, id)
				}
				return models.User{ID: userID, Email: "user@example.com", Name: "Ada"}, nil
			},
		},
		session: sessions,
		sub:     defaultSub(),
	}

	svc := service.NewAuthService(repo, testCfg)

	// Mixed-case/padded email must be normalized before lookup.
	user, pair, err := svc.Login(context.Background(), models.LoginInput{
		Email:    "  User@Example.com ",
		Password: "correct-horse-battery",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != userID {
		t.Errorf("expected user %s, got %s", userID, user.ID)
	}
	if pair.RawRefreshToken == "" {
		t.Error("expected a non-empty refresh token")
	}
	if pair.AccessToken == "" {
		t.Error("expected a non-empty access token")
	}
	if sessions.created != 1 {
		t.Errorf("expected exactly one session to be created, got %d", sessions.created)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	userID := uuid.New()

	tests := []struct {
		name     string
		input    models.LoginInput
		identity *fakeIdentityRepo
	}{
		{
			name:  "malformed email",
			input: models.LoginInput{Email: "not-an-email", Password: "correct-horse-battery"},
			identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				t.Fatal("identity lookup should not run for a malformed email")
				return models.AuthIdentity{}, nil
			}},
		},
		{
			name:  "unknown email",
			input: models.LoginInput{Email: "user@example.com", Password: "correct-horse-battery"},
			identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				return models.AuthIdentity{}, msgs.ErrUserNotFound
			}},
		},
		{
			name:  "wrong password",
			input: models.LoginInput{Email: "user@example.com", Password: "wrong-password"},
			identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				return passwordIdentity(t, userID, "correct-horse-battery"), nil
			}},
		},
		{
			name:  "account without password (OAuth-only)",
			input: models.LoginInput{Email: "user@example.com", Password: "correct-horse-battery"},
			identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				return models.AuthIdentity{ID: uuid.New(), UserID: userID, Provider: "password", ProviderSubject: "user@example.com"}, nil
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sessions := &fakeSessionRepo{}
			repo := &fakeRepo{
				identity: tc.identity,
				user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
					t.Fatal("user lookup should not run when authentication fails")
					return models.User{}, nil
				}},
				session: sessions,
				sub:     defaultSub(),
			}

			_, _, err := service.NewAuthService(repo, testCfg).Login(context.Background(), tc.input)
			if !errors.Is(err, msgs.ErrInvalidCredentials) {
				t.Fatalf("expected ErrInvalidCredentials, got %v", err)
			}
			if sessions.created != 0 {
				t.Errorf("no session should be created on failed login, got %d", sessions.created)
			}
		})
	}
}

func TestRefreshSuccess(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	familyID := uuid.New()
	sessions := &fakeSessionRepo{
		getByHash: func(ctx context.Context, h string) (models.Session, error) {
			return models.Session{
				ID:            sessionID,
				UserID:        userID,
				TokenFamilyID: familyID,
				ExpiresAt:     time.Now().Add(24 * time.Hour),
			}, nil
		},
	}

	repo := &fakeRepo{
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{ID: userID, Email: "user@example.com", Name: "Ada"}, nil
		}},
		identity: &fakeIdentityRepo{get: func(ctx context.Context, p, s string) (models.AuthIdentity, error) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		}},
		session: sessions,
		sub:     defaultSub(),
	}

	svc := service.NewAuthService(repo, testCfg)
	_, pair, err := svc.Refresh(context.Background(), "some-raw-refresh-token")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if pair.AccessToken == "" || pair.RawRefreshToken == "" {
		t.Error("expected both tokens to be non-empty")
	}
	if len(sessions.consumedIDs) != 1 || sessions.consumedIDs[0] != sessionID {
		t.Errorf("expected old session %s to be consumed, got %v", sessionID, sessions.consumedIDs)
	}
	// A new session should have been created for the rotated token.
	if sessions.created != 1 {
		t.Errorf("expected exactly one new session, got %d", sessions.created)
	}
}

func TestRefreshReuseDetected(t *testing.T) {
	familyID := uuid.New()
	now := time.Now()
	sessions := &fakeSessionRepo{
		getByHash: func(ctx context.Context, h string) (models.Session, error) {
			// The session exists but was already consumed (revoked_at is set).
			return models.Session{
				ID:            uuid.New(),
				UserID:        uuid.New(),
				TokenFamilyID: familyID,
				ExpiresAt:     now.Add(24 * time.Hour),
				RevokedAt:     &now,
			}, nil
		},
	}
	repo := &fakeRepo{
		user:     &fakeUserRepo{},
		identity: &fakeIdentityRepo{},
		session:  sessions,
		sub:      defaultSub(),
	}

	_, _, err := service.NewAuthService(repo, testCfg).Refresh(context.Background(), "old-consumed-token")
	if !errors.Is(err, msgs.ErrTokenReuseDetected) {
		t.Fatalf("expected ErrTokenReuseDetected, got %v", err)
	}
	// The family must have been revoked defensively.
	if len(sessions.revokedFamilies) != 1 || sessions.revokedFamilies[0] != familyID {
		t.Errorf("expected family %s to be revoked on reuse detection, got %v", familyID, sessions.revokedFamilies)
	}
}

func TestRefreshExpiredSession(t *testing.T) {
	sessions := &fakeSessionRepo{
		getByHash: func(ctx context.Context, h string) (models.Session, error) {
			return models.Session{
				ID:        uuid.New(),
				UserID:    uuid.New(),
				ExpiresAt: time.Now().Add(-time.Minute), // already expired
			}, nil
		},
	}
	repo := &fakeRepo{
		user:     &fakeUserRepo{},
		identity: &fakeIdentityRepo{},
		session:  sessions,
		sub:      defaultSub(),
	}

	_, _, err := service.NewAuthService(repo, testCfg).Refresh(context.Background(), "expired-token")
	if !errors.Is(err, msgs.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for an expired session, got %v", err)
	}
}

func TestRegisterSuccess(t *testing.T) {
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{
		// getByEmail defaults to ErrUserNotFound — email is free
		user:     &fakeUserRepo{},
		identity: &fakeIdentityRepo{get: func(ctx context.Context, p, s string) (models.AuthIdentity, error) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		}},
		session: sessions,
		sub:     defaultSub(),
	}

	user, pair, err := service.NewAuthService(repo, testCfg).Register(context.Background(), models.RegisterInput{
		Email:    "  New@Example.com ",
		Password: "correct-horse-battery",
		Name:     "Ada",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.Email != "new@example.com" {
		t.Errorf("email should be normalized, got %q", user.Email)
	}
	if user.Name != "Ada" {
		t.Errorf("name: got %q, want %q", user.Name, "Ada")
	}
	if user.ID == (uuid.UUID{}) {
		t.Error("user.ID should be non-zero")
	}
	if pair.AccessToken == "" || pair.RawRefreshToken == "" {
		t.Error("expected both tokens to be non-empty on successful registration")
	}
	if sessions.created != 1 {
		t.Errorf("expected exactly one session to be created, got %d", sessions.created)
	}
}

func TestRegisterEmailAlreadyExists(t *testing.T) {
	existing := models.User{ID: uuid.New(), Email: "taken@example.com", Name: "Bob"}
	repo := &fakeRepo{
		user: &fakeUserRepo{
			getByEmail: func(ctx context.Context, email string) (models.User, error) {
				return existing, nil // email is taken
			},
		},
		identity: &fakeIdentityRepo{get: func(ctx context.Context, p, s string) (models.AuthIdentity, error) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		}},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	_, _, err := service.NewAuthService(repo, testCfg).Register(context.Background(), models.RegisterInput{
		Email:    "taken@example.com",
		Password: "correct-horse-battery",
		Name:     "New User",
	})
	if !errors.Is(err, msgs.ErrEmailAlreadyExists) {
		t.Fatalf("expected ErrEmailAlreadyExists, got %v", err)
	}
}

func TestLogout(t *testing.T) {
	sessionID := uuid.New()
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{
		user:     &fakeUserRepo{},
		identity: &fakeIdentityRepo{},
		session:  sessions,
		sub:      defaultSub(),
	}

	err := service.NewAuthService(repo, testCfg).Logout(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(sessions.revokedSessions) != 1 || sessions.revokedSessions[0] != sessionID {
		t.Errorf("expected session %s to be revoked, got %v", sessionID, sessions.revokedSessions)
	}
}

func TestLogoutAll(t *testing.T) {
	userID := uuid.New()
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{
		user:     &fakeUserRepo{},
		identity: &fakeIdentityRepo{},
		session:  sessions,
		sub:      defaultSub(),
	}

	err := service.NewAuthService(repo, testCfg).LogoutAll(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if sessions.revokedAllForUser != userID {
		t.Errorf("expected all sessions for user %s to be revoked, got %v", userID, sessions.revokedAllForUser)
	}
}

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

	user, sub, err := service.NewAuthService(repo, testCfg).GetMe(context.Background(), userID)
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

	_, _, err := service.NewAuthService(repo, testCfg).GetMe(context.Background(), uuid.New())
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

	err := service.NewAuthService(repo, testCfg).ChangePassword(
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

	err := service.NewAuthService(repo, testCfg).ChangePassword(
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

	err := service.NewAuthService(repo, testCfg).ChangePassword(
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

	err := service.NewAuthService(repo, testCfg).ChangePassword(
		context.Background(), userID, uuid.New(), "correct-battery", "weak",
	)
	if !errors.Is(err, msgs.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for weak new password, got %v", err)
	}
}

func TestRegisterInvalidInput(t *testing.T) {
	// None of these should reach the repository — validation gates everything.
	tests := []struct {
		name  string
		input models.RegisterInput
	}{
		{"invalid email", models.RegisterInput{Email: "not-an-email", Password: "correct-horse-battery", Name: "Ada"}},
		{"empty email", models.RegisterInput{Email: "", Password: "correct-horse-battery", Name: "Ada"}},
		{"short password", models.RegisterInput{Email: "ada@example.com", Password: "short", Name: "Ada"}},
		{"empty name", models.RegisterInput{Email: "ada@example.com", Password: "correct-horse-battery", Name: ""}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sessions := &fakeSessionRepo{}
			repo := &fakeRepo{
				user: &fakeUserRepo{
					getByEmail: func(ctx context.Context, email string) (models.User, error) {
						t.Fatal("repo should not be reached when input is invalid")
						return models.User{}, nil
					},
				},
				identity: &fakeIdentityRepo{},
				session:  sessions,
				sub:      defaultSub(),
			}

			_, _, err := service.NewAuthService(repo, testCfg).Register(context.Background(), tc.input)
			if !errors.Is(err, msgs.ErrInvalidCredentials) {
				t.Fatalf("expected ErrInvalidCredentials for %q, got %v", tc.name, err)
			}
			if sessions.created != 0 {
				t.Errorf("no session should be created on failed registration, got %d", sessions.created)
			}
		})
	}
}
