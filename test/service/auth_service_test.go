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

// strPtr returns a pointer to s, for populating the optional *string fields
// on models.User/UpdateProfileInput in test fixtures.
func strPtr(s string) *string { return &s }

// --- fake repository ---
//
// The auth service embeds repository.Repository, so its business logic can be
// exercised without a database by wiring it to a fake that returns canned
// results. Only the methods each test flow touches carry non-trivial behavior;
// the rest satisfy the interface with safe no-op defaults.

type fakeRepo struct {
	user              *fakeUserRepo
	identity          *fakeIdentityRepo
	session           *fakeSessionRepo
	sub               *fakeSubscriptionRepo
	emailVerification *fakeEmailVerificationTokenRepo
	passwordReset     *fakePasswordResetTokenRepo
}

func (f *fakeRepo) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
func (f *fakeRepo) User() repository.UserRepository                 { return f.user }
func (f *fakeRepo) AuthIdentity() repository.AuthIdentityRepository { return f.identity }
func (f *fakeRepo) Subscription() repository.SubscriptionRepository { return f.sub }
func (f *fakeRepo) Session() repository.SessionRepository           { return f.session }
func (f *fakeRepo) EmailVerificationToken() repository.EmailVerificationTokenRepository {
	return f.emailVerification
}
func (f *fakeRepo) PasswordResetToken() repository.PasswordResetTokenRepository {
	return f.passwordReset
}

// AuditEvent is never exercised through fakeRepo: AuthService/UserService
// take an AuditRecorder directly (see fakeAuditRecorder), not through the
// embedded repository.Repository, so this only exists to satisfy the
// interface.
func (f *fakeRepo) AuditEvent() repository.AuditEventRepository { return nil }

// newTestAuthService builds an authService wired to repo, testCfg, and
// no-op fakeEmailService/fakeSessionRevoker — the email-sending and
// session-revocation behavior are only asserted by the tests that
// construct and inspect their own fakes.
func newTestAuthService(repo *fakeRepo) service.AuthService {
	return service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, &fakeAuditRecorder{})
}

// fakeGoogleOAuthClient is a fake service.GoogleOAuthClient with optional
// func fields (nil defaults documented per-method), same style as every
// other fake in this file.
type fakeGoogleOAuthClient struct {
	authURL       func(state string) string
	exchange      func(ctx context.Context, code string) (string, error)
	fetchUserInfo func(ctx context.Context, accessToken string) (service.GoogleUserInfo, error)
}

func (f *fakeGoogleOAuthClient) AuthURL(state string) string {
	if f.authURL != nil {
		return f.authURL(state)
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state
}
func (f *fakeGoogleOAuthClient) Exchange(ctx context.Context, code string) (string, error) {
	if f.exchange != nil {
		return f.exchange(ctx, code)
	}
	return "fake-access-token", nil
}
func (f *fakeGoogleOAuthClient) FetchUserInfo(ctx context.Context, accessToken string) (service.GoogleUserInfo, error) {
	if f.fetchUserInfo != nil {
		return f.fetchUserInfo(ctx, accessToken)
	}
	return service.GoogleUserInfo{}, errors.New("fetchUserInfo not configured")
}

// fakeSessionRevoker is a spy service.SessionRevoker that captures the
// session ID(s) passed to each call.
type fakeSessionRevoker struct {
	revokedSessionID  uuid.UUID
	revokedSessionIDs []uuid.UUID
	err               error
}

func (f *fakeSessionRevoker) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	f.revokedSessionID = sessionID
	return f.err
}
func (f *fakeSessionRevoker) RevokeSessions(ctx context.Context, sessionIDs []uuid.UUID) error {
	f.revokedSessionIDs = sessionIDs
	return f.err
}

// fakeAuditRecorder is a spy service.AuditRecorder that records the last
// event it was called with, plus a running count, so tests can assert
// which (if any) audit event a flow recorded.
type fakeAuditRecorder struct {
	lastEventType models.AuditEventType
	lastUserID    *uuid.UUID
	lastMetadata  map[string]any
	callCount     int
}

func (f *fakeAuditRecorder) Record(ctx context.Context, eventType models.AuditEventType, userID *uuid.UUID, metadata map[string]any) {
	f.lastEventType = eventType
	f.lastUserID = userID
	f.lastMetadata = metadata
	f.callCount++
}

// fakeLoginAttemptLimiter is a spy service.LoginAttemptLimiter. Allow
// defaults to permitting every attempt (allowed: true is the zero value's
// intent, so a nil allow func just returns true) and records the last
// email it was called with so tests can assert the limiter is keyed on the
// normalized email.
type fakeLoginAttemptLimiter struct {
	allow     func(ctx context.Context, email string) (bool, error)
	lastEmail string
	callCount int
}

func (f *fakeLoginAttemptLimiter) Allow(ctx context.Context, email string) (bool, error) {
	f.lastEmail = email
	f.callCount++
	if f.allow != nil {
		return f.allow(ctx, email)
	}
	return true, nil
}

// defaultSub returns a minimal fakeSubscriptionRepo that always reports
// the free plan — suitable for tests that don't care about the plan.
func defaultSub() *fakeSubscriptionRepo {
	return &fakeSubscriptionRepo{planID: models.FreePlan.String()}
}

