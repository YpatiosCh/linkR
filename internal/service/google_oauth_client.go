package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	googleAuthEndpoint     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenEndpoint    = "https://oauth2.googleapis.com/token"
	googleUserInfoEndpoint = "https://openidconnect.googleapis.com/v1/userinfo"
)

// GoogleUserInfo is the verified profile fetched from Google's userinfo
// endpoint after a successful token exchange.
type GoogleUserInfo struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// GoogleOAuthClient builds Google's consent-screen URL, exchanges an
// authorization code for an access token, and fetches the verified profile
// behind it. Declared here (not imported from elsewhere) so AuthService
// stays decoupled and fakeable in tests, mirroring SessionRevoker.
type GoogleOAuthClient interface {
	// AuthURL returns the URL to redirect the user to Google's consent
	// screen, embedding state as the CSRF-protection query parameter.
	AuthURL(state string) string
	// Exchange trades an authorization code for an access token.
	Exchange(ctx context.Context, code string) (accessToken string, err error)
	// FetchUserInfo retrieves the verified Google profile behind accessToken.
	FetchUserInfo(ctx context.Context, accessToken string) (GoogleUserInfo, error)
}

// googleOAuthClient is the real implementation: two plain HTTP calls via
// net/http, no new dependency — the same shape emailService uses to wrap an
// injected Resend client.
type googleOAuthClient struct {
	httpClient   *http.Client
	clientID     string
	clientSecret string
	redirectURL  string
}

// NewGoogleOAuthClient builds a GoogleOAuthClient using httpClient for
// outbound calls and the given OAuth application credentials.
func NewGoogleOAuthClient(httpClient *http.Client, clientID, clientSecret, redirectURL string) GoogleOAuthClient {
	return &googleOAuthClient{
		httpClient:   httpClient,
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
	}
}

func (c *googleOAuthClient) AuthURL(state string) string {
	q := url.Values{
		"client_id":     {c.clientID},
		"redirect_uri":  {c.redirectURL},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"state":         {state},
	}
	return googleAuthEndpoint + "?" + q.Encode()
}

func (c *googleOAuthClient) Exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"redirect_uri":  {c.redirectURL},
		"grant_type":    {"authorization_code"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("building google token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchanging code with google: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("exchanging code with google: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decoding google token exchange response: %w", err)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("exchanging code with google: response had no access_token")
	}

	return body.AccessToken, nil
}

func (c *googleOAuthClient) FetchUserInfo(ctx context.Context, accessToken string) (GoogleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserInfoEndpoint, nil)
	if err != nil {
		return GoogleUserInfo{}, fmt.Errorf("building google userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return GoogleUserInfo{}, fmt.Errorf("fetching google userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GoogleUserInfo{}, fmt.Errorf("fetching google userinfo: unexpected status %d", resp.StatusCode)
	}

	var info GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return GoogleUserInfo{}, fmt.Errorf("decoding google userinfo response: %w", err)
	}
	if info.Subject == "" {
		return GoogleUserInfo{}, fmt.Errorf("fetching google userinfo: response had no sub claim")
	}

	return info, nil
}
