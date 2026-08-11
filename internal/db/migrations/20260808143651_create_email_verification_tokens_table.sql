-- +goose Up
CREATE TABLE email_verification_tokens (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (token_hash)
);

CREATE INDEX idx_email_verification_tokens_user_id ON email_verification_tokens (user_id);

-- +goose Down
DROP TABLE email_verification_tokens;