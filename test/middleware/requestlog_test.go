package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linkMe/internal/middleware"
)

// decodeLogLines splits buf's content on newlines and JSON-decodes each
// non-empty line into a map, for asserting on field presence/values without
// depending on exact message wording.
func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("decodeLogLines: invalid JSON line %q: %v", raw, err)
		}
		lines = append(lines, m)
	}
	return lines
}

func TestRequestLogger_SetsRequestIDHeaderAndDiffersAcrossRequests(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.RequestLogger(base)(inner)

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/health", nil))
	id1 := rec1.Header().Get("X-Request-ID")
	if id1 == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/health", nil))
	id2 := rec2.Header().Get("X-Request-ID")
	if id2 == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}

	if id1 == id2 {
		t.Errorf("expected distinct request IDs across requests, got %q twice", id1)
	}
}

func TestRequestLogger_EmitsAccessLogLine(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	handler := middleware.RequestLogger(base)(inner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	lines := decodeLogLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 log line, got %d", len(lines))
	}
	line := lines[0]

	for _, field := range []string{"method", "path", "status", "duration_ms", "request_id"} {
		if _, ok := line[field]; !ok {
			t.Errorf("expected log line to contain field %q, got %v", field, line)
		}
	}
	if line["method"] != http.MethodPost {
		t.Errorf("method: got %v, want %v", line["method"], http.MethodPost)
	}
	if line["path"] != "/api/v1/auth/register" {
		t.Errorf("path: got %v, want %v", line["path"], "/api/v1/auth/register")
	}
	if line["status"] != float64(http.StatusCreated) {
		t.Errorf("status: got %v, want %v", line["status"], http.StatusCreated)
	}
	if line["request_id"] != rec.Header().Get("X-Request-ID") {
		t.Errorf("request_id log field %v does not match X-Request-ID header %v", line["request_id"], rec.Header().Get("X-Request-ID"))
	}
}

func TestRequestLogger_InjectsLoggerIntoContext(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		middleware.LoggerFromContext(r).Info("inner handler log")
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.RequestLogger(base)(inner)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	lines := decodeLogLines(t, &buf)
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines (inner + access log), got %d", len(lines))
	}

	requestID := rec.Header().Get("X-Request-ID")
	for _, line := range lines {
		if line["request_id"] != requestID {
			t.Errorf("expected every log line to carry request_id %v, got %v", requestID, line["request_id"])
		}
	}
}

func TestRequestLogger_DefaultsStatusTo200WhenWriteHeaderNotCalled(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	handler := middleware.RequestLogger(base)(inner)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	lines := decodeLogLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 log line, got %d", len(lines))
	}
	if lines[0]["status"] != float64(http.StatusOK) {
		t.Errorf("status: got %v, want %v", lines[0]["status"], http.StatusOK)
	}
}

func TestLoggerFromContext_FallsBackOutsideMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	logger := middleware.LoggerFromContext(req)
	if logger == nil {
		t.Fatal("expected LoggerFromContext to never return nil")
	}
}
