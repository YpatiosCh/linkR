package response_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linkMe/internal/msgs"
	"linkMe/internal/utils/logctx"
	"linkMe/internal/utils/response"
)

// requestWithLogger returns req with a *slog.Logger writing JSON to buf
// injected into its context, the same way middleware.RequestLogger would.
func requestWithLogger(req *http.Request, buf *bytes.Buffer) *http.Request {
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	return req.WithContext(logctx.WithLogger(req.Context(), logger))
}

// decodedLogLevel returns the "level" field of the single JSON log line
// written to buf.
func decodedLogLevel(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected a log line to have been written, got none")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatalf("decodedLogLevel: invalid JSON line %q: %v", line, err)
	}
	level, _ := decoded["level"].(string)
	return level
}

func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) response.ErrorBody {
	t.Helper()
	var envelope struct {
		Error response.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decodeErrorBody: invalid JSON body %q: %v", rec.Body.String(), err)
	}
	return envelope.Error
}

func TestHandleError_MappedSentinel(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	// No RequestLogger in the chain, so this also exercises the
	// logctx.FromContext fallback to slog.Default() for the mapped-sentinel
	// log line — must not panic.
	response.HandleError(rec, req, msgs.ErrUserNotFound)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusNotFound)
	}
	body := decodeErrorBody(t, rec)
	if body.Code != response.CodeUserNotFound {
		t.Errorf("code: got %q, want %q", body.Code, response.CodeUserNotFound)
	}
	if body.Message != msgs.ErrUserNotFound.Error() {
		t.Errorf("message: got %q, want %q", body.Message, msgs.ErrUserNotFound.Error())
	}
}

func TestHandleError_UnmappedError_Returns500(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	// No RequestLogger in the chain, so this also exercises the
	// logctx.FromContext fallback to slog.Default() through the real call
	// path — must not panic.
	response.HandleError(rec, req, errors.New("boom"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	body := decodeErrorBody(t, rec)
	if body.Code != response.CodeInternalError {
		t.Errorf("code: got %q, want %q", body.Code, response.CodeInternalError)
	}
	if body.Message != "something went wrong" {
		t.Errorf("message: got %q, want generic message, did not expect the raw error leaked", body.Message)
	}
}

func TestHandleError_ClientMappedSentinel_LogsAtWarn(t *testing.T) {
	var buf bytes.Buffer
	req := requestWithLogger(httptest.NewRequest(http.MethodGet, "/anything", nil), &buf)
	rec := httptest.NewRecorder()

	// ErrUserNotFound maps to 404 — a routine client-facing outcome, not a
	// server fault, so it should log at Warn, not Error.
	response.HandleError(rec, req, msgs.ErrUserNotFound)

	if level := decodedLogLevel(t, &buf); level != "WARN" {
		t.Errorf("log level: got %q, want %q", level, "WARN")
	}
}

func TestHandleError_ServerMappedSentinel_LogsAtError(t *testing.T) {
	var buf bytes.Buffer
	req := requestWithLogger(httptest.NewRequest(http.MethodGet, "/anything", nil), &buf)
	rec := httptest.NewRecorder()

	// ErrSubscriptionNotFound maps to 500 — an unexpected internal state
	// (every user should have an active subscription), so it should log at
	// Error just like a genuinely unmapped error would.
	response.HandleError(rec, req, msgs.ErrSubscriptionNotFound)

	if level := decodedLogLevel(t, &buf); level != "ERROR" {
		t.Errorf("log level: got %q, want %q", level, "ERROR")
	}
}

func TestDecodeJSON_ValidBody(t *testing.T) {
	var target struct {
		Email string `json:"email"`
	}
	req := httptest.NewRequest(http.MethodPost, "/anything", bytes.NewBufferString(`{"email":"ada@example.com"}`))
	rec := httptest.NewRecorder()

	ok := response.DecodeJSON(rec, req, &target)
	if !ok {
		t.Fatalf("expected DecodeJSON to succeed, response: %s", rec.Body)
	}
	if target.Email != "ada@example.com" {
		t.Errorf("Email: got %q, want %q", target.Email, "ada@example.com")
	}
}

func TestDecodeJSON_UnknownFieldRejected(t *testing.T) {
	var target struct {
		Email string `json:"email"`
	}
	req := httptest.NewRequest(http.MethodPost, "/anything", bytes.NewBufferString(`{"email":"ada@example.com","extra":"nope"}`))
	rec := httptest.NewRecorder()

	ok := response.DecodeJSON(rec, req, &target)
	if ok {
		t.Fatal("expected DecodeJSON to reject an unknown field")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body := decodeErrorBody(t, rec)
	if body.Code != response.CodeInvalidBody {
		t.Errorf("code: got %q, want %q", body.Code, response.CodeInvalidBody)
	}
}

func TestDecodeJSON_MalformedBodyRejected(t *testing.T) {
	var target struct {
		Email string `json:"email"`
	}
	req := httptest.NewRequest(http.MethodPost, "/anything", bytes.NewBufferString(`{bad json`))
	rec := httptest.NewRecorder()

	ok := response.DecodeJSON(rec, req, &target)
	if ok {
		t.Fatal("expected DecodeJSON to reject malformed JSON")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
