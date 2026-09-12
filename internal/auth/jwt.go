package auth

import (
	"time"

	"github.com/aidarkhanov/nanoid"
	"github.com/golang-jwt/jwt/v5"
)

type AccessClaims struct {
	UID      string `json:"uid"`
	Username string `json:"username"`
	Version  int    `json:"ver"`
	jwt.RegisteredClaims
}

func NewAccessToken(secret []byte, issuer string, ttl time.Duration, subject, uid, username string, version int) (string, error) {
	jti := nanoid.New()

	now := time.Now()
	claims := AccessClaims{
		UID:      uid,
		Username: username,
		Version:  version,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        jti,
		},
	}
	if ttl > 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}