type fakeUserRepo struct {
	getByID                    func(ctx context.Context, id uuid.UUID) (models.User, error)
	getByEmail                 func(ctx context.Context, email string) (models.User, error)
	getByEmailIncludingDeleted func(ctx context.Context, email string) (models.User, error)
	verifiedEmailUserIDs       []uuid.UUID
	softDeletedUserIDs         []uuid.UUID
	reactivatedUserIDs         []uuid.UUID
	// stored simulates the persisted row UpdateProfile applies a partial
	// update to (mirroring the real UPDATE ... COALESCE query), so tests
	// can assert that omitted fields are left unchanged.
	stored           models.User
	updateProfileErr error
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
func (f *fakeUserRepo) GetUserByEmailIncludingDeleted(ctx context.Context, email string) (models.User, error) {
	if f.getByEmailIncludingDeleted != nil {
		return f.getByEmailIncludingDeleted(ctx, email)
	}
	return models.User{}, msgs.ErrUserNotFound
}
func (f *fakeUserRepo) GetUserByID(ctx context.Context, id uuid.UUID) (models.User, error) {
	if f.getByID != nil {
		return f.getByID(ctx, id)
	}
	// Default: the user exists and isn't deleted. Most tests only care about
	// GetUserByID indirectly (e.g. via VerifyEmail/ResetPassword's
	// deleted-account guard) and don't need to configure a specific user.
	return models.User{ID: id}, nil
}
func (f *fakeUserRepo) UpdateEmailVerifiedAt(ctx context.Context, id uuid.UUID) error {
	f.verifiedEmailUserIDs = append(f.verifiedEmailUserIDs, id)
	return nil
}
func (f *fakeUserRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	f.softDeletedUserIDs = append(f.softDeletedUserIDs, id)
	return nil
}
func (f *fakeUserRepo) Reactivate(ctx context.Context, id uuid.UUID) error {
	f.reactivatedUserIDs = append(f.reactivatedUserIDs, id)
	return nil
}
func (f *fakeUserRepo) UpdateProfile(ctx context.Context, id uuid.UUID, input models.UpdateProfileInput) (models.User, error) {
	if f.updateProfileErr != nil {
		return models.User{}, f.updateProfileErr
	}
	if input.Name != nil {
		f.stored.Name = input.Name
	}
	if input.AvatarURL != nil {
		f.stored.AvatarURL = input.AvatarURL
	}
	if input.CompanyName != nil {
		f.stored.CompanyName = input.CompanyName
	}
	if input.Description != nil {
		f.stored.Description = input.Description
	}
	if input.SocialLinks != nil {
		f.stored.SocialLinks = *input.SocialLinks
	}
	f.stored.ID = id
	return f.stored, nil
}

type fakeIdentityRepo struct {
	get              func(ctx context.Context, provider, subject string) (models.AuthIdentity, error)
	getByUserAndProv func(ctx context.Context, userID uuid.UUID, provider string) (models.AuthIdentity, error)
	updatedHash      string
	// created captures the identity passed to CreateAuthIdentity, for tests
	// that assert on what was created (e.g. SetPassword).
	created *models.AuthIdentity
}

func (f *fakeIdentityRepo) CreateAuthIdentity(ctx context.Context, i models.AuthIdentity) (models.AuthIdentity, error) {
	f.created = &i
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

	// revokedFamilyIDs/revokedAllIDs/revokedOtherIDs configure the session
	// IDs the corresponding mass-revoke method reports as affected
	// (:many RETURNING id in the real query) — tests that assert on
	// SessionRevoker being called with the right IDs set these.
	revokedFamilyIDs []uuid.UUID
	revokedAllIDs    []uuid.UUID
	revokedOtherIDs  []uuid.UUID
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
func (f *fakeSessionRepo) RevokeSessionFamily(ctx context.Context, familyID uuid.UUID) ([]uuid.UUID, error) {
	f.revokedFamilies = append(f.revokedFamilies, familyID)
	return f.revokedFamilyIDs, nil
}
func (f *fakeSessionRepo) RevokeSession(ctx context.Context, id uuid.UUID) error {
	f.revokedSessions = append(f.revokedSessions, id)
	return nil
}
func (f *fakeSessionRepo) RevokeAllSessionsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	f.revokedAllForUser = userID
	return f.revokedAllIDs, nil
}
func (f *fakeSessionRepo) RevokeOtherSessionsForUser(ctx context.Context, userID uuid.UUID, keepSessionID uuid.UUID) ([]uuid.UUID, error) {
	f.revokedOtherForUser = userID
	f.keptSessionID = keepSessionID
	return f.revokedOtherIDs, nil
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

type fakeEmailVerificationTokenRepo struct {
	getByHash   func(ctx context.Context, hash string) (models.EmailVerificationToken, error)
	created     int
	consumedIDs []uuid.UUID
}

func (f *fakeEmailVerificationTokenRepo) CreateEmailVerificationToken(ctx context.Context, t models.EmailVerificationToken) (models.EmailVerificationToken, error) {
	f.created++
	return t, nil
}
func (f *fakeEmailVerificationTokenRepo) GetEmailVerificationTokenByHash(ctx context.Context, hash string) (models.EmailVerificationToken, error) {
	if f.getByHash != nil {
		return f.getByHash(ctx, hash)
	}
	return models.EmailVerificationToken{}, msgs.ErrTokenInvalid
}
func (f *fakeEmailVerificationTokenRepo) MarkEmailVerificationTokenConsumed(ctx context.Context, id uuid.UUID) error {
	f.consumedIDs = append(f.consumedIDs, id)
	return nil
}

type fakePasswordResetTokenRepo struct {
	getByHash   func(ctx context.Context, hash string) (models.PasswordResetToken, error)
	created     int
	consumedIDs []uuid.UUID
}

func (f *fakePasswordResetTokenRepo) CreatePasswordResetToken(ctx context.Context, t models.PasswordResetToken) (models.PasswordResetToken, error) {
	f.created++
	return t, nil
}
func (f *fakePasswordResetTokenRepo) GetPasswordResetTokenByHash(ctx context.Context, hash string) (models.PasswordResetToken, error) {
	if f.getByHash != nil {
		return f.getByHash(ctx, hash)
	}
	return models.PasswordResetToken{}, msgs.ErrTokenInvalid
}
func (f *fakePasswordResetTokenRepo) MarkPasswordResetTokenConsumed(ctx context.Context, id uuid.UUID) error {
	f.consumedIDs = append(f.consumedIDs, id)
	return nil
}

// fakeEmailService is a spy service.EmailService that captures the
// email/token pair passed to each Send call, for tests asserting an email
// was actually "sent".
type fakeEmailService struct {
	verificationEmail   string
	verificationToken   string
	passwordResetEmail  string
	passwordResetToken  string
	sendVerificationErr error
	sendResetErr        error
}

func (f *fakeEmailService) SendVerificationEmail(ctx context.Context, email string, token string) error {
	f.verificationEmail = email
	f.verificationToken = token
	return f.sendVerificationErr
}
func (f *fakeEmailService) SendPasswordResetEmail(ctx context.Context, email string, token string) error {
	f.passwordResetEmail = email
	f.passwordResetToken = token
	return f.sendResetErr
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
	identity := passwordIdentity(t, userID, "Correct-Horse1!")
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
				return models.User{ID: userID, Email: "user@example.com", Name: strPtr("Ada")}, nil
			},
		},
		session: sessions,
		sub:     defaultSub(),
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)

	// Mixed-case/padded email must be normalized before lookup.
	user, pair, err := svc.Login(context.Background(), models.LoginInput{
		Email:    "  User@Example.com ",
		Password: "Correct-Horse1!",
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
	if recorder.lastEventType != models.AuditLoginSucceeded {
		t.Errorf("expected AuditLoginSucceeded, got %v", recorder.lastEventType)
	}
	if recorder.lastUserID == nil || *recorder.lastUserID != userID {
		t.Errorf("expected recorded userID %s, got %v", userID, recorder.lastUserID)
	}
}

func TestLoginDeletedAccount(t *testing.T) {
	// Deletion never touches auth_identities, so the identity+password check
	// below still succeeds for a deleted account. GetUserByID's deleted_at
	// filter then reports ErrUserNotFound — Login must translate that into
	// the same generic ErrInvalidCredentials every other failure uses,
	// never leak a distinguishable error that would reveal the account used
	// to exist.
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "Correct-Horse1!")
	repo := &fakeRepo{
		identity: &fakeIdentityRepo{
			get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				return identity, nil
			},
		},
		user: &fakeUserRepo{
			getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
				return models.User{}, msgs.ErrUserNotFound
			},
		},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	_, _, err := svc.Login(context.Background(), models.LoginInput{
		Email:    "user@example.com",
		Password: "Correct-Horse1!",
	})
	if !errors.Is(err, msgs.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for a deleted account, got %v", err)
	}
	if recorder.lastEventType != models.AuditLoginFailed {
		t.Errorf("expected AuditLoginFailed, got %v", recorder.lastEventType)
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
			input: models.LoginInput{Email: "not-an-email", Password: "Correct-Horse1!"},
			identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				t.Fatal("identity lookup should not run for a malformed email")
				return models.AuthIdentity{}, nil
			}},
		},
		{
			name:  "unknown email",
			input: models.LoginInput{Email: "user@example.com", Password: "Correct-Horse1!"},
			identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				return models.AuthIdentity{}, msgs.ErrUserNotFound
			}},
		},
		{
			name:  "wrong password",
			input: models.LoginInput{Email: "user@example.com", Password: "Wrong-Password1!"},
			identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				return passwordIdentity(t, userID, "Correct-Horse1!"), nil
			}},
		},
		{
			name:  "account without password (OAuth-only)",
			input: models.LoginInput{Email: "user@example.com", Password: "Correct-Horse1!"},
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

			recorder := &fakeAuditRecorder{}
			svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
			_, _, err := svc.Login(context.Background(), tc.input)
			if !errors.Is(err, msgs.ErrInvalidCredentials) {
				t.Fatalf("expected ErrInvalidCredentials, got %v", err)
			}
			if sessions.created != 0 {
				t.Errorf("no session should be created on failed login, got %d", sessions.created)
			}
			// The malformed-email case fails before the limiter/identity lookup
			// even runs, so nothing is recorded for it; every other case is a
			// real (if failed) login attempt.
			if tc.name == "malformed email" {
				if recorder.callCount != 0 {
					t.Errorf("expected no audit event for a malformed email, got %v", recorder.lastEventType)
				}
			} else if recorder.lastEventType != models.AuditLoginFailed {
				t.Errorf("expected AuditLoginFailed, got %v", recorder.lastEventType)
			}
		})
	}
}

