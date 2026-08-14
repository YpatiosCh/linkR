package models

// RegisterRequest is the JSON body expected by POST /api/v1/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
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
