package httpapi

import (
	"context"
	"time"

	"github.com/aidarkhanov/nanoid"

	"ReOpenOldChatServer/internal/auth"
	"ReOpenOldChatServer/internal/data"
)

type tokenPair struct {
	AccessToken  string
	RefreshToken string
}

// deviceInfo 签发 token 时随行的设备信息，落 user_sessions 供会话列表/精确吊销。
type deviceInfo struct {
	ID         string
	Name       string
	Platform   string
	AppVersion string
}

// issueTokens 签发 access+refresh 并登记一条登录会话（jti = access token 的 JWT ID）。
func (a *API) issueTokens(ctx context.Context, user *data.User, dev deviceInfo) (tokenPair, error) {
	version := 0
	if user != nil {
		version = user.TokenVersion
	}
	accessToken, jti, err := auth.NewAccessTokenWithJTI(a.cfg.JWTSecret, a.cfg.JWTIssuer, a.cfg.AccessTokenTTL, user.ID, user.UID, user.NCUID, user.Username, version, dev.ID)
	if err != nil {
		return tokenPair{}, err
	}

	refreshModel, rawRefresh, err := a.newRefreshToken(user.ID)
	if err != nil {
		return tokenPair{}, err
	}

	if err := a.refresh.Create(ctx, refreshModel); err != nil {
		return tokenPair{}, err
	}

	_ = a.loginSessions.Create(ctx, &data.UserSession{
		JTI:        jti,
		UserID:     user.ID,
		DeviceID:   dev.ID,
		DeviceName: dev.Name,
		Platform:   dev.Platform,
		AppVersion: dev.AppVersion,
	})

	return tokenPair{AccessToken: accessToken, RefreshToken: rawRefresh}, nil
}

func (a *API) newRefreshToken(userID string) (*data.RefreshToken, string, error) {
	raw, hash, err := auth.NewRefreshToken()
	if err != nil {
		return nil, "", err
	}

	id := nanoid.New()
	token := &data.RefreshToken{
		ID:        id,
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(a.cfg.RefreshTokenTTL),
	}
	return token, raw, nil
}
