-- name: CreateUserSubscription :one
INSERT INTO user_subscriptions (id, user_id, plan_id, status)
VALUES ($1, $2, $3, $4)
RETURNING *;