package oauthstate_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"linkMe/internal/utils/oauthstate"
)

const testSecret = "oauthstate-test-secret-at-least-32-bytes!!"

func TestSignVerify_Roundtrip(t *testing.T) {
	signed := oauthstate.Sign(testSecret, "raw-state-value")
	if !oauthstate.Verify(testSecret, signed, "raw-state-value") {
		t.Error("expected a freshly signed state to verify against its own raw value")
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	signed := oauthstate.Sign(testSecret, "raw-state-value")
	if oauthstate.Verify("a-completely-different-secret!!", signed, "raw-state-value") {
		t.Error("expected verification to fail with the wrong secret")
	}
}

func TestVerify_MismatchedQueryState(t *testing.T) {
	signed := oauthstate.Sign(testSecret, "raw-state-value")
	if oauthstate.Verify(testSecret, signed, "a-different-state-value") {
		t.Error("expected verification to fail when the query state doesn't match")
	}
}

func TestVerify_TamperedSignature(t *testing.T) {
	signed := oauthstate.Sign(testSecret, "raw-state-value")
	tampered := signed[:len(signed)-1] + "0"
	if oauthstate.Verify(testSecret, tampered, "raw-state-value") {
		t.Error("expected verification to fail for a tampered signature")
	}
}

func TestVerify_MalformedCookieValue(t *testing.T) {
	for _, malformed := range []string{"", "no-dot-separator", ".", "raw.", ".sig", "raw.not-hex"} {
		if oauthstate.Verify(testSecret, malformed, "raw") {
			t.Errorf("expected verification to fail for malformed cookie value %q", malformed)
		}
	}
}

func TestSetCookie_ClearCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	oauthstate.SetCookie(rec, "signed-value")

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != oauthstate.CookieName || cookies[0].Value != "signed-value" {
		t.Fatalf("expected a single %s cookie with the signed value, got %v", oauthstate.CookieName, cookies)
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Error("expected the state cookie to be HttpOnly + Secure + SameSite=Lax")
	}

	rec2 := httptest.NewRecorder()
	oauthstate.ClearCookie(rec2)
	cleared := rec2.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("expected ClearCookie to expire the cookie immediately, got %v", cleared)
	}
}
