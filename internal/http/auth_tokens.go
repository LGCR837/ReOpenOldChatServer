package httpapi

import (
	"context"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/auth"
	"metrochat/internal/data"
)

type tokenPair struct {
	AccessToken  string
	RefreshToken string
}

func (a *API) issueTokens(ctx context.Context, user *data.User) (tokenPair, error) {
	version := 0
	if user != nil {
		version = user.TokenVersion
	}
	accessToken, err := auth.NewAccessToken(a.cfg.JWTSecret, a.cfg.JWTIssuer, a.cfg.AccessTokenTTL, user.ID, user.UID, user.Username, version)
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
