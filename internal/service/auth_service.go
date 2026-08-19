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

const (
	sessionDuration                = 30 * 24 * time.Hour // 30 days
	emailVerificationTokenDuration = 24 * time.Hour
	passwordResetTokenDuration     = time.Hour
	loginAttemptLimit              = 5
	loginAttemptWindow             = 15 * time.Minute
)

// authService implements AuthService by combining the embedded shared
// repository with the business rules for authentication.
type authService struct {
	repository.Repository
	cfg          config.Config
	email        EmailService
	sessions     SessionRevoker
	google       GoogleOAuthClient
	loginLimiter LoginAttemptLimiter
}

// NewAuthService builds an authService backed by the given repositories,
// application config, email service, session revoker, Google OAuth client,
// and login attempt limiter, embedding the repository so auth flows can
// reach all entity repositories and WithinTx directly.
func NewAuthService(repos repository.Repository, cfg config.Config, emailSvc EmailService, sessions SessionRevoker, google GoogleOAuthClient, loginLimiter LoginAttemptLimiter) *authService {
	return &authService{Repository: repos, cfg: cfg, email: emailSvc, sessions: sessions, google: google, loginLimiter: loginLimiter}
}

// Register handles user registration: it normalizes and validates the
// email/password, rejects already-taken emails, hashes the password, then
// inside a transaction creates the user, a password auth identity, and a
// free-plan subscription. Registration intentionally collects only
// email/password — the created user's Name is left unset; profile details
// are filled in afterward via UserService.UpdateProfile. On success it
// issues a TokenPair and returns the created user with it; failures return
// msgs.ErrInvalidCredentials, msgs.ErrEmailAlreadyExists, or the underlying
// repository error.
func (s *authService) Register(ctx context.Context, input models.RegisterInput) (models.User, models.TokenPair, error) {
	email := validate.NormalizeEmail(input.Email)

	if !validate.Email(email) {
		return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
	}
	if !validate.Password(input.Password) {
		return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
	}

	existingUser, err := s.User().GetUserByEmailIncludingDeleted(ctx, email)
	if err == nil {
		if existingUser.DeletedAt != nil {
			return s.reactivateAccount(ctx, existingUser, input.Password)
		}
		return s.attachPasswordIdentity(ctx, existingUser, input.Password)
	}
	if !errors.Is(err, msgs.ErrUserNotFound) {
		return models.User{}, models.TokenPair{}, err
	}

	passwordHash, err := hash.HashPassword(input.Password)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	var createdUser models.User

	err = s.WithinTx(ctx, func(ctx context.Context) error {
		userID := uuid.New()

		user, err := s.User().CreateUser(ctx, models.User{
			ID:    userID,
			Email: email,
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
		return models.User{}, models.TokenPair{}, err
	}

	// New registrations are always on the free plan — no extra query needed.
	pair, err := s.issueTokenPair(ctx, createdUser.ID, models.FreePlan.String())
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	return createdUser, pair, nil
}

// attachPasswordIdentity is called by Register when the requested email
// already belongs to an existingUser. If that account already has a
// password identity, registration is a genuine duplicate and rejected with
// msgs.ErrEmailAlreadyExists. Otherwise the account is Google-only (per the
// account-linking policy: Google's email is verified before such an account
// can exist at all, so attaching by email match is safe here) and a new
// password identity is attached to it rather than creating a second
// account; the caller is signed in immediately. existingUser's Name/AvatarURL
// are left untouched.
func (s *authService) attachPasswordIdentity(ctx context.Context, existingUser models.User, password string) (models.User, models.TokenPair, error) {
	_, err := s.AuthIdentity().GetAuthIdentityByUserIDAndProvider(ctx, existingUser.ID, "password")
	if err == nil {
		return models.User{}, models.TokenPair{}, msgs.ErrEmailAlreadyExists
	}
	if !errors.Is(err, msgs.ErrUserNotFound) {
		return models.User{}, models.TokenPair{}, err
	}

	passwordHash, err := hash.HashPassword(password)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	_, err = s.AuthIdentity().CreateAuthIdentity(ctx, models.AuthIdentity{
		ID:              uuid.New(),
		UserID:          existingUser.ID,
		Provider:        "password",
		ProviderSubject: existingUser.Email,
		PasswordHash:    &passwordHash,
	})
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, existingUser.ID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}
	pair, err := s.issueTokenPair(ctx, existingUser.ID, sub.PlanID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}
	return existingUser, pair, nil
}

// reactivateAccount is called by Register when the requested email belongs
// to a previously soft-deleted account (deletedUser.DeletedAt is set).
// users.email is a permanent UNIQUE constraint — deliberately never relaxed
// on deletion, so a deleted account's email is retained for record-keeping —
// so a second Register with that email can never create a new account; this
// treats it as an intentional restoration instead of a rejection: clears
// deleted_at, sets the password identity to the newly supplied password
// (updating it if one already existed, or creating one if the account had
// been Google-only), and signs the caller in as the same account, same ID,
// same history.
func (s *authService) reactivateAccount(ctx context.Context, deletedUser models.User, password string) (models.User, models.TokenPair, error) {
	passwordHash, err := hash.HashPassword(password)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	err = s.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.User().Reactivate(ctx, deletedUser.ID); err != nil {
			return err
		}

		_, err := s.AuthIdentity().GetAuthIdentityByUserIDAndProvider(ctx, deletedUser.ID, "password")
		if err == nil {
			return s.AuthIdentity().UpdatePasswordHash(ctx, deletedUser.ID, passwordHash)
		}
		if !errors.Is(err, msgs.ErrUserNotFound) {
			return err
		}

		_, err = s.AuthIdentity().CreateAuthIdentity(ctx, models.AuthIdentity{
			ID:              uuid.New(),
			UserID:          deletedUser.ID,
			Provider:        "password",
			ProviderSubject: deletedUser.Email,
			PasswordHash:    &passwordHash,
		})
		return err
	})
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	reactivatedUser, err := s.User().GetUserByID(ctx, deletedUser.ID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}
	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, deletedUser.ID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}
	pair, err := s.issueTokenPair(ctx, reactivatedUser.ID, sub.PlanID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}
	return reactivatedUser, pair, nil
}

