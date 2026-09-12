package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"metrochat/internal/auth"
	"metrochat/internal/data"
)

type ctxKey string

const claimsKey ctxKey = "authClaims"
const tokenVersionCacheTTL = 30 * time.Second

type tokenVersionEntry struct {
	version   int
	expiresAt time.Time
}

func (a *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := a.authenticateFromHeader(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if !a.validateTokenVersion(r.Context(), claims.Subject, claims.Version) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		banned, err := a.devices.IsUserBanned(ctx, claims.Subject)
		cancel()
		if err == nil && banned {
			writeError(w, http.StatusForbidden, "user_banned", "user banned")
			return
		}

		ctx = context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func claimsFromContext(ctx context.Context) (*auth.AccessClaims, bool) {
	val := ctx.Value(claimsKey)
	claims, ok := val.(*auth.AccessClaims)
	return claims, ok
}

func (a *API) authenticateFromHeader(r *http.Request) (*auth.AccessClaims, bool) {
	header := r.Header.Get("Authorization")
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, false
	}
	tokenStr := strings.TrimSpace(parts[1])
	if tokenStr == "" {
		return nil, false
	}
	return a.authenticateFromToken(tokenStr)
}

func (a *API) authenticateFromToken(tokenStr string) (*auth.AccessClaims, bool) {
	claims := &auth.AccessClaims{}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	token, err := parser.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return a.cfg.JWTSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, false
	}
	if claims.Subject == "" {
		return nil, false
	}
	if claims.Issuer != "" && claims.Issuer != a.cfg.JWTIssuer {
		match := false
		for _, legacy := range a.cfg.JWTIssuerLegacy {
			if legacy != "" && claims.Issuer == legacy {
				match = true
				break
			}
		}
		if !match {
			return nil, false
		}
	}
	return claims, true
}

func (a *API) validateTokenVersion(ctx context.Context, userID string, tokenVersion int) bool {
	if userID == "" {
		return false
	}
	version, err := a.getTokenVersion(ctx, userID)
	if err != nil {
		return false
	}
	return version == tokenVersion
}

func (a *API) getTokenVersion(ctx context.Context, userID string) (int, error) {
	if userID == "" {
		return 0, data.ErrNotFound
	}
	now := time.Now()
	a.tokenVersionMu.Lock()
	entry, ok := a.tokenVersionCache[userID]
	if ok && now.Before(entry.expiresAt) {
		version := entry.version
		a.tokenVersionMu.Unlock()
		return version, nil
	}
	a.tokenVersionMu.Unlock()

	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	version, err := a.users.GetTokenVersion(cctx, userID)
	if err != nil {
		return 0, err
	}
	a.tokenVersionMu.Lock()
	a.tokenVersionCache[userID] = tokenVersionEntry{
		version:   version,
		expiresAt: now.Add(tokenVersionCacheTTL),
	}
	a.tokenVersionMu.Unlock()
	return version, nil
}

func (a *API) setTokenVersionCache(userID string, version int) {
	if userID == "" {
		return
	}
	a.tokenVersionMu.Lock()
	a.tokenVersionCache[userID] = tokenVersionEntry{
		version:   version,
		expiresAt: time.Now().Add(tokenVersionCacheTTL),
	}
	a.tokenVersionMu.Unlock()
}

func (a *API) clearTokenVersionCache(userID string) {
	if userID == "" {
		return
	}
	a.tokenVersionMu.Lock()
	delete(a.tokenVersionCache, userID)
	a.tokenVersionMu.Unlock()
}
