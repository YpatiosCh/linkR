package response

const (
	// CodeInvalidBody indicates the request body could not be parsed or did not match the expected shape.
	CodeInvalidBody = "INVALID_BODY"
	// CodeInvalidInput indicates the supplied input failed validation.
	CodeInvalidInput = "INVALID_INPUT"
	// CodeInvalidCredentials indicates the provided email or password did not match an existing account.
	CodeInvalidCredentials = "INVALID_CREDENTIALS"
	// CodeEmailAlreadyExists indicates registration was attempted with an email that is already in use.
	CodeEmailAlreadyExists = "EMAIL_ALREADY_EXISTS"
	// CodeUserNotFound indicates no user matched the requested identifier.
	CodeUserNotFound = "USER_NOT_FOUND"
	// CodePasswordNotSet indicates the account has no password configured (e.g. an OAuth-only account).
	CodePasswordNotSet = "PASSWORD_NOT_SET"
	// CodeTokenInvalid indicates a token was missing, invalid, or expired.
	CodeTokenInvalid = "TOKEN_INVALID"
	// CodeTokenAlreadyUsed indicates a one-time token that has already been consumed.
	CodeTokenAlreadyUsed = "TOKEN_ALREADY_USED"
	// CodeSessionRevoked indicates the session has been revoked.
	CodeSessionRevoked = "SESSION_REVOKED"
	// CodeTokenReuseDetected indicates reuse of a rotated refresh token.
	CodeTokenReuseDetected = "TOKEN_REUSE_DETECTED"
	// CodeInternalError indicates an unexpected server-side failure.
	CodeInternalError = "INTERNAL_ERROR"
	// CodeUnauthorized indicates a request reached a protected resource without valid credentials.
	CodeUnauthorized = "UNAUTHORIZED"
	// CodeTooManyRequests indicates the client has exceeded the rate limit for this endpoint.
	CodeTooManyRequests = "TOO_MANY_REQUESTS"
)
