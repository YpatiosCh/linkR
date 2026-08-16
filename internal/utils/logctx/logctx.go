// Package logctx propagates a request-scoped *slog.Logger through a
// context.Context. It exists as the shared seam between
// internal/middleware (which injects the logger) and
// internal/utils/response (which reads it) — internal/middleware already
// imports internal/utils/response, so response cannot import middleware
// back without an import cycle; logctx has no further internal
// dependencies and can be imported by both.
package logctx

import (
	"context"
	"log/slog"
)

// loggerKey is the unexported type used to store a request-scoped
// *slog.Logger in a context.Context, preventing collisions with other
// context values.
type loggerKey struct{}

// WithLogger returns a copy of ctx carrying logger, retrievable via
// FromContext.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// FromContext retrieves the logger injected by WithLogger, or
// slog.Default() if none was injected. Always returns a usable logger, so
// it's safe to call even outside a request that went through the
// request-logging middleware.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}
