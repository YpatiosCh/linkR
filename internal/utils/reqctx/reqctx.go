// Package reqctx propagates per-request metadata (client IP, User-Agent,
// request ID) through a context.Context. It exists as the shared seam
// between internal/middleware (which injects the metadata, via
// RequestLogger — the same place logctx's logger is injected) and any
// deeper layer that needs it without threading new parameters through
// every intervening function signature — today that's internal/service's
// audit logging, which reads IP/User-Agent/request ID to attach to each
// audit_events row.
package reqctx

import (
	"context"
	"net/http"
	"strings"
)

// Meta holds the per-request metadata carried through context.Context.
type Meta struct {
	// IP is the caller's address, from ClientIP.
	IP string
	// UserAgent is the request's User-Agent header, verbatim.
	UserAgent string
	// RequestID is the request's correlation ID, matching the value set on
	// the X-Request-ID response header and the request-scoped logger's
	// "request_id" field.
	RequestID string
}

// metaKey is the unexported type used to store Meta in a context.Context,
// preventing collisions with other context values.
type metaKey struct{}

// WithMeta returns a copy of ctx carrying meta, retrievable via FromContext.
func WithMeta(ctx context.Context, meta Meta) context.Context {
	return context.WithValue(ctx, metaKey{}, meta)
}

// FromContext retrieves the Meta injected by WithMeta, or the zero value
// (all empty strings) if none was injected — always safe to call, even
// outside a request that went through the request-logging middleware.
func FromContext(ctx context.Context) Meta {
	if meta, ok := ctx.Value(metaKey{}).(Meta); ok {
		return meta
	}
	return Meta{}
}

// ClientIP extracts the caller's IP from the request: X-Real-IP is
// preferred (set by a trusted reverse proxy), falling back to RemoteAddr
// with the port stripped.
func ClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i != -1 {
		return addr[:i]
	}
	return addr
}
