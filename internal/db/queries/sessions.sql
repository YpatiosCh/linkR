-- name: CreateSession :one
INSERT INTO sessions (id, user_id, refresh_token_hash, token_family_id, expires_at, ip_address, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE refresh_token_hash = $1;

-- name: MarkSessionConsumed :exec
UPDATE sessions SET revoked_at = now() WHERE id = $1;

-- name: RevokeSessionFamily :many
UPDATE sessions SET revoked_at = now() WHERE token_family_id = $1 AND revoked_at IS NULL RETURNING id;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE id = $1;

-- name: RevokeAllSessionsForUser :many
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL RETURNING id;

-- name: RevokeOtherSessionsForUser :many
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND id != $2 AND revoked_at IS NULL RETURNING id;