func TestLoginTooManyAttempts(t *testing.T) {
	// The limiter denies even though the credentials below are otherwise
	// valid — the attempt limit must be checked before any credential
	// verification, not used as a tiebreaker after.
	repo := &fakeRepo{
		identity: &fakeIdentityRepo{
			get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				t.Fatal("identity lookup should not run once the login attempt limit is exceeded")
				return models.AuthIdentity{}, nil
			},
		},
		user: &fakeUserRepo{
			getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
				t.Fatal("user lookup should not run once the login attempt limit is exceeded")
				return models.User{}, nil
			},
		},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	limiter := &fakeLoginAttemptLimiter{allow: func(ctx context.Context, email string) (bool, error) {
		return false, nil
	}}
	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, limiter, recorder)

	_, _, err := svc.Login(context.Background(), models.LoginInput{
		Email:    "user@example.com",
		Password: "Correct-Horse1!",
	})
	if !errors.Is(err, msgs.ErrTooManyLoginAttempts) {
		t.Fatalf("expected ErrTooManyLoginAttempts, got %v", err)
	}
	if recorder.lastEventType != models.AuditLoginRateLimited {
		t.Errorf("expected AuditLoginRateLimited, got %v", recorder.lastEventType)
	}
}

func TestLoginAttemptLimiterKeyedOnNormalizedEmail(t *testing.T) {
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "Correct-Horse1!")
	repo := &fakeRepo{
		identity: &fakeIdentityRepo{
			get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				return identity, nil
			},
		},
		user: &fakeUserRepo{
			getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
				return models.User{ID: userID, Email: "user@example.com"}, nil
			},
		},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	limiter := &fakeLoginAttemptLimiter{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, limiter, &fakeAuditRecorder{})

	// Casing/whitespace variants of the same address must key the limiter
	// identically to what the identity lookup itself normalizes to.
	_, _, err := svc.Login(context.Background(), models.LoginInput{
		Email:    "  User@Example.com ",
		Password: "Correct-Horse1!",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if limiter.lastEmail != "user@example.com" {
		t.Errorf("expected limiter to be called with normalized email %q, got %q", "user@example.com", limiter.lastEmail)
	}
	if limiter.callCount != 1 {
		t.Errorf("expected exactly 1 limiter call, got %d", limiter.callCount)
	}
}

func TestLoginAttemptLimiterErrorPropagates(t *testing.T) {
	repo := &fakeRepo{
		identity: &fakeIdentityRepo{
			get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
				t.Fatal("identity lookup should not run when the limiter check itself fails")
				return models.AuthIdentity{}, nil
			},
		},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	limiterErr := errors.New("redis unavailable")
	limiter := &fakeLoginAttemptLimiter{allow: func(ctx context.Context, email string) (bool, error) {
		return false, limiterErr
	}}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, limiter, &fakeAuditRecorder{})

	_, _, err := svc.Login(context.Background(), models.LoginInput{
		Email:    "user@example.com",
		Password: "Correct-Horse1!",
	})
	if !errors.Is(err, limiterErr) {
		t.Fatalf("expected the limiter error to propagate, got %v", err)
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
			return models.User{ID: userID, Email: "user@example.com", Name: strPtr("Ada")}, nil
		}},
		identity: &fakeIdentityRepo{get: func(ctx context.Context, p, s string) (models.AuthIdentity, error) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		}},
		session: sessions,
		sub:     defaultSub(),
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
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
	// An ordinary refresh happens every ~15 minutes automatically — it must
	// not generate an audit event (only the reuse-detection branch does).
	if recorder.callCount != 0 {
		t.Errorf("expected no audit event on an ordinary refresh, got %v", recorder.lastEventType)
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

	revokedFamilyID := uuid.New()
	sessions.revokedFamilyIDs = []uuid.UUID{revokedFamilyID}
	revoker := &fakeSessionRevoker{}
	recorder := &fakeAuditRecorder{}

	_, _, err := service.NewAuthService(repo, testCfg, &fakeEmailService{}, revoker, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder).Refresh(context.Background(), "old-consumed-token")
	if !errors.Is(err, msgs.ErrTokenReuseDetected) {
		t.Fatalf("expected ErrTokenReuseDetected, got %v", err)
	}
	// The family must have been revoked defensively.
	if len(sessions.revokedFamilies) != 1 || sessions.revokedFamilies[0] != familyID {
		t.Errorf("expected family %s to be revoked on reuse detection, got %v", familyID, sessions.revokedFamilies)
	}
	if len(revoker.revokedSessionIDs) != 1 || revoker.revokedSessionIDs[0] != revokedFamilyID {
		t.Errorf("expected SessionRevoker to be called with %v, got %v", []uuid.UUID{revokedFamilyID}, revoker.revokedSessionIDs)
	}
	if recorder.lastEventType != models.AuditRefreshTokenReuseDetected {
		t.Errorf("expected AuditRefreshTokenReuseDetected, got %v", recorder.lastEventType)
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

	_, _, err := newTestAuthService(repo).Refresh(context.Background(), "expired-token")
	if !errors.Is(err, msgs.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for an expired session, got %v", err)
	}
}

func TestRegisterSuccess(t *testing.T) {
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{
		user: &fakeUserRepo{
			// getByEmailIncludingDeleted defaults to ErrUserNotFound — email
			// is free. getByEmail backs the post-register
			// RequestEmailVerification lookup: a fresh password signup is
			// never pre-verified, so this reports the new, unverified user.
			getByEmail: func(ctx context.Context, email string) (models.User, error) {
				return models.User{ID: uuid.New(), Email: "new@example.com"}, nil
			},
		},
		identity: &fakeIdentityRepo{get: func(ctx context.Context, p, s string) (models.AuthIdentity, error) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		}},
		session: sessions,
		sub:     defaultSub(),
		emailVerification: &fakeEmailVerificationTokenRepo{},
	}

	recorder := &fakeAuditRecorder{}
	emailSvc := &fakeEmailService{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	user, pair, err := svc.Register(context.Background(), models.RegisterInput{
		Email:    "  New@Example.com ",
		Password: "Correct-Horse1!",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.Email != "new@example.com" {
		t.Errorf("email should be normalized, got %q", user.Email)
	}
	if user.Name != nil {
		t.Errorf("expected Name to be unset on registration, got %q", *user.Name)
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
	if recorder.lastEventType != models.AuditUserRegistered {
		t.Errorf("expected AuditUserRegistered, got %v", recorder.lastEventType)
	}
	if recorder.lastMetadata["provider"] != "password" {
		t.Errorf("expected metadata provider=password, got %v", recorder.lastMetadata)
	}
	// A first-time password signup should be auto-sent a verification
	// email, not left to sit unverified until the user thinks to hit
	// "resend" themselves.
	if emailSvc.verificationEmail != "new@example.com" {
		t.Errorf("expected a verification email to be auto-sent to new@example.com, got %q", emailSvc.verificationEmail)
	}
}

func TestRegisterEmailAlreadyExists(t *testing.T) {
	existing := models.User{ID: uuid.New(), Email: "taken@example.com", Name: strPtr("Bob")}
	repo := &fakeRepo{
		user: &fakeUserRepo{
			getByEmailIncludingDeleted: func(ctx context.Context, email string) (models.User, error) {
				return existing, nil // email is taken, not deleted
			},
		},
		identity: &fakeIdentityRepo{
			get: func(ctx context.Context, p, s string) (models.AuthIdentity, error) {
				return models.AuthIdentity{}, msgs.ErrUserNotFound
			},
			// The existing account already has a password identity — a
			// genuine duplicate registration, not a Google-only account to
			// attach a password to.
			getByUserAndProv: func(ctx context.Context, id uuid.UUID, provider string) (models.AuthIdentity, error) {
				return passwordIdentity(t, existing.ID, "some-existing-password"), nil
			},
		},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	_, _, err := newTestAuthService(repo).Register(context.Background(), models.RegisterInput{
		Email:    "taken@example.com",
		Password: "Correct-Horse1!",
	})
	if !errors.Is(err, msgs.ErrEmailAlreadyExists) {
		t.Fatalf("expected ErrEmailAlreadyExists, got %v", err)
	}
}

func TestRegister_AttachPasswordToExistingGoogleAccount(t *testing.T) {
	verifiedAt := time.Now().Add(-24 * time.Hour)
	// A Google-created account is already email-verified (GoogleCallback
	// sets EmailVerifiedAt on creation) - attaching a password here must not
	// trigger a spurious verification email on top of that.
	existingUser := models.User{ID: uuid.New(), Email: "ada@example.com", Name: strPtr("Ada"), EmailVerifiedAt: &verifiedAt}
	identities := &fakeIdentityRepo{
		getByUserAndProv: func(ctx context.Context, id uuid.UUID, provider string) (models.AuthIdentity, error) {
			// The account only has a google identity — no password identity.
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		},
	}
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{
		user: &fakeUserRepo{
			getByEmailIncludingDeleted: func(ctx context.Context, email string) (models.User, error) {
				return existingUser, nil
			},
			getByEmail: func(ctx context.Context, email string) (models.User, error) {
				return existingUser, nil
			},
		},
		identity: identities,
		session:  sessions,
		sub:      defaultSub(),
		emailVerification: &fakeEmailVerificationTokenRepo{},
	}

	recorder := &fakeAuditRecorder{}
	emailSvc := &fakeEmailService{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	user, pair, err := svc.Register(context.Background(), models.RegisterInput{
		Email:    "ada@example.com",
		Password: "Correct-Horse1!",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != existingUser.ID {
		t.Errorf("expected the existing account to be reused, got a different user ID")
	}
	if user.Name == nil || *user.Name != "Ada" {
		t.Errorf("existing profile Name should not be overwritten, got %v", user.Name)
	}
	if pair.AccessToken == "" || pair.RawRefreshToken == "" {
		t.Error("expected both tokens to be non-empty")
	}
	if sessions.created != 1 {
		t.Errorf("expected exactly one session to be created, got %d", sessions.created)
	}
	if recorder.lastEventType != models.AuditPasswordIdentityAttached {
		t.Errorf("expected AuditPasswordIdentityAttached, got %v", recorder.lastEventType)
	}
	if emailSvc.verificationEmail != "" {
		t.Errorf("expected no verification email for an already-verified (Google) account, but one was sent to %q", emailSvc.verificationEmail)
	}
}

func TestRegister_ReactivatesDeletedAccount_WithExistingPassword(t *testing.T) {
	deletedAt := time.Now().Add(-24 * time.Hour)
	userID := uuid.New()
	deletedUser := models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada"), DeletedAt: &deletedAt}
	identity := passwordIdentity(t, userID, "Old-Correct1!")
	identities := &fakeIdentityRepo{
		getByUserAndProv: func(ctx context.Context, id uuid.UUID, provider string) (models.AuthIdentity, error) {
			return identity, nil
		},
	}
	// Was never verified before deletion either - reactivating it should
	// still get a verification email, same as any other unverified account.
	reactivated := models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada")}
	users := &fakeUserRepo{
		getByEmailIncludingDeleted: func(ctx context.Context, email string) (models.User, error) {
			return deletedUser, nil
		},
		getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			// Reactivate cleared deleted_at, so this now succeeds like any
			// other non-deleted lookup.
			return reactivated, nil
		},
		getByEmail: func(ctx context.Context, email string) (models.User, error) {
			return reactivated, nil
		},
	}
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{user: users, identity: identities, session: sessions, sub: defaultSub(), emailVerification: &fakeEmailVerificationTokenRepo{}}

	recorder := &fakeAuditRecorder{}
	emailSvc := &fakeEmailService{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	user, pair, err := svc.Register(context.Background(), models.RegisterInput{
		Email:    "ada@example.com",
		Password: "New-Correct1!",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != userID {
		t.Errorf("expected the same account ID to be reactivated, got %s", user.ID)
	}
	if len(users.reactivatedUserIDs) != 1 || users.reactivatedUserIDs[0] != userID {
		t.Errorf("expected user %s to be reactivated, got %v", userID, users.reactivatedUserIDs)
	}
	if identities.updatedHash == "" {
		t.Error("expected the existing password identity's hash to be updated")
	}
	if pair.AccessToken == "" || pair.RawRefreshToken == "" {
		t.Error("expected both tokens to be non-empty")
	}
	if sessions.created != 1 {
		t.Errorf("expected exactly one session to be created, got %d", sessions.created)
	}
	if recorder.lastEventType != models.AuditAccountReactivated {
		t.Errorf("expected AuditAccountReactivated, got %v", recorder.lastEventType)
	}
	if emailSvc.verificationEmail != "ada@example.com" {
		t.Errorf("expected a verification email for this previously-unverified account, got %q", emailSvc.verificationEmail)
	}
}

func TestRegister_ReactivatesDeletedAccount_WasOAuthOnly(t *testing.T) {
	deletedAt := time.Now().Add(-24 * time.Hour)
	verifiedAt := time.Now().Add(-48 * time.Hour)
	userID := uuid.New()
	// Was Google-only and already verified before deletion (GoogleCallback
	// sets EmailVerifiedAt on creation) - reactivating it here, now with a
	// password attached, must not send a spurious verification email for an
	// address that's provably already been proven.
	deletedUser := models.User{ID: userID, Email: "ada@example.com", DeletedAt: &deletedAt, EmailVerifiedAt: &verifiedAt}
	reactivated := models.User{ID: userID, Email: "ada@example.com", EmailVerifiedAt: &verifiedAt}
	identities := &fakeIdentityRepo{
		getByUserAndProv: func(ctx context.Context, id uuid.UUID, provider string) (models.AuthIdentity, error) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound // was Google-only before deletion
		},
	}
	users := &fakeUserRepo{
		getByEmailIncludingDeleted: func(ctx context.Context, email string) (models.User, error) {
			return deletedUser, nil
		},
		getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return reactivated, nil
		},
		getByEmail: func(ctx context.Context, email string) (models.User, error) {
			return reactivated, nil
		},
	}
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{user: users, identity: identities, session: sessions, sub: defaultSub(), emailVerification: &fakeEmailVerificationTokenRepo{}}

	recorder := &fakeAuditRecorder{}
	emailSvc := &fakeEmailService{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	user, pair, err := svc.Register(context.Background(), models.RegisterInput{
		Email:    "ada@example.com",
		Password: "New-Correct1!",
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != userID {
		t.Errorf("expected the same account ID to be reactivated, got %s", user.ID)
	}
	if identities.created == nil {
		t.Fatal("expected a new password identity to be created")
	}
	if identities.created.Provider != "password" {
		t.Errorf("Provider: got %q, want %q", identities.created.Provider, "password")
	}
	if pair.AccessToken == "" || pair.RawRefreshToken == "" {
		t.Error("expected both tokens to be non-empty")
	}
	if recorder.lastEventType != models.AuditAccountReactivated {
		t.Errorf("expected AuditAccountReactivated, got %v", recorder.lastEventType)
	}
	if emailSvc.verificationEmail != "" {
		t.Errorf("expected no verification email for a previously-Google-verified account, but one was sent to %q", emailSvc.verificationEmail)
	}
}

// --- GoogleAuthURL ---

func TestGoogleAuthURL_Success(t *testing.T) {
	repo := &fakeRepo{user: &fakeUserRepo{}, identity: &fakeIdentityRepo{}, session: &fakeSessionRepo{}, sub: defaultSub()}
	google := &fakeGoogleOAuthClient{
		authURL: func(state string) string {
			return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state
		},
	}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, google, &fakeLoginAttemptLimiter{}, &fakeAuditRecorder{})

	authURL, state, err := svc.GoogleAuthURL(context.Background())
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if state == "" {
		t.Error("expected a non-empty state value")
	}
	if authURL == "" {
		t.Error("expected a non-empty auth URL")
	}
}

// --- GoogleCallback ---

func googleInfo(subject, email string) service.GoogleUserInfo {
	return service.GoogleUserInfo{Subject: subject, Email: email, EmailVerified: true, Name: "Ada", Picture: "https://example.com/ada.png"}
}

func TestGoogleCallback_ExistingGoogleUser(t *testing.T) {
	userID := uuid.New()
	identity := models.AuthIdentity{ID: uuid.New(), UserID: userID, Provider: "google", ProviderSubject: "google-sub-1"}
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{
		identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
			if provider != "google" || subject != "google-sub-1" {
				t.Fatalf("unexpected lookup: %s/%s", provider, subject)
			}
			return identity, nil
		}},
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada")}, nil
		}},
		session: sessions,
		sub:     defaultSub(),
	}
	google := &fakeGoogleOAuthClient{
		fetchUserInfo: func(ctx context.Context, accessToken string) (service.GoogleUserInfo, error) {
			return googleInfo("google-sub-1", "ada@example.com"), nil
		},
	}
	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, google, &fakeLoginAttemptLimiter{}, recorder)

	user, pair, err := svc.GoogleCallback(context.Background(), "auth-code")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != userID {
		t.Errorf("expected user %s, got %s", userID, user.ID)
	}
	if pair.AccessToken == "" || pair.RawRefreshToken == "" {
		t.Error("expected both tokens to be non-empty")
	}
	if sessions.created != 1 {
		t.Errorf("expected exactly one session to be created, got %d", sessions.created)
	}
	if recorder.lastEventType != models.AuditGoogleLogin {
		t.Errorf("expected AuditGoogleLogin, got %v", recorder.lastEventType)
	}
}

func TestGoogleCallback_AttachToExistingPasswordAccount(t *testing.T) {
	existingUser := models.User{ID: uuid.New(), Email: "ada@example.com", Name: strPtr("Ada")}
	sessions := &fakeSessionRepo{}
	identities := &fakeIdentityRepo{
		get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		},
	}
	repo := &fakeRepo{
		identity: identities,
		user: &fakeUserRepo{getByEmail: func(ctx context.Context, email string) (models.User, error) {
			return existingUser, nil
		}},
		session: sessions,
		sub:     defaultSub(),
	}
	google := &fakeGoogleOAuthClient{
		fetchUserInfo: func(ctx context.Context, accessToken string) (service.GoogleUserInfo, error) {
			return googleInfo("google-sub-2", "ada@example.com"), nil
		},
	}
	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, google, &fakeLoginAttemptLimiter{}, recorder)

	user, pair, err := svc.GoogleCallback(context.Background(), "auth-code")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != existingUser.ID {
		t.Error("expected the existing password account to be reused")
	}
	if pair.AccessToken == "" || pair.RawRefreshToken == "" {
		t.Error("expected both tokens to be non-empty")
	}
	if sessions.created != 1 {
		t.Errorf("expected exactly one session to be created, got %d", sessions.created)
	}
	if recorder.lastEventType != models.AuditGoogleLinked {
		t.Errorf("expected AuditGoogleLinked, got %v", recorder.lastEventType)
	}
}

func TestGoogleCallback_NewUser(t *testing.T) {
	sessions := &fakeSessionRepo{}
	repo := &fakeRepo{
		identity: &fakeIdentityRepo{get: func(ctx context.Context, provider, subject string) (models.AuthIdentity, error) {
			return models.AuthIdentity{}, msgs.ErrUserNotFound
		}},
		user: &fakeUserRepo{getByEmail: func(ctx context.Context, email string) (models.User, error) {
			return models.User{}, msgs.ErrUserNotFound
		}},
		session: sessions,
		sub:     defaultSub(),
	}
	google := &fakeGoogleOAuthClient{
		fetchUserInfo: func(ctx context.Context, accessToken string) (service.GoogleUserInfo, error) {
			return googleInfo("google-sub-3", "new-google-user@example.com"), nil
		},
	}
	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, google, &fakeLoginAttemptLimiter{}, recorder)

	user, pair, err := svc.GoogleCallback(context.Background(), "auth-code")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.Email != "new-google-user@example.com" {
		t.Errorf("email: got %q, want %q", user.Email, "new-google-user@example.com")
	}
	if user.EmailVerifiedAt == nil {
		t.Error("expected EmailVerifiedAt to be set for a Google-verified email")
	}
	// Google's Name/Picture must never be seeded onto the created user —
	// account creation (via any provider) never touches profile fields;
	// that only happens via UserService.UpdateProfile.
	if user.Name != nil {
		t.Errorf("expected Name to be unset on Google signup, got %q", *user.Name)
	}
	if user.AvatarURL != nil {
		t.Errorf("expected AvatarURL to be unset on Google signup, got %q", *user.AvatarURL)
	}
	if pair.AccessToken == "" || pair.RawRefreshToken == "" {
		t.Error("expected both tokens to be non-empty")
	}
	if sessions.created != 1 {
		t.Errorf("expected exactly one session to be created, got %d", sessions.created)
	}
	if recorder.lastEventType != models.AuditUserRegistered {
		t.Errorf("expected AuditUserRegistered, got %v", recorder.lastEventType)
	}
	if recorder.lastMetadata["provider"] != "google" {
		t.Errorf("expected metadata provider=google, got %v", recorder.lastMetadata)
	}
}

func TestGoogleCallback_EmailNotVerified(t *testing.T) {
	repo := &fakeRepo{user: &fakeUserRepo{}, identity: &fakeIdentityRepo{}, session: &fakeSessionRepo{}, sub: defaultSub()}
	google := &fakeGoogleOAuthClient{
		fetchUserInfo: func(ctx context.Context, accessToken string) (service.GoogleUserInfo, error) {
			return service.GoogleUserInfo{Subject: "google-sub-4", Email: "unverified@example.com", EmailVerified: false}, nil
		},
	}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, google, &fakeLoginAttemptLimiter{}, &fakeAuditRecorder{})

	_, _, err := svc.GoogleCallback(context.Background(), "auth-code")
	if !errors.Is(err, msgs.ErrOAuthEmailNotVerified) {
		t.Fatalf("expected ErrOAuthEmailNotVerified, got %v", err)
	}
}

func TestGoogleCallback_ExchangeFailure(t *testing.T) {
	repo := &fakeRepo{user: &fakeUserRepo{}, identity: &fakeIdentityRepo{}, session: &fakeSessionRepo{}, sub: defaultSub()}
	google := &fakeGoogleOAuthClient{
		exchange: func(ctx context.Context, code string) (string, error) {
			return "", errors.New("boom")
		},
	}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, google, &fakeLoginAttemptLimiter{}, &fakeAuditRecorder{})

	_, _, err := svc.GoogleCallback(context.Background(), "auth-code")
	if err == nil {
		t.Fatal("expected an error when the code exchange fails")
	}
}

