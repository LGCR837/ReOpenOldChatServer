package auth

import (
	"time"

	"github.com/aidarkhanov/nanoid"
	"github.com/golang-jwt/jwt/v5"
)

type AccessClaims struct {
	UID      string `json:"uid"`
	NCUID    string `json:"ncuid"`
	Username string `json:"username"`
	Version  int    `json:"ver"`
	// 会话关联的设备 ID（user_sessions.jti 同源），中间件据此做会话吊销检查
	DeviceID string `json:"did,omitempty"`
	jwt.RegisteredClaims
}

func NewAccessToken(secret []byte, issuer string, ttl time.Duration, subject, uid, ncuid, username string, version int, deviceID string) (string, error) {
	jti := nanoid.New()

	now := time.Now()
	claims := AccessClaims{
		UID:      uid,
		NCUID:    ncuid,
		Username: username,
		Version:  version,
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   issuer,
			Subject:  subject,
			IssuedAt: jwt.NewNumericDate(now),
			ID:       jti,
		},
	}
	if ttl > 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// NewAccessTokenWithJTI 同 NewAccessToken 并返回 jti，供签发方登记 user_sessions。
func NewAccessTokenWithJTI(secret []byte, issuer string, ttl time.Duration, subject, uid, ncuid, username string, version int, deviceID string) (string, string, error) {
	jti := nanoid.New()

	now := time.Now()
	claims := AccessClaims{
		UID:      uid,
		NCUID:    ncuid,
		Username: username,
		Version:  version,
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   issuer,
			Subject:  subject,
			IssuedAt: jwt.NewNumericDate(now),
			ID:       jti,
		},
	}
	if ttl > 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	return signed, jti, err
}
