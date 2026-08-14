-- name: CreateEmailVerificationToken :one
INSERT INTO email_verification_tokens (id, user_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetEmailVerificationTokenByHash :one
SELECT * FROM email_verification_tokens WHERE token_hash = $1;

-- name: MarkEmailVerificationTokenConsumed :exec
UPDATE email_verification_tokens SET used_at = now() WHERE id = $1;