// GoogleAuthURL returns the URL to redirect the user to Google's consent
// screen, plus a freshly generated CSRF state value the caller must persist
// (e.g. in a signed cookie) and present back to GoogleCallback.
func (s *authService) GoogleAuthURL(ctx context.Context) (string, string, error) {
	state, err := token.Generate()
	if err != nil {
		return "", "", err
	}
	return s.google.AuthURL(state), state, nil
}

// GoogleCallback exchanges the authorization code for Google's verified
// profile and signs the user in. An account with a matching google identity
// signs in as-is; no google identity but a matching verified email attaches
// a new google identity to that existing account (see attachPasswordIdentity
// for the mirror-image case on Register); no match at all creates a new
// user, google identity, and free-plan subscription. Returns
// msgs.ErrOAuthEmailNotVerified if Google reports the email unverified.
func (s *authService) GoogleCallback(ctx context.Context, code string) (models.User, models.TokenPair, error) {
	accessToken, err := s.google.Exchange(ctx, code)
	if err != nil {
		return models.User{}, models.TokenPair{}, fmt.Errorf("exchanging google authorization code: %w", err)
	}

	info, err := s.google.FetchUserInfo(ctx, accessToken)
	if err != nil {
		return models.User{}, models.TokenPair{}, fmt.Errorf("fetching google user info: %w", err)
	}
	if !info.EmailVerified {
		return models.User{}, models.TokenPair{}, msgs.ErrOAuthEmailNotVerified
	}
	email := validate.NormalizeEmail(info.Email)

	identity, err := s.AuthIdentity().GetAuthIdentityByProviderAndSubject(ctx, "google", info.Subject)
	if err == nil {
		user, err := s.User().GetUserByID(ctx, identity.UserID)
		if err != nil {
			return models.User{}, models.TokenPair{}, err
		}
		sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, user.ID)
		if err != nil {
			return models.User{}, models.TokenPair{}, err
		}
		pair, err := s.issueTokenPair(ctx, user.ID, sub.PlanID)
		if err != nil {
			return models.User{}, models.TokenPair{}, err
		}
		return user, pair, nil
	}
	if !errors.Is(err, msgs.ErrUserNotFound) {
		return models.User{}, models.TokenPair{}, err
	}

	existingUser, err := s.User().GetUserByEmail(ctx, email)
	if err == nil {
		_, err = s.AuthIdentity().CreateAuthIdentity(ctx, models.AuthIdentity{
			ID:              uuid.New(),
			UserID:          existingUser.ID,
			Provider:        "google",
			ProviderSubject: info.Subject,
		})
		if err != nil {
			return models.User{}, models.TokenPair{}, err
		}
		sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, existingUser.ID)
		if err != nil {
			return models.User{}, models.TokenPair{}, err
		}
		pair, err := s.issueTokenPair(ctx, existingUser.ID, sub.PlanID)
		if err != nil {
			return models.User{}, models.TokenPair{}, err
		}
		return existingUser, pair, nil
	}
	if !errors.Is(err, msgs.ErrUserNotFound) {
		return models.User{}, models.TokenPair{}, err
	}

	var createdUser models.User
	verifiedAt := time.Now()

	err = s.WithinTx(ctx, func(ctx context.Context) error {
		newUserID := uuid.New()

		// Name/AvatarURL are intentionally left unset here, exactly like the
		// password Register path — account creation never touches profile
		// fields, regardless of provider. Google's info.Name/info.Picture
		// are discarded rather than seeded, so there's no path by which
		// creating an account (via any provider) can populate or override
		// profile data; that only ever happens via UserService.UpdateProfile.
		user, err := s.User().CreateUser(ctx, models.User{
			ID:              newUserID,
			Email:           email,
			EmailVerifiedAt: &verifiedAt,
		})
		if err != nil {
			return err
		}
		createdUser = user

		_, err = s.AuthIdentity().CreateAuthIdentity(ctx, models.AuthIdentity{
			ID:              uuid.New(),
			UserID:          newUserID,
			Provider:        "google",
			ProviderSubject: info.Subject,
		})
		if err != nil {
			return err
		}

		plan := models.CreatePlan(models.FreePlan)
		_, err = s.Subscription().CreateUserSubscription(ctx, models.Subscription{
			ID:     uuid.New(),
			UserID: newUserID,
			PlanID: plan.Name,
			Status: "active",
		})
		return err
	})
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	pair, err := s.issueTokenPair(ctx, createdUser.ID, models.FreePlan.String())
	if err != nil {
		return models.User{}, models.TokenPair{}, err
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
// failures are returned wrapped. Returns msgs.ErrTooManyLoginAttempts if the
// normalized email has exceeded its login attempt limit, independent of the
// per-IP rate limit already applied to the route.
func (s *authService) Login(ctx context.Context, input models.LoginInput) (models.User, models.TokenPair, error) {
	email := validate.NormalizeEmail(input.Email)

	if !validate.Email(email) {
		return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
	}
	if !validate.Password(input.Password) {
		return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
	}

	// Keyed on the normalized email, independent of the caller's IP — closes
	// the gap where an attacker distributes login attempts against one
	// account across many IPs. Increments on every attempt, successful or
	// not, so a correct guess can't be used to reset the counter.
	allowed, err := s.loginLimiter.Allow(ctx, email)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}
	if !allowed {
		return models.User{}, models.TokenPair{}, msgs.ErrTooManyLoginAttempts
	}

	identity, err := s.AuthIdentity().GetAuthIdentityByProviderAndSubject(ctx, "password", email)
	if err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
		}
		return models.User{}, models.TokenPair{}, err
	}

	// An identity without a password hash (e.g. an OAuth-only account) cannot
	// authenticate by password; return the generic error rather than
	// msgs.ErrPasswordNotSet so login never reveals that the account exists.
	if identity.PasswordHash == nil {
		return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
	}

	ok, err := hash.VerifyPassword(input.Password, *identity.PasswordHash)
	if err != nil {
		return models.User{}, models.TokenPair{}, fmt.Errorf("error verifying password: %w", err)
	}
	if !ok {
		return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
	}

	// auth_identities isn't touched by account deletion (only users.deleted_at
	// is set), so a correct password can still be verified above for a
	// deleted account. GetUserByID's deleted_at filter then reports
	// ErrUserNotFound — translate that to the same generic
	// ErrInvalidCredentials as every other login failure, rather than
	// leaking a distinguishable error that would reveal the account used to
	// exist (breaking the enumeration defense this function otherwise
	// maintains throughout).
	user, err := s.User().GetUserByID(ctx, identity.UserID)
	if err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return models.User{}, models.TokenPair{}, msgs.ErrInvalidCredentials
		}
		return models.User{}, models.TokenPair{}, err
	}

	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, user.ID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	pair, err := s.issueTokenPair(ctx, user.ID, sub.PlanID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
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
func (s *authService) Refresh(ctx context.Context, rawRefreshToken string) (models.User, models.TokenPair, error) {
	tokenHash := token.Hash(rawRefreshToken)

	session, err := s.Session().GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return models.User{}, models.TokenPair{}, err // msgs.ErrTokenInvalid from repo
	}

	// A revoked_at that is set means this session was already consumed via
	// rotation (or explicitly revoked). Presenting its token is a reuse
	// signal — revoke the whole token family defensively.
	if session.RevokedAt != nil {
		if ids, err := s.Session().RevokeSessionFamily(ctx, session.TokenFamilyID); err == nil {
			_ = s.sessions.RevokeSessions(ctx, ids)
		}
		return models.User{}, models.TokenPair{}, msgs.ErrTokenReuseDetected
	}

	if time.Now().After(session.ExpiresAt) {
		return models.User{}, models.TokenPair{}, msgs.ErrTokenInvalid
	}

	user, err := s.User().GetUserByID(ctx, session.UserID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, user.ID)
	if err != nil {
		return models.User{}, models.TokenPair{}, err
	}

	// Rotate inside a transaction: consume the old session and create the new
	// one atomically so there is never a window with two valid tokens.
	var pair models.TokenPair
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
		return models.User{}, models.TokenPair{}, err
	}

	return user, pair, nil
}

