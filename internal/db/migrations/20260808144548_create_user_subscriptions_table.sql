-- +goose Up
CREATE TABLE user_subscriptions (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id     TEXT NOT NULL REFERENCES plans(id),
    status      TEXT NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    ends_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_subscriptions_user_id ON user_subscriptions (user_id);
CREATE UNIQUE INDEX idx_user_subscriptions_active_per_user
    ON user_subscriptions (user_id)
    WHERE status = 'active';

-- +goose Down
DROP TABLE user_subscriptions;