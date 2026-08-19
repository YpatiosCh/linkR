package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"linkMe/internal/middleware"
	"linkMe/internal/utils/logctx"
)

func TestRecover_PanickingHandlerReturns500InternalError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	handler := middleware.Recover()(inner)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req = req.WithContext(logctx.WithLogger(req.Context(), logger))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	if body.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("error code: got %q, want %q", body.Error.Code, "INTERNAL_ERROR")
	}

	lines := decodeLogLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 log line, got %d", len(lines))
	}
	if lines[0]["level"] != "ERROR" {
		t.Errorf("log level: got %v, want ERROR", lines[0]["level"])
	}
	if lines[0]["panic"] != "boom" {
		t.Errorf("panic field: got %v, want %q", lines[0]["panic"], "boom")
	}
	if _, ok := lines[0]["stack"]; !ok {
		t.Error("expected log line to contain a stack field")
	}
}

func TestRecover_NonPanickingHandlerPassesThrough(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("ok"))
	})
	handler := middleware.Recover()(inner)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req = req.WithContext(logctx.WithLogger(req.Context(), logger))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body: got %q, want %q", rec.Body.String(), "ok")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output, got %q", buf.String())
	}
}

func TestRecover_ComposedWithRequestLoggerStillLogsAccessLine(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	handler := middleware.RequestLogger(base)(middleware.Recover()(inner))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	lines := decodeLogLines(t, &buf)
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines (panic + access log), got %d", len(lines))
	}

	accessLine := lines[len(lines)-1]
	if accessLine["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("access log status: got %v, want %v", accessLine["status"], http.StatusInternalServerError)
	}
}
