-- +goose Up
CREATE TABLE plans (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    price_monthly  INTEGER NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO plans (id, name, price_monthly) VALUES ('free', 'Free', 0);

-- +goose Down
DROP TABLE plans;