// Logout revokes the session with the given ID, invalidating its refresh
// token and immediately invalidating its access token via the shared
// session-revocation store. It is idempotent: revoking an already-revoked
// session succeeds silently.
func (s *authService) Logout(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.Session().RevokeSession(ctx, sessionID); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	if err := s.sessions.RevokeSession(ctx, sessionID); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// LogoutAll revokes every active session belonging to userID, signing the
// user out of all devices, invalidating all refresh-token families, and
// immediately invalidating every access token via the shared
// session-revocation store.
func (s *authService) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	ids, err := s.Session().RevokeAllSessionsForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("logout all: %w", err)
	}
	if err := s.sessions.RevokeSessions(ctx, ids); err != nil {
		return fmt.Errorf("logout all: %w", err)
	}
	return nil
}

// RequestEmailVerification issues a new email verification token for email
// and sends it via EmailService. Unknown or already-verified emails are a
// silent no-op so the caller can always respond generically.
func (s *authService) RequestEmailVerification(ctx context.Context, email string) error {
	email = validate.NormalizeEmail(email)
	if !validate.Email(email) {
		return nil
	}

	user, err := s.User().GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return nil
		}
		return err
	}
	if user.EmailVerifiedAt != nil {
		return nil
	}

	rawToken, err := token.Generate()
	if err != nil {
		return err
	}

	_, err = s.EmailVerificationToken().CreateEmailVerificationToken(ctx, models.EmailVerificationToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: token.Hash(rawToken),
		ExpiresAt: time.Now().Add(emailVerificationTokenDuration),
	})
	if err != nil {
		return err
	}

	if err := s.email.SendVerificationEmail(ctx, email, rawToken); err != nil {
		return fmt.Errorf("sending verification email: %w", err)
	}
	return nil
}

