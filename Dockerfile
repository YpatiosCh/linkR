# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

FROM alpine:3.20
# ca-certificates is required for the outbound HTTPS calls this server makes
# (Resend API, Google's token/userinfo endpoints).
RUN apk add --no-cache ca-certificates

COPY --from=builder /out/server /app/server

EXPOSE 8080
ENTRYPOINT ["/app/server"]
