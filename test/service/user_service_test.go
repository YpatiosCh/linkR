package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"linkMe/internal/models"
	"linkMe/internal/msgs"
	"linkMe/internal/service"
	"linkMe/internal/utils/validate"

	"github.com/google/uuid"
)

// fakeRepo, fakeUserRepo, fakeIdentityRepo, fakeSessionRepo, fakeSubscriptionRepo,
// defaultSub, and passwordIdentity are shared fixtures defined in auth_service_test.go.

func TestGetMeSuccess(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepo{
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{ID: userID, Email: "ada@example.com", Name: strPtr("Ada")}, nil
		}},
		identity: &fakeIdentityRepo{
			getByUserAndProv: func(_ context.Context, _ uuid.UUID, _ string) (models.AuthIdentity, error) {
				return passwordIdentity(t, userID, "Correct-Battery1!"), nil
			},
		},
		session: &fakeSessionRepo{},
		sub:     defaultSub(),
	}

	user, sub, hasPassword, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).GetMe(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.ID != userID {
		t.Errorf("user.ID: got %s, want %s", user.ID, userID)
	}
	if sub.PlanID != models.FreePlan.String() {
		t.Errorf("sub.PlanID: got %q, want %q", sub.PlanID, models.FreePlan.String())
	}
	if !hasPassword {
		t.Error("expected hasPassword to be true (fake reports a password identity)")
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

	_, _, _, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).GetMe(context.Background(), uuid.New())
	if !errors.Is(err, msgs.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestGetMeHasPasswordFalseForOAuthOnlyAccount(t *testing.T) {
	userID := uuid.New()
	repo := &fakeRepo{
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{ID: userID, Email: "ada@example.com"}, nil
		}},
		identity: &fakeIdentityRepo{}, // getByUserAndProv unset -> ErrUserNotFound -> hasPassword = false
		session:  &fakeSessionRepo{},
		sub:      defaultSub(),
	}

	_, _, hasPassword, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).GetMe(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if hasPassword {
		t.Error("expected hasPassword to be false for an OAuth-only account")
	}
}

func TestChangePasswordSuccess(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	identity := passwordIdentity(t, userID, "Old-Correct1!")
	revokedSessionID := uuid.New()
	sessions := &fakeSessionRepo{revokedOtherIDs: []uuid.UUID{revokedSessionID}}
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
	revoker := &fakeSessionRevoker{}
	recorder := &fakeAuditRecorder{}

	err := service.NewUserService(repo, revoker, recorder).ChangePassword(
		context.Background(), userID, sessionID, "Old-Correct1!", "New-Correct1!",
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
	if len(revoker.revokedSessionIDs) != 1 || revoker.revokedSessionIDs[0] != revokedSessionID {
		t.Errorf("expected SessionRevoker to be called with %v, got %v", []uuid.UUID{revokedSessionID}, revoker.revokedSessionIDs)
	}
	if recorder.lastEventType != models.AuditPasswordChanged {
		t.Errorf("expected AuditPasswordChanged, got %v", recorder.lastEventType)
	}
	if recorder.lastUserID == nil || *recorder.lastUserID != userID {
		t.Errorf("expected recorded userID %s, got %v", userID, recorder.lastUserID)
	}
}

func TestChangePasswordInvalidCredentials(t *testing.T) {
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "Correct-Battery1!")

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

	err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).ChangePassword(
		context.Background(), userID, uuid.New(), "wrong-password", "New-Correct1!",
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

	err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).ChangePassword(
		context.Background(), userID, uuid.New(), "any", "New-Correct1!",
	)
	if !errors.Is(err, msgs.ErrPasswordNotSet) {
		t.Fatalf("expected ErrPasswordNotSet for OAuth-only account, got %v", err)
	}
}

func TestChangePasswordWeakNewPassword(t *testing.T) {
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "Correct-Battery1!")
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

	err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).ChangePassword(
		context.Background(), userID, uuid.New(), "Correct-Battery1!", "weak",
	)
	if !errors.Is(err, msgs.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for weak new password, got %v", err)
	}
}

// --- SetPassword ---

