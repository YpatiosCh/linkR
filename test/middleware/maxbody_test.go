package middleware_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"linkMe/internal/middleware"
)

func TestMaxBody_UnderLimitPasses(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading body under the limit should not error: %v", err)
		}
		w.Write(body)
	})
	handler := middleware.MaxBody(10)(inner)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("small"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Body.String() != "small" {
		t.Errorf("expected body to pass through unchanged, got %q", rec.Body.String())
	}
}

func TestMaxBody_OverLimitFails(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err == nil {
			t.Error("expected reading past the limit to error")
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.MaxBody(5)(inner)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("this is definitely over five bytes"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}
