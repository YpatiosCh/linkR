-- name: CreateSession :one
INSERT INTO sessions (id, user_id, refresh_token_hash, token_family_id, expires_at, ip_address, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;