func TestSetPasswordSuccess(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	revokedSessionID := uuid.New()
	sessions := &fakeSessionRepo{revokedOtherIDs: []uuid.UUID{revokedSessionID}}
	identities := &fakeIdentityRepo{} // getByUserAndProv unset -> no existing password identity
	revoker := &fakeSessionRevoker{}

	repo := &fakeRepo{
		user: &fakeUserRepo{getByID: func(ctx context.Context, id uuid.UUID) (models.User, error) {
			return models.User{ID: userID, Email: "ada@example.com"}, nil
		}},
		identity: identities,
		session:  sessions,
		sub:      defaultSub(),
	}

	recorder := &fakeAuditRecorder{}
	err := service.NewUserService(repo, revoker, recorder).SetPassword(
		context.Background(), userID, sessionID, "Correct-Horse1!",
	)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if identities.created == nil {
		t.Fatal("expected a password identity to be created")
	}
	if identities.created.Provider != "password" {
		t.Errorf("Provider: got %q, want %q", identities.created.Provider, "password")
	}
	if identities.created.ProviderSubject != "ada@example.com" {
		t.Errorf("ProviderSubject: got %q, want %q", identities.created.ProviderSubject, "ada@example.com")
	}
	if identities.created.PasswordHash == nil || *identities.created.PasswordHash == "" {
		t.Error("expected a non-empty PasswordHash on the created identity")
	}
	if sessions.revokedOtherForUser != userID {
		t.Errorf("expected other sessions for user %s to be revoked, got %v", userID, sessions.revokedOtherForUser)
	}
	if sessions.keptSessionID != sessionID {
		t.Errorf("expected current session %s to be kept, got %v", sessionID, sessions.keptSessionID)
	}
	if len(revoker.revokedSessionIDs) != 1 || revoker.revokedSessionIDs[0] != revokedSessionID {
		t.Errorf("expected SessionRevoker to be called with %v, got %v", []uuid.UUID{revokedSessionID}, revoker.revokedSessionIDs)
	}
	if recorder.lastEventType != models.AuditPasswordSet {
		t.Errorf("expected AuditPasswordSet, got %v", recorder.lastEventType)
	}
}

func TestSetPasswordAlreadySet(t *testing.T) {
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "existing-password")
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

	err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).SetPassword(
		context.Background(), userID, uuid.New(), "Correct-Horse1!",
	)
	if !errors.Is(err, msgs.ErrPasswordAlreadySet) {
		t.Fatalf("expected ErrPasswordAlreadySet, got %v", err)
	}
}

func TestSetPasswordWeakNewPassword(t *testing.T) {
	repo := newTestUserRepo()
	err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).SetPassword(
		context.Background(), uuid.New(), uuid.New(), "weak",
	)
	if !errors.Is(err, msgs.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for weak new password, got %v", err)
	}
}

// --- UpdateProfile ---

func newTestUserRepo() *fakeRepo {
	return &fakeRepo{
		user:     &fakeUserRepo{},
		identity: &fakeIdentityRepo{},
		session:  &fakeSessionRepo{},
		sub:      defaultSub(),
	}
}

