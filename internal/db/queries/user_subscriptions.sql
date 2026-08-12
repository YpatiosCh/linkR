-- name: CreateUserSubscription :one
INSERT INTO user_subscriptions (id, user_id, plan_id, status)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetActiveSubscriptionByUserID :one
SELECT * FROM user_subscriptions
WHERE user_id = $1 AND status = 'active'
ORDER BY created_at DESC
LIMIT 1;