// VerifyEmail consumes an email verification token: it validates the token,
// marks the owning user's email verified, consumes the token, and returns
// the user, their active subscription, and whether they have a password
// identity.
func (s *authService) VerifyEmail(ctx context.Context, rawToken string) (models.User, models.Subscription, bool, error) {
	tokenHash := token.Hash(rawToken)

	t, err := s.EmailVerificationToken().GetEmailVerificationTokenByHash(ctx, tokenHash)
	if err != nil {
		return models.User{}, models.Subscription{}, false, err // msgs.ErrTokenInvalid from repo
	}
	if t.UsedAt != nil {
		return models.User{}, models.Subscription{}, false, msgs.ErrTokenAlreadyUsed
	}
	if time.Now().After(t.ExpiresAt) {
		return models.User{}, models.Subscription{}, false, msgs.ErrTokenInvalid
	}
	if _, err := s.User().GetUserByID(ctx, t.UserID); err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return models.User{}, models.Subscription{}, false, msgs.ErrTokenInvalid
		}
		return models.User{}, models.Subscription{}, false, err
	}

	err = s.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.User().UpdateEmailVerifiedAt(ctx, t.UserID); err != nil {
			return err
		}
		return s.EmailVerificationToken().MarkEmailVerificationTokenConsumed(ctx, t.ID)
	})
	if err != nil {
		return models.User{}, models.Subscription{}, false, err
	}

	user, err := s.User().GetUserByID(ctx, t.UserID)
	if err != nil {
		return models.User{}, models.Subscription{}, false, err
	}
	sub, err := s.Subscription().GetActiveSubscriptionByUserID(ctx, t.UserID)
	if err != nil {
		return models.User{}, models.Subscription{}, false, err
	}
	hasPassword, err := hasPasswordIdentity(ctx, s.Repository, t.UserID)
	if err != nil {
		return models.User{}, models.Subscription{}, false, err
	}
	return user, sub, hasPassword, nil
}

