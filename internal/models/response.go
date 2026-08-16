package models

import "time"

// AuthResponse is the JSON body returned on a successful registration or login.
// expires_at reports when the access token expires so clients can schedule a
// proactive background refresh before the token lapses. Name is omitted
// until the user sets it via UserService.UpdateProfile — registration
// itself never collects it.
type AuthResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      *string   `json:"name,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RefreshResponse is the JSON body returned on a successful token refresh.
// It reports when the new access token expires so clients can reschedule
// their background refresh timer.
type RefreshResponse struct {
	ExpiresAt time.Time `json:"expires_at"`
}

// MePlanResponse is the plan sub-object inside MeResponse.
type MePlanResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// MeResponse is the JSON body returned by GET /api/v1/me, wrapped under a
// "data" key. It exposes only safe account fields — never hashes or tokens.
// It is also reused by POST /api/v1/auth/email/verification/verify, which
// returns the same "public account representation" per the auth spec.
type MeResponse struct {
	ID            string      `json:"id"`
	Email         string      `json:"email"`
	EmailVerified bool        `json:"email_verified"`
	Name          *string     `json:"name,omitempty"`
	AvatarURL     *string     `json:"avatar_url,omitempty"`
	CompanyName   *string     `json:"company_name,omitempty"`
	Description   *string     `json:"description,omitempty"`
	SocialLinks   SocialLinks `json:"social_links,omitempty"`
	// HasPassword reports whether the account has a password identity —
	// false for an OAuth-only account. Not omitempty: the frontend needs to
	// reliably tell "false" apart from "field absent" to decide whether to
	// offer "set a password" or "change password".
	HasPassword bool           `json:"has_password"`
	Plan        MePlanResponse `json:"plan"`
}

// MessageResponse is the JSON body returned by the intentionally generic
// "request" endpoints (email verification request, password reset request),
// wrapped under a "data" key. It never reveals whether the requested email
// exists.
type MessageResponse struct {
	Message string `json:"message"`
}
