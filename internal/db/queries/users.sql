-- name: CreateUser :one
INSERT INTO users (id, email, name, avatar_url)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = $1
  AND deleted_at IS NULL;

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: UpdateEmailVerifiedAt :exec
UPDATE users SET email_verified_at = now() WHERE id = $1;