// RequestPasswordReset issues a new password reset token for email and
// sends it via EmailService. An unknown email is a silent no-op so the
// caller can always respond generically.
func (s *authService) RequestPasswordReset(ctx context.Context, email string) error {
	email = validate.NormalizeEmail(email)
	if !validate.Email(email) {
		return nil
	}

	user, err := s.User().GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return nil
		}
		return err
	}

	rawToken, err := token.Generate()
	if err != nil {
		return err
	}

	_, err = s.PasswordResetToken().CreatePasswordResetToken(ctx, models.PasswordResetToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: token.Hash(rawToken),
		ExpiresAt: time.Now().Add(passwordResetTokenDuration),
	})
	if err != nil {
		return err
	}

	if err := s.email.SendPasswordResetEmail(ctx, email, rawToken); err != nil {
		return fmt.Errorf("sending password reset email: %w", err)
	}
	return nil
}

// ResetPassword consumes a password reset token: it validates the token and
// newPassword, replaces the account's password hash, consumes the token,
// and revokes every active session for the account.
func (s *authService) ResetPassword(ctx context.Context, rawToken string, newPassword string) error {
	if !validate.Password(newPassword) {
		return msgs.ErrInvalidCredentials
	}

	tokenHash := token.Hash(rawToken)

	t, err := s.PasswordResetToken().GetPasswordResetTokenByHash(ctx, tokenHash)
	if err != nil {
		return err // msgs.ErrTokenInvalid from repo
	}
	if t.UsedAt != nil {
		return msgs.ErrTokenAlreadyUsed
	}
	if time.Now().After(t.ExpiresAt) {
		return msgs.ErrTokenInvalid
	}
	if _, err := s.User().GetUserByID(ctx, t.UserID); err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return msgs.ErrTokenInvalid
		}
		return err
	}

	identity, err := s.AuthIdentity().GetAuthIdentityByUserIDAndProvider(ctx, t.UserID, "password")
	if err != nil {
		if errors.Is(err, msgs.ErrUserNotFound) {
			return msgs.ErrPasswordNotSet
		}
		return err
	}
	if identity.PasswordHash == nil {
		return msgs.ErrPasswordNotSet
	}

	newHash, err := hash.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hashing new password: %w", err)
	}

	var revokedSessionIDs []uuid.UUID
	err = s.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.AuthIdentity().UpdatePasswordHash(ctx, t.UserID, newHash); err != nil {
			return err
		}
		if err := s.PasswordResetToken().MarkPasswordResetTokenConsumed(ctx, t.ID); err != nil {
			return err
		}
		ids, err := s.Session().RevokeAllSessionsForUser(ctx, t.UserID)
		if err != nil {
			return err
		}
		revokedSessionIDs = ids
		return nil
	})
	if err != nil {
		return err
	}
	return s.sessions.RevokeSessions(ctx, revokedSessionIDs)
}

// issueTokenPair generates a fresh token pair starting a new token family.
// Called at the end of registration and login.
func (s *authService) issueTokenPair(ctx context.Context, userID uuid.UUID, planKey string) (models.TokenPair, error) {
	return s.issueTokenPairInFamily(ctx, userID, planKey, uuid.New())
}

// issueTokenPairInFamily generates a new refresh token + session in the given
// token family, signs a short-lived JWT access token, and returns both as a
// models.TokenPair. familyID is uuid.New() on first issuance and the existing
// family UUID on rotation, preserving the revocation lineage across
// refreshes. The planKey is embedded in the JWT so protected handlers can
// authorize without a database round-trip.
func (s *authService) issueTokenPairInFamily(ctx context.Context, userID uuid.UUID, planKey string, familyID uuid.UUID) (models.TokenPair, error) {
	rawToken, err := token.Generate()
	if err != nil {
		return models.TokenPair{}, err
	}

	session, err := s.Session().CreateSession(ctx, models.Session{
		ID:               uuid.New(),
		UserID:           userID,
		RefreshTokenHash: token.Hash(rawToken),
		TokenFamilyID:    familyID,
		ExpiresAt:        time.Now().Add(sessionDuration),
	})
	if err != nil {
		return models.TokenPair{}, err
	}

	accessToken, err := jwttoken.Issue(s.cfg.JWTSecret, userID, session.ID, planKey)
	if err != nil {
		return models.TokenPair{}, err
	}

	return models.TokenPair{AccessToken: accessToken, RawRefreshToken: rawToken}, nil
}
