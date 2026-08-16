package models

// RegisterRequest is the JSON body expected by POST /api/v1/auth/register.
// Registration intentionally collects only Email and Password — profile
// details (name, avatar, etc.) are set afterward via
// PATCH /api/v1/me/profile.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest is the JSON body expected by POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// PasswordChangeRequest is the JSON body expected by POST /api/v1/me/password/change.
type PasswordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// SetPasswordRequest is the JSON body expected by POST /api/v1/me/password/set —
// used to set an initial password on an account that doesn't have one yet
// (e.g. an OAuth-only account), unlike PasswordChangeRequest which rotates
// an existing one.
type SetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// DeleteAccountRequest is the JSON body expected by DELETE /api/v1/me.
// CurrentPassword is required only for accounts that have a password
// identity (verified as a confirmation step before this destructive,
// irreversible action); it's ignored for OAuth-only accounts, which have
// nothing to verify it against.
type DeleteAccountRequest struct {
	CurrentPassword string `json:"current_password,omitempty"`
}

// RequestEmailVerificationRequest is the JSON body expected by
// POST /api/v1/auth/email/verification/request.
type RequestEmailVerificationRequest struct {
	Email string `json:"email"`
}

// VerifyEmailRequest is the JSON body expected by
// POST /api/v1/auth/email/verification/verify.
type VerifyEmailRequest struct {
	Token string `json:"token"`
}

// RequestPasswordResetRequest is the JSON body expected by
// POST /api/v1/auth/password/reset/request.
type RequestPasswordResetRequest struct {
	Email string `json:"email"`
}

// ResetPasswordRequest is the JSON body expected by
// POST /api/v1/auth/password/reset/confirm.
type ResetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// UpdateProfileRequest is the JSON body expected by
// PATCH /api/v1/me/profile. Every field is a pointer: an omitted field is
// left unchanged; an explicitly-sent field (including an empty string)
// replaces the current value. SocialLinks, when present, replaces the
// entire social-links struct rather than merging.
type UpdateProfileRequest struct {
	Name        *string      `json:"name,omitempty"`
	AvatarURL   *string      `json:"avatar_url,omitempty"`
	CompanyName *string      `json:"company_name,omitempty"`
	Description *string      `json:"description,omitempty"`
	SocialLinks *SocialLinks `json:"social_links,omitempty"`
}