func TestUpdateProfileSuccess_EachFieldIndependently(t *testing.T) {
	userID := uuid.New()
	repo := newTestUserRepo()
	recorder := &fakeAuditRecorder{}
	svc := service.NewUserService(repo, &fakeSessionRevoker{}, recorder)

	user, err := svc.UpdateProfile(context.Background(), userID, models.UpdateProfileInput{
		Name: strPtr("Jane"),
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.Name == nil || *user.Name != "Jane" {
		t.Errorf("Name: got %v, want %q", user.Name, "Jane")
	}

	user, err = svc.UpdateProfile(context.Background(), userID, models.UpdateProfileInput{
		AvatarURL: strPtr("https://example.com/avatar.png"),
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.AvatarURL == nil || *user.AvatarURL != "https://example.com/avatar.png" {
		t.Errorf("AvatarURL: got %v, want set", user.AvatarURL)
	}
	// Previously-set Name should be untouched by this call (partial patch).
	if user.Name == nil || *user.Name != "Jane" {
		t.Errorf("expected Name to remain %q, got %v", "Jane", user.Name)
	}

	user, err = svc.UpdateProfile(context.Background(), userID, models.UpdateProfileInput{
		CompanyName: strPtr("Jane's Templates"),
		Description: strPtr("I make Notion templates."),
		SocialLinks: &models.SocialLinks{
			Platforms: map[models.SocialPlatform]string{models.SocialPlatformDiscord: "https://discord.gg/xyz"},
			Other:     []models.CustomSocialLink{{Label: "Slack", URL: "https://joinslack.example/xyz"}},
		},
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if user.CompanyName == nil || *user.CompanyName != "Jane's Templates" {
		t.Errorf("CompanyName: got %v, want set", user.CompanyName)
	}
	if user.Description == nil || *user.Description != "I make Notion templates." {
		t.Errorf("Description: got %v, want set", user.Description)
	}
	if got := user.SocialLinks.Platforms[models.SocialPlatformDiscord]; got != "https://discord.gg/xyz" {
		t.Errorf("SocialLinks.Platforms[discord]: got %q, want set", got)
	}
	if len(user.SocialLinks.Other) != 1 || user.SocialLinks.Other[0].Label != "Slack" {
		t.Errorf("SocialLinks.Other: got %v, want one Slack entry", user.SocialLinks.Other)
	}
	if recorder.lastEventType != models.AuditProfileUpdated {
		t.Errorf("expected AuditProfileUpdated, got %v", recorder.lastEventType)
	}
	if recorder.callCount != 3 {
		t.Errorf("expected one audit event per successful UpdateProfile call, got %d", recorder.callCount)
	}
	changedFields, _ := recorder.lastMetadata["changed_fields"].([]string)
	wantFields := []string{"company_name", "description", "social_links"}
	if len(changedFields) != len(wantFields) {
		t.Fatalf("changed_fields: got %v, want %v", changedFields, wantFields)
	}
	for i, f := range wantFields {
		if changedFields[i] != f {
			t.Errorf("changed_fields[%d]: got %q, want %q", i, changedFields[i], f)
		}
	}
}

func TestUpdateProfileInvalidName(t *testing.T) {
	repo := newTestUserRepo()
	_, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(), models.UpdateProfileInput{Name: strPtr("")},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty name, got %v", err)
	}
}

func TestUpdateProfileInvalidCompanyName(t *testing.T) {
	repo := newTestUserRepo()
	_, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(),
		models.UpdateProfileInput{CompanyName: strPtr(strings.Repeat("a", validate.MaxCompanyNameLength+1))},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for too-long company name, got %v", err)
	}
}

func TestUpdateProfileInvalidDescription(t *testing.T) {
	repo := newTestUserRepo()
	_, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(),
		models.UpdateProfileInput{Description: strPtr(strings.Repeat("a", validate.MaxDescriptionLength+1))},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for too-long description, got %v", err)
	}
}

func TestUpdateProfileInvalidAvatarURL(t *testing.T) {
	repo := newTestUserRepo()
	_, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(),
		models.UpdateProfileInput{AvatarURL: strPtr("not-a-url")},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for invalid avatar URL, got %v", err)
	}
}

func TestUpdateProfileUnknownSocialPlatform(t *testing.T) {
	repo := newTestUserRepo()
	_, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(),
		models.UpdateProfileInput{SocialLinks: &models.SocialLinks{
			Platforms: map[models.SocialPlatform]string{"diskord": "https://discord.gg/xyz"},
		}},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for unknown social platform, got %v", err)
	}
}

func TestUpdateProfileInvalidSocialPlatformURL(t *testing.T) {
	repo := newTestUserRepo()
	_, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(),
		models.UpdateProfileInput{SocialLinks: &models.SocialLinks{
			Platforms: map[models.SocialPlatform]string{models.SocialPlatformDiscord: "not-a-url"},
		}},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for invalid platform URL, got %v", err)
	}
}

