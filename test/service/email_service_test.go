package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"linkMe/config"
	"linkMe/internal/service"

	"github.com/resend/resend-go/v3"
)

// fakeRoundTripper is an http.RoundTripper that captures the outgoing
// request and returns a canned response, so emailService can be tested
// without any network access.
type fakeRoundTripper struct {
	statusCode int
	body       string

	capturedBody []byte
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		f.capturedBody = b
	}
	return &http.Response{
		StatusCode: f.statusCode,
		Body:       io.NopCloser(bytes.NewReader([]byte(f.body))),
		Header:     make(http.Header),
	}, nil
}

func newTestResendClient(rt *fakeRoundTripper) *resend.Client {
	return resend.NewCustomClient(&http.Client{Transport: rt}, "test-key")
}

var emailTestCfg = config.Config{
	EmailFrom:   "linkMe <noreply@linkme.app>",
	FrontendURL: "https://app.linkme.com",
}

func TestEmailService_SendVerificationEmail_Success(t *testing.T) {
	rt := &fakeRoundTripper{statusCode: http.StatusOK, body: `{"id":"abc123"}`}
	svc := service.NewEmailService(newTestResendClient(rt), emailTestCfg)

	if err := svc.SendVerificationEmail(context.Background(), "ada@example.com", "tok-123"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var payload resend.SendEmailRequest
	if err := json.Unmarshal(rt.capturedBody, &payload); err != nil {
		t.Fatalf("decoding captured request body: %v", err)
	}
	if payload.From != emailTestCfg.EmailFrom {
		t.Errorf("From: got %q, want %q", payload.From, emailTestCfg.EmailFrom)
	}
	if len(payload.To) != 1 || payload.To[0] != "ada@example.com" {
		t.Errorf("To: got %v", payload.To)
	}
	if payload.Subject != "Verify your email" {
		t.Errorf("Subject: got %q", payload.Subject)
	}
	if !strings.Contains(payload.Html, "tok-123") {
		t.Errorf("Html body should contain the token-bearing link, got %q", payload.Html)
	}
	if !strings.Contains(payload.Html, emailTestCfg.FrontendURL+"/verify-email") {
		t.Errorf("Html body should link to the frontend verify-email route, got %q", payload.Html)
	}
}

func TestEmailService_SendPasswordResetEmail_Success(t *testing.T) {
	rt := &fakeRoundTripper{statusCode: http.StatusOK, body: `{"id":"abc123"}`}
	svc := service.NewEmailService(newTestResendClient(rt), emailTestCfg)

	if err := svc.SendPasswordResetEmail(context.Background(), "ada@example.com", "tok-456"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var payload resend.SendEmailRequest
	if err := json.Unmarshal(rt.capturedBody, &payload); err != nil {
		t.Fatalf("decoding captured request body: %v", err)
	}
	if payload.Subject != "Reset your password" {
		t.Errorf("Subject: got %q", payload.Subject)
	}
	if !strings.Contains(payload.Html, "tok-456") {
		t.Errorf("Html body should contain the token-bearing link, got %q", payload.Html)
	}
	if !strings.Contains(payload.Html, emailTestCfg.FrontendURL+"/reset-password") {
		t.Errorf("Html body should link to the frontend reset-password route, got %q", payload.Html)
	}
}

func TestEmailService_SendVerificationEmail_NonSuccessStatus(t *testing.T) {
	rt := &fakeRoundTripper{
		statusCode: http.StatusUnprocessableEntity,
		body:       `{"statusCode":422,"name":"validation_error","message":"invalid from address"}`,
	}
	svc := service.NewEmailService(newTestResendClient(rt), emailTestCfg)

	err := svc.SendVerificationEmail(context.Background(), "ada@example.com", "tok-123")
	if err == nil {
		t.Fatal("expected an error for a non-2xx response, got nil")
	}
}

func TestEmailService_SendPasswordResetEmail_NonSuccessStatus(t *testing.T) {
	rt := &fakeRoundTripper{
		statusCode: http.StatusTooManyRequests,
		body:       `{"message":"rate limited"}`,
	}
	svc := service.NewEmailService(newTestResendClient(rt), emailTestCfg)

	err := svc.SendPasswordResetEmail(context.Background(), "ada@example.com", "tok-456")
	if err == nil {
		t.Fatal("expected an error for a non-2xx response, got nil")
	}
}