func TestGoogleCallback_UserInfoFetchFailure(t *testing.T) {
	repo := &fakeRepo{user: &fakeUserRepo{}, identity: &fakeIdentityRepo{}, session: &fakeSessionRepo{}, sub: defaultSub()}
	google := &fakeGoogleOAuthClient{
		fetchUserInfo: func(ctx context.Context, accessToken string) (service.GoogleUserInfo, error) {
			return service.GoogleUserInfo{}, errors.New("boom")
		},
	}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, google, &fakeLoginAttemptLimiter{}, &fakeAuditRecorder{})

	_, _, err := svc.GoogleCallback(context.Background(), "auth-code")
	if err == nil {
		t.Fatal("expected an error when the userinfo fetch fails")
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
	revoker := &fakeSessionRevoker{}
	recorder := &fakeAuditRecorder{}

	err := service.NewAuthService(repo, testCfg, &fakeEmailService{}, revoker, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder).Logout(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(sessions.revokedSessions) != 1 || sessions.revokedSessions[0] != sessionID {
		t.Errorf("expected session %s to be revoked, got %v", sessionID, sessions.revokedSessions)
	}
	if revoker.revokedSessionID != sessionID {
		t.Errorf("expected SessionRevoker to be called with %s, got %s", sessionID, revoker.revokedSessionID)
	}
	if recorder.lastEventType != models.AuditLogout {
		t.Errorf("expected AuditLogout, got %v", recorder.lastEventType)
	}
	if recorder.lastMetadata["session_id"] != sessionID.String() {
		t.Errorf("expected metadata session_id=%s, got %v", sessionID, recorder.lastMetadata)
	}
}

func TestLogoutAll(t *testing.T) {
	userID := uuid.New()
	sessions := &fakeSessionRepo{revokedAllIDs: []uuid.UUID{uuid.New(), uuid.New()}}
	repo := &fakeRepo{
		user:     &fakeUserRepo{},
		identity: &fakeIdentityRepo{},
		session:  sessions,
		sub:      defaultSub(),
	}
	revoker := &fakeSessionRevoker{}
	recorder := &fakeAuditRecorder{}

	err := service.NewAuthService(repo, testCfg, &fakeEmailService{}, revoker, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder).LogoutAll(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(revoker.revokedSessionIDs) != 2 {
		t.Errorf("expected SessionRevoker to be called with %v, got %v", sessions.revokedAllIDs, revoker.revokedSessionIDs)
	}
	if sessions.revokedAllForUser != userID {
		t.Errorf("expected all sessions for user %s to be revoked, got %v", userID, sessions.revokedAllForUser)
	}
	if recorder.lastEventType != models.AuditLogoutAll {
		t.Errorf("expected AuditLogoutAll, got %v", recorder.lastEventType)
	}
	if recorder.lastUserID == nil || *recorder.lastUserID != userID {
		t.Errorf("expected recorded userID %s, got %v", userID, recorder.lastUserID)
	}
}

func TestRegisterInvalidInput(t *testing.T) {
	// None of these should reach the repository — validation gates everything.
	tests := []struct {
		name  string
		input models.RegisterInput
	}{
		{"invalid email", models.RegisterInput{Email: "not-an-email", Password: "Correct-Horse1!"}},
		{"empty email", models.RegisterInput{Email: "", Password: "Correct-Horse1!"}},
		{"short password", models.RegisterInput{Email: "ada@example.com", Password: "short"}},
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

			_, _, err := newTestAuthService(repo).Register(context.Background(), tc.input)
			if !errors.Is(err, msgs.ErrInvalidCredentials) {
				t.Fatalf("expected ErrInvalidCredentials for %q, got %v", tc.name, err)
			}
			if sessions.created != 0 {
				t.Errorf("no session should be created on failed registration, got %d", sessions.created)
			}
		})
	}
}

