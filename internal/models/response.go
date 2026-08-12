package models

import "time"

// UserResponse is the JSON body returned on a successful registration or login.
type UserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// RefreshResponse is the JSON body returned on a successful token refresh.
// It reports when the new access token expires so clients know when to refresh.
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
type MeResponse struct {
	ID            string         `json:"id"`
	Email         string         `json:"email"`
	EmailVerified bool           `json:"email_verified"`
	Name          string         `json:"name"`
	AvatarURL     *string        `json:"avatar_url,omitempty"`
	Plan          MePlanResponse `json:"plan"`
}
