-- +goose Up
CREATE TABLE sessions (
    id                  UUID PRIMARY KEY,
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash  TEXT NOT NULL,
    token_family_id     UUID NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at          TIMESTAMPTZ NOT NULL,
    last_used_at        TIMESTAMPTZ,
    revoked_at          TIMESTAMPTZ,
    ip_address          TEXT,
    user_agent          TEXT
);

CREATE INDEX idx_sessions_user_id ON sessions (user_id);
CREATE UNIQUE INDEX idx_sessions_refresh_token_hash ON sessions (refresh_token_hash);
CREATE INDEX idx_sessions_token_family_id ON sessions (token_family_id);

-- +goose Down
DROP TABLE sessions;
