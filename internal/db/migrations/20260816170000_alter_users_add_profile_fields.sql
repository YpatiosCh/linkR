-- +goose Up
ALTER TABLE users ALTER COLUMN name DROP NOT NULL;
ALTER TABLE users ADD COLUMN company_name TEXT;
ALTER TABLE users ADD COLUMN description TEXT;
ALTER TABLE users ADD COLUMN social_links JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE users DROP COLUMN social_links;
ALTER TABLE users DROP COLUMN description;
ALTER TABLE users DROP COLUMN company_name;
ALTER TABLE users ALTER COLUMN name SET NOT NULL;
