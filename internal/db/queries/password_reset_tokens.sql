-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetPasswordResetTokenByHash :one
SELECT * FROM password_reset_tokens WHERE token_hash = $1;

-- name: MarkPasswordResetTokenConsumed :exec
UPDATE password_reset_tokens SET used_at = now() WHERE id = $1;
