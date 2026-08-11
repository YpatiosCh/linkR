-- name: CreateAuthIdentity :one
INSERT INTO auth_identities (id, user_id, provider, provider_subject, password_hash)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAuthIdentityByProviderAndSubject :one
SELECT *
FROM auth_identities
WHERE provider = $1
  AND provider_subject = $2;