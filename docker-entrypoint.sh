#!/bin/sh
set -e

echo "Running database migrations..."
/app/goose -dir /app/migrations postgres "$DATABASE_URL" up

exec /app/server
