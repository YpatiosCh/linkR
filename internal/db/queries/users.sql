-- name: CreateUser :one
INSERT INTO users (id, email, name, avatar_url)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = $1
  AND deleted_at IS NULL;

-- name: GetUserByEmailIncludingDeleted :one
SELECT *
FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = $1
  AND deleted_at IS NULL;

-- name: UpdateEmailVerifiedAt :exec
UPDATE users SET email_verified_at = now() WHERE id = $1;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = now(), updated_at = now() WHERE id = $1;

-- name: ReactivateUser :exec
UPDATE users SET deleted_at = NULL, updated_at = now() WHERE id = $1;

-- name: UpdateUserProfile :one
UPDATE users
SET
    name         = COALESCE(sqlc.narg('name'), name),
    avatar_url   = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    company_name = COALESCE(sqlc.narg('company_name'), company_name),
    description  = COALESCE(sqlc.narg('description'), description),
    social_links = COALESCE(sqlc.narg('social_links'), social_links),
    updated_at   = now()
WHERE id = sqlc.arg('id')
  AND deleted_at IS NULL
RETURNING *;