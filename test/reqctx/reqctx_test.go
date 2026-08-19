package reqctx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"linkMe/internal/utils/reqctx"
)

func TestWithMetaAndFromContext_Roundtrip(t *testing.T) {
	meta := reqctx.Meta{IP: "203.0.113.1", UserAgent: "test-agent/1.0", RequestID: "req-123"}
	ctx := reqctx.WithMeta(context.Background(), meta)

	got := reqctx.FromContext(ctx)
	if got != meta {
		t.Errorf("expected %+v, got %+v", meta, got)
	}
}

func TestFromContext_ZeroValueOutsideInjectedContext(t *testing.T) {
	got := reqctx.FromContext(context.Background())
	if got != (reqctx.Meta{}) {
		t.Errorf("expected zero-value Meta outside any injected context, got %+v", got)
	}
}

func TestClientIP_PrefersXRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:54321"
	req.Header.Set("X-Real-IP", "203.0.113.9")

	if got := reqctx.ClientIP(req); got != "203.0.113.9" {
		t.Errorf("expected X-Real-IP to take precedence, got %q", got)
	}
}

func TestClientIP_FallsBackToRemoteAddrWithPortStripped(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:54321"

	if got := reqctx.ClientIP(req); got != "203.0.113.1" {
		t.Errorf("expected port-stripped RemoteAddr, got %q", got)
	}
}
