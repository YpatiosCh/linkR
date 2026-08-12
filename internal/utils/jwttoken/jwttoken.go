package jwttoken

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// AccessTokenDuration is the lifetime of an issued access token. The short
// window limits the blast radius of a stolen token without requiring
// continuous database lookups on the request hot path.
const AccessTokenDuration = 15 * time.Minute

// Claims is the set of fields embedded in every access token. It extends
// jwt.RegisteredClaims so the standard expiry, issued-at, and subject
// fields are available alongside the application-specific ones.
type Claims struct {
	UserID    uuid.UUID `json:"user_id"`
	SessionID uuid.UUID `json:"session_id"`
	PlanKey   string    `json:"plan_key"`
	jwt.RegisteredClaims
}

// Issue signs a new HS256 JWT carrying the given claims, setting IssuedAt to
// now and ExpiresAt to now + AccessTokenDuration. It returns the compact
// token string, or an error if signing fails.
func Issue(secret string, userID, sessionID uuid.UUID, planKey string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:    userID,
		SessionID: sessionID,
		PlanKey:   planKey,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenDuration)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("signing access token: %w", err)
	}
	return signed, nil
}

// Verify parses and validates the compact token string using the given
// HS256 secret. It enforces expiry automatically. On success it returns
// the embedded Claims; on failure (wrong key, expired, malformed) it
// returns a non-nil error.
func Verify(secret, tokenString string) (Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Claims{}, fmt.Errorf("access token expired: %w", err)
		}
		return Claims{}, fmt.Errorf("invalid access token: %w", err)
	}
	return claims, nil
}