func TestUpdateProfileTooManyOtherSocialLinks(t *testing.T) {
	other := make([]models.CustomSocialLink, models.MaxOtherSocialLinks+1)
	for i := range other {
		other[i] = models.CustomSocialLink{Label: "Custom", URL: "https://example.com"}
	}
	repo := newTestUserRepo()
	_, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(),
		models.UpdateProfileInput{SocialLinks: &models.SocialLinks{Other: other}},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for too many custom social links, got %v", err)
	}
}

func TestUpdateProfileInvalidCustomSocialLink(t *testing.T) {
	repo := newTestUserRepo()
	_, err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(),
		models.UpdateProfileInput{SocialLinks: &models.SocialLinks{
			Other: []models.CustomSocialLink{{Label: "", URL: "https://example.com"}},
		}},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for a custom link with an empty label, got %v", err)
	}

	_, err = service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).UpdateProfile(
		context.Background(), uuid.New(),
		models.UpdateProfileInput{SocialLinks: &models.SocialLinks{
			Other: []models.CustomSocialLink{{Label: "Slack", URL: "not-a-url"}},
		}},
	)
	if !errors.Is(err, msgs.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for a custom link with an invalid URL, got %v", err)
	}
}

// --- DeleteAccount ---

func TestDeleteAccountSuccess_WithPassword(t *testing.T) {
	userID := uuid.New()
	sessionID := uuid.New()
	identity := passwordIdentity(t, userID, "Correct-Battery1!")
	revokedSessionID := uuid.New()
	sessions := &fakeSessionRepo{revokedAllIDs: []uuid.UUID{revokedSessionID}}
	users := &fakeUserRepo{}
	repo := &fakeRepo{
		user: users,
		identity: &fakeIdentityRepo{
			getByUserAndProv: func(_ context.Context, _ uuid.UUID, _ string) (models.AuthIdentity, error) {
				return identity, nil
			},
		},
		session: sessions,
		sub:     defaultSub(),
	}
	revoker := &fakeSessionRevoker{}
	recorder := &fakeAuditRecorder{}

	err := service.NewUserService(repo, revoker, recorder).DeleteAccount(context.Background(), userID, sessionID, "Correct-Battery1!")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(users.softDeletedUserIDs) != 1 || users.softDeletedUserIDs[0] != userID {
		t.Errorf("expected user %s to be soft-deleted, got %v", userID, users.softDeletedUserIDs)
	}
	if sessions.revokedAllForUser != userID {
		t.Errorf("expected all sessions for user %s to be revoked, got %v", userID, sessions.revokedAllForUser)
	}
	if len(revoker.revokedSessionIDs) != 1 || revoker.revokedSessionIDs[0] != revokedSessionID {
		t.Errorf("expected SessionRevoker to be called with %v, got %v", []uuid.UUID{revokedSessionID}, revoker.revokedSessionIDs)
	}
	if recorder.lastEventType != models.AuditAccountDeleted {
		t.Errorf("expected AuditAccountDeleted, got %v", recorder.lastEventType)
	}
}

func TestDeleteAccountSuccess_OAuthOnly(t *testing.T) {
	userID := uuid.New()
	users := &fakeUserRepo{}
	repo := &fakeRepo{
		user: users,
		// getByUserAndProv unset -> ErrUserNotFound -> OAuth-only, no password check performed
		identity: &fakeIdentityRepo{},
		session:  &fakeSessionRepo{},
		sub:      defaultSub(),
	}

	err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).DeleteAccount(context.Background(), userID, uuid.New(), "")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(users.softDeletedUserIDs) != 1 || users.softDeletedUserIDs[0] != userID {
		t.Errorf("expected user %s to be soft-deleted, got %v", userID, users.softDeletedUserIDs)
	}
}

func TestDeleteAccountWrongPassword(t *testing.T) {
	userID := uuid.New()
	identity := passwordIdentity(t, userID, "Correct-Battery1!")
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

	err := service.NewUserService(repo, &fakeSessionRevoker{}, &fakeAuditRecorder{}).DeleteAccount(context.Background(), userID, uuid.New(), "wrong-password")
	if !errors.Is(err, msgs.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for wrong password, got %v", err)
	}
}
