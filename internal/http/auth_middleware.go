package httpapi

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"ReOpenOldChatServer/internal/auth"
	"ReOpenOldChatServer/internal/data"
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
		if a.isSessionRevoked(r.Context(), claims.ID) {
			writeError(w, http.StatusUnauthorized, "session_revoked", "session revoked")
			return
		}
		a.touchSession(r.Context(), claims.ID)
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

// invalidateSessionCache 立即清除某会话的正向缓存，让吊销即时生效（logout/cleanup 调用）。
func invalidateSessionCache(jti string) {
	if jti == "" {
		return
	}
	sessionCheckMu.Lock()
	delete(sessionCheckCache, jti)
	sessionTouchMu.Lock()
	delete(sessionTouchCache, jti)
	sessionTouchMu.Unlock()
	sessionCheckMu.Unlock()
}

// sessionCheckCache 会话吊销状态的正向缓存：短 TTL 内不重复查库。
// 只缓存「有效」结论，吊销是低频管理动作，缓存过期（30s）后必然生效。
type sessionCheckEntry struct {
	ok        bool
	expiresAt time.Time
}

var sessionCheckMu sync.Mutex
var sessionCheckCache = map[string]sessionCheckEntry{}

const sessionCheckTTL = 30 * time.Second

func (a *API) isSessionRevoked(ctx context.Context, jti string) bool {
	if jti == "" {
		return false
	}
	now := time.Now()
	sessionCheckMu.Lock()
	if e, hit := sessionCheckCache[jti]; hit && now.Before(e.expiresAt) {
		sessionCheckMu.Unlock()
		return !e.ok
	}
	sessionCheckMu.Unlock()

	revoked, err := a.loginSessions.IsRevoked(ctx, jti)
	if err != nil {
		// 查库失败放行：token version 与签名校验仍然生效，不因辅助表故障拒绝登录
		return false
	}
	sessionCheckMu.Lock()
	// 顺带清理过期项，防止长期运行下 map 无界增长
	if len(sessionCheckCache) > 4096 {
		for k, e := range sessionCheckCache {
			if now.After(e.expiresAt) {
				delete(sessionCheckCache, k)
			}
		}
	}
	sessionCheckCache[jti] = sessionCheckEntry{ok: !revoked, expiresAt: now.Add(sessionCheckTTL)}
	sessionCheckMu.Unlock()
	return revoked
}

// sessionTouchCache last_seen 更新节流：每会话 5 分钟最多写一次。
var sessionTouchMu sync.Mutex
var sessionTouchCache = map[string]time.Time{}

const sessionTouchInterval = 5 * time.Minute

func (a *API) touchSession(ctx context.Context, jti string) {
	if jti == "" {
		return
	}
	now := time.Now()
	sessionTouchMu.Lock()
	if last, hit := sessionTouchCache[jti]; hit && now.Sub(last) < sessionTouchInterval {
		sessionTouchMu.Unlock()
		return
	}
	sessionTouchCache[jti] = now
	if len(sessionTouchCache) > 4096 {
		for k, t := range sessionTouchCache {
			if now.Sub(t) > time.Hour {
				delete(sessionTouchCache, k)
			}
		}
	}
	sessionTouchMu.Unlock()

	go func() {
		tctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = a.loginSessions.TouchLastSeen(tctx, jti)
	}()
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