// --- RequestEmailVerification ---

func TestRequestEmailVerification_Success(t *testing.T) {
	userID := uuid.New()
	tokens := &fakeEmailVerificationTokenRepo{}
	emailSvc := &fakeEmailService{}
	repo := &fakeRepo{
		user: &fakeUserRepo{getByEmail: func(ctx context.Context, email string) (models.User, error) {
			return models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada")}, nil
		}},
		identity:          &fakeIdentityRepo{},
		session:           &fakeSessionRepo{},
		sub:               defaultSub(),
		emailVerification: tokens,
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	if err := svc.RequestEmailVerification(context.Background(), "  Ada@Example.com "); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if tokens.created != 1 {
		t.Errorf("expected exactly one token to be created, got %d", tokens.created)
	}
	if emailSvc.verificationEmail != "ada@example.com" {
		t.Errorf("expected verification email to normalized address, got %q", emailSvc.verificationEmail)
	}
	if emailSvc.verificationToken == "" {
		t.Error("expected a non-empty raw token to be sent")
	}
	if recorder.lastEventType != models.AuditEmailVerificationRequested {
		t.Errorf("expected AuditEmailVerificationRequested, got %v", recorder.lastEventType)
	}
	if recorder.lastUserID == nil || *recorder.lastUserID != userID {
		t.Errorf("expected recorded userID %s, got %v", userID, recorder.lastUserID)
	}
}

func TestRequestEmailVerification_UnknownEmail(t *testing.T) {
	tokens := &fakeEmailVerificationTokenRepo{}
	emailSvc := &fakeEmailService{}
	repo := &fakeRepo{
		user:              &fakeUserRepo{},
		identity:          &fakeIdentityRepo{},
		session:           &fakeSessionRepo{},
		sub:               defaultSub(),
		emailVerification: tokens,
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	if err := svc.RequestEmailVerification(context.Background(), "unknown@example.com"); err != nil {
		t.Fatalf("expected silent success, got error: %v", err)
	}
	if tokens.created != 0 {
		t.Errorf("no token should be created for an unknown email, got %d", tokens.created)
	}
	if emailSvc.verificationEmail != "" {
		t.Error("no email should be sent for an unknown email")
	}
	if recorder.callCount != 0 {
		t.Errorf("no audit event should be recorded for the silent unknown-email no-op, got %v", recorder.lastEventType)
	}
}

func TestRequestEmailVerification_AlreadyVerified(t *testing.T) {
	verifiedAt := time.Now().Add(-time.Hour)
	tokens := &fakeEmailVerificationTokenRepo{}
	emailSvc := &fakeEmailService{}
	repo := &fakeRepo{
		user: &fakeUserRepo{getByEmail: func(ctx context.Context, email string) (models.User, error) {
			return models.User{ID: uuid.New(), Email: email, EmailVerifiedAt: &verifiedAt}, nil
		}},
		identity:          &fakeIdentityRepo{},
		session:           &fakeSessionRepo{},
		sub:               defaultSub(),
		emailVerification: tokens,
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	if err := svc.RequestEmailVerification(context.Background(), "ada@example.com"); err != nil {
		t.Fatalf("expected silent success, got error: %v", err)
	}
	if tokens.created != 0 {
		t.Errorf("no token should be created for an already-verified email, got %d", tokens.created)
	}
	if emailSvc.verificationEmail != "" {
		t.Error("no email should be sent for an already-verified email")
	}
	if recorder.callCount != 0 {
		t.Errorf("no audit event should be recorded for the silent already-verified no-op, got %v", recorder.lastEventType)
	}
}

// --- VerifyEmail ---

func TestVerifyEmail_Success(t *testing.T) {
	userID := uuid.New()
	tokenID := uuid.New()
	tokens := &fakeEmailVerificationTokenRepo{
		getByHash: func(ctx context.Context, hash string) (models.EmailVerificationToken, error) {
			return models.EmailVerificationToken{
				ID:        tokenID,
				UserID:    userID,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}
	users := &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
		return models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada")}, nil
	}}
	repo := &fakeRepo{
		user:              users,
		identity:          &fakeIdentityRepo{},
		session:           &fakeSessionRepo{},
		sub:               defaultSub(),
		emailVerification: tokens,
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, &fakeEmailService{}, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	user, sub, hasPassword, err := svc.VerifyEmail(context.Background(), "some-raw-token")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != userID {
		t.Errorf("user.ID: got %s, want %s", user.ID, userID)
	}
	if sub.PlanID != models.FreePlan.String() {
		t.Errorf("sub.PlanID: got %q, want %q", sub.PlanID, models.FreePlan.String())
	}
	if len(users.verifiedEmailUserIDs) != 1 || users.verifiedEmailUserIDs[0] != userID {
		t.Errorf("expected user %s to be marked verified, got %v", userID, users.verifiedEmailUserIDs)
	}
	if len(tokens.consumedIDs) != 1 || tokens.consumedIDs[0] != tokenID {
		t.Errorf("expected token %s to be consumed, got %v", tokenID, tokens.consumedIDs)
	}
	if hasPassword {
		t.Error("expected hasPassword to be false (fakeIdentityRepo defaults to ErrUserNotFound)")
	}
	if recorder.lastEventType != models.AuditEmailVerified {
		t.Errorf("expected AuditEmailVerified, got %v", recorder.lastEventType)
	}
}

func TestVerifyEmail_NotFound(t *testing.T) {
	repo := &fakeRepo{
		user:              &fakeUserRepo{},
		identity:          &fakeIdentityRepo{},
		session:           &fakeSessionRepo{},
		sub:               defaultSub(),
		emailVerification: &fakeEmailVerificationTokenRepo{},
	}

	_, _, _, err := newTestAuthService(repo).VerifyEmail(context.Background(), "unknown-token")
	if !errors.Is(err, msgs.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestVerifyEmail_Expired(t *testing.T) {
	tokens := &fakeEmailVerificationTokenRepo{
		getByHash: func(ctx context.Context, hash string) (models.EmailVerificationToken, error) {
			return models.EmailVerificationToken{
				ID:        uuid.New(),
				UserID:    uuid.New(),
				ExpiresAt: time.Now().Add(-time.Minute),
			}, nil
		},
	}
	repo := &fakeRepo{
		user:              &fakeUserRepo{},
		identity:          &fakeIdentityRepo{},
		session:           &fakeSessionRepo{},
		sub:               defaultSub(),
		emailVerification: tokens,
	}

	_, _, _, err := newTestAuthService(repo).VerifyEmail(context.Background(), "expired-token")
	if !errors.Is(err, msgs.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for an expired token, got %v", err)
	}
}

func TestVerifyEmail_AlreadyUsed(t *testing.T) {
	usedAt := time.Now().Add(-time.Minute)
	tokens := &fakeEmailVerificationTokenRepo{
		getByHash: func(ctx context.Context, hash string) (models.EmailVerificationToken, error) {
			return models.EmailVerificationToken{
				ID:        uuid.New(),
				UserID:    uuid.New(),
				ExpiresAt: time.Now().Add(time.Hour),
				UsedAt:    &usedAt,
			}, nil
		},
	}
	repo := &fakeRepo{
		user:              &fakeUserRepo{},
		identity:          &fakeIdentityRepo{},
		session:           &fakeSessionRepo{},
		sub:               defaultSub(),
		emailVerification: tokens,
	}

	_, _, _, err := newTestAuthService(repo).VerifyEmail(context.Background(), "consumed-token")
	if !errors.Is(err, msgs.ErrTokenAlreadyUsed) {
		t.Fatalf("expected ErrTokenAlreadyUsed, got %v", err)
	}
}

func TestVerifyEmail_DeletedAccount(t *testing.T) {
	tokens := &fakeEmailVerificationTokenRepo{
		getByHash: func(ctx context.Context, hash string) (models.EmailVerificationToken, error) {
			return models.EmailVerificationToken{
				ID:        uuid.New(),
				UserID:    uuid.New(),
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}
	repo := &fakeRepo{
		// getByID returns ErrUserNotFound, simulating a soft-deleted (and
		// thus invisible) account owning this otherwise-valid token.
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{}, msgs.ErrUserNotFound
		}},
		identity:          &fakeIdentityRepo{},
		session:           &fakeSessionRepo{},
		sub:               defaultSub(),
		emailVerification: tokens,
	}

	_, _, _, err := newTestAuthService(repo).VerifyEmail(context.Background(), "some-token")
	if !errors.Is(err, msgs.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for a deleted account, got %v", err)
	}
}

// --- RequestPasswordReset ---

func TestRequestPasswordReset_Success(t *testing.T) {
	userID := uuid.New()
	tokens := &fakePasswordResetTokenRepo{}
	emailSvc := &fakeEmailService{}
	repo := &fakeRepo{
		user: &fakeUserRepo{getByEmail: func(ctx context.Context, email string) (models.User, error) {
			return models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada")}, nil
		}},
		identity:      &fakeIdentityRepo{},
		session:       &fakeSessionRepo{},
		sub:           defaultSub(),
		passwordReset: tokens,
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	if err := svc.RequestPasswordReset(context.Background(), "  Ada@Example.com "); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if tokens.created != 1 {
		t.Errorf("expected exactly one token to be created, got %d", tokens.created)
	}
	if emailSvc.passwordResetEmail != "ada@example.com" {
		t.Errorf("expected password reset email to normalized address, got %q", emailSvc.passwordResetEmail)
	}
	if emailSvc.passwordResetToken == "" {
		t.Error("expected a non-empty raw token to be sent")
	}
	if recorder.lastEventType != models.AuditPasswordResetRequested {
		t.Errorf("expected AuditPasswordResetRequested, got %v", recorder.lastEventType)
	}
}

func TestRequestPasswordReset_UnknownEmail(t *testing.T) {
	tokens := &fakePasswordResetTokenRepo{}
	emailSvc := &fakeEmailService{}
	repo := &fakeRepo{
		user:          &fakeUserRepo{},
		identity:      &fakeIdentityRepo{},
		session:       &fakeSessionRepo{},
		sub:           defaultSub(),
		passwordReset: tokens,
	}

	recorder := &fakeAuditRecorder{}
	svc := service.NewAuthService(repo, testCfg, emailSvc, &fakeSessionRevoker{}, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder)
	if err := svc.RequestPasswordReset(context.Background(), "unknown@example.com"); err != nil {
		t.Fatalf("expected silent success, got error: %v", err)
	}
	if tokens.created != 0 {
		t.Errorf("no token should be created for an unknown email, got %d", tokens.created)
	}
	if emailSvc.passwordResetEmail != "" {
		t.Error("no email should be sent for an unknown email")
	}
	if recorder.callCount != 0 {
		t.Errorf("no audit event should be recorded for the silent unknown-email no-op, got %v", recorder.lastEventType)
	}
}

// --- ResetPassword ---

func TestResetPassword_Success(t *testing.T) {
	userID := uuid.New()
	tokenID := uuid.New()
	identity := passwordIdentity(t, userID, "Old-Correct1!")
	tokens := &fakePasswordResetTokenRepo{
		getByHash: func(ctx context.Context, hash string) (models.PasswordResetToken, error) {
			return models.PasswordResetToken{
				ID:        tokenID,
				UserID:    userID,
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}
	identities := &fakeIdentityRepo{
		getByUserAndProv: func(ctx context.Context, id uuid.UUID, provider string) (models.AuthIdentity, error) {
			return identity, nil
		},
	}
	revokedSessionID := uuid.New()
	sessions := &fakeSessionRepo{revokedAllIDs: []uuid.UUID{revokedSessionID}}
	repo := &fakeRepo{
		user:          &fakeUserRepo{},
		identity:      identities,
		session:       sessions,
		sub:           defaultSub(),
		passwordReset: tokens,
	}
	revoker := &fakeSessionRevoker{}
	recorder := &fakeAuditRecorder{}

	err := service.NewAuthService(repo, testCfg, &fakeEmailService{}, revoker, &fakeGoogleOAuthClient{}, &fakeLoginAttemptLimiter{}, recorder).ResetPassword(context.Background(), "some-raw-token", "New-Correct1!")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if identities.updatedHash == "" {
		t.Error("expected password hash to be updated")
	}
	if len(tokens.consumedIDs) != 1 || tokens.consumedIDs[0] != tokenID {
		t.Errorf("expected token %s to be consumed, got %v", tokenID, tokens.consumedIDs)
	}
	if sessions.revokedAllForUser != userID {
		t.Errorf("expected all sessions for user %s to be revoked, got %v", userID, sessions.revokedAllForUser)
	}
	if len(revoker.revokedSessionIDs) != 1 || revoker.revokedSessionIDs[0] != revokedSessionID {
		t.Errorf("expected SessionRevoker to be called with %v, got %v", []uuid.UUID{revokedSessionID}, revoker.revokedSessionIDs)
	}
	if recorder.lastEventType != models.AuditPasswordResetCompleted {
		t.Errorf("expected AuditPasswordResetCompleted, got %v", recorder.lastEventType)
	}
}

func TestResetPassword_TokenInvalid(t *testing.T) {
	repo := &fakeRepo{
		user:          &fakeUserRepo{},
		identity:      &fakeIdentityRepo{},
		session:       &fakeSessionRepo{},
		sub:           defaultSub(),
		passwordReset: &fakePasswordResetTokenRepo{},
	}

	err := newTestAuthService(repo).ResetPassword(context.Background(), "unknown-token", "New-Correct1!")
	if !errors.Is(err, msgs.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestResetPassword_TokenAlreadyUsed(t *testing.T) {
	usedAt := time.Now().Add(-time.Minute)
	tokens := &fakePasswordResetTokenRepo{
		getByHash: func(ctx context.Context, hash string) (models.PasswordResetToken, error) {
			return models.PasswordResetToken{
				ID:        uuid.New(),
				UserID:    uuid.New(),
				ExpiresAt: time.Now().Add(time.Hour),
				UsedAt:    &usedAt,
			}, nil
		},
	}
	repo := &fakeRepo{
		user:          &fakeUserRepo{},
		identity:      &fakeIdentityRepo{},
		session:       &fakeSessionRepo{},
		sub:           defaultSub(),
		passwordReset: tokens,
	}

	err := newTestAuthService(repo).ResetPassword(context.Background(), "consumed-token", "New-Correct1!")
	if !errors.Is(err, msgs.ErrTokenAlreadyUsed) {
		t.Fatalf("expected ErrTokenAlreadyUsed, got %v", err)
	}
}

func TestResetPassword_OAuthOnlyAccount(t *testing.T) {
	tokens := &fakePasswordResetTokenRepo{
		getByHash: func(ctx context.Context, hash string) (models.PasswordResetToken, error) {
			return models.PasswordResetToken{
				ID:        uuid.New(),
				UserID:    uuid.New(),
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}
	repo := &fakeRepo{
		user: &fakeUserRepo{},
		// getByUserAndProv not set → defaults to ErrUserNotFound → OAuth-only
		identity:      &fakeIdentityRepo{},
		session:       &fakeSessionRepo{},
		sub:           defaultSub(),
		passwordReset: tokens,
	}

	err := newTestAuthService(repo).ResetPassword(context.Background(), "some-raw-token", "New-Correct1!")
	if !errors.Is(err, msgs.ErrPasswordNotSet) {
		t.Fatalf("expected ErrPasswordNotSet for OAuth-only account, got %v", err)
	}
}

func TestResetPassword_DeletedAccount(t *testing.T) {
	tokens := &fakePasswordResetTokenRepo{
		getByHash: func(ctx context.Context, hash string) (models.PasswordResetToken, error) {
			return models.PasswordResetToken{
				ID:        uuid.New(),
				UserID:    uuid.New(),
				ExpiresAt: time.Now().Add(time.Hour),
			}, nil
		},
	}
	repo := &fakeRepo{
		// getByID returns ErrUserNotFound, simulating a soft-deleted (and
		// thus invisible) account owning this otherwise-valid token.
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{}, msgs.ErrUserNotFound
		}},
		identity:      &fakeIdentityRepo{},
		session:       &fakeSessionRepo{},
		sub:           defaultSub(),
		passwordReset: tokens,
	}

	err := newTestAuthService(repo).ResetPassword(context.Background(), "some-raw-token", "New-Correct1!")
	if !errors.Is(err, msgs.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for a deleted account, got %v", err)
	}
}

func TestResetPassword_WeakPassword(t *testing.T) {
	repo := &fakeRepo{
		user:          &fakeUserRepo{},
		identity:      &fakeIdentityRepo{},
		session:       &fakeSessionRepo{},
		sub:           defaultSub(),
		passwordReset: &fakePasswordResetTokenRepo{},
	}

	err := newTestAuthService(repo).ResetPassword(context.Background(), "some-raw-token", "weak")
	if !errors.Is(err, msgs.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for a weak new password, got %v", err)
	}
}
