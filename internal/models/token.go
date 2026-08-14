package models

// TokenPair holds the two credentials issued at the end of an authentication
// flow. AccessToken is a short-lived JWT for stateless request authorization;
// RawRefreshToken is an opaque token delivered as an HttpOnly cookie and used
// to obtain fresh token pairs without re-entering credentials.
type TokenPair struct {
	AccessToken     string
	RawRefreshToken string
}
