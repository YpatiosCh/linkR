# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

# goose is a build-time tool only, installed independently of the app's own
# module graph (go install with an explicit @version never touches this
# repo's go.mod/go.sum) — matches how it's used outside Docker (see
# DOCS/postman.md §1: "goose is installed locally but not wired into go.mod").
RUN CGO_ENABLED=0 GOOS=linux go install github.com/pressly/goose/v3/cmd/goose@v3.27.3 && \
    cp /go/bin/goose /out/goose

FROM alpine:3.20
# ca-certificates is required for the outbound HTTPS calls this server makes
# (Resend API, Google's token/userinfo endpoints).
RUN apk add --no-cache ca-certificates

COPY --from=builder /out/server /app/server
COPY --from=builder /out/goose /app/goose
COPY --from=builder /src/internal/db/migrations /app/migrations
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

EXPOSE 8080
ENTRYPOINT ["/app/docker-entrypoint.sh"]
