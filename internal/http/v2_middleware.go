package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"ReOpenOldChatServer/internal/secure"
)

// sign = base64url_nopad(HMAC-SHA256(macKey, "METHOD\nPATH\nTS\nNONCE"))，PATH 不含 query。
// 客户端只对 invalid/missing_session 自愈重握手，bad_signature 不会重试。
const (
	hdrV2Session = "X-Session"
	hdrV2Ts      = "X-Ts"
	hdrV2Nonce   = "X-Nonce"
	hdrV2Sign    = "X-Sign"
	hdrV2Device  = "X-Device-Id"

	v2ClockSkew     = 300 * time.Second // 允许的时钟偏移
	v2NonceCacheTTL = 600 * time.Second // nonce 去重保留时长
)

// 官方 §4.5：/v2/{files,resources}/{upload,download} 不加密、不签名，仅 Bearer JWT 鉴权
// （流式 multipart / Range 下载，无法逐帧签名）。客户端 SDK 的 V2_UNSIGNED_PATHS
// 刻意不带 X-Session/X-Sign，若在此照常校验签名链必然 401。
// 豁免的只是签名，鉴权仍由 authMiddleware 的 Bearer JWT 负责。
var v2UnsignedPaths = regexp.MustCompile(`^/v2/(files|resources)/(upload|download)(/|$)`)

type nonceCache struct {
	mu      sync.Mutex
	seen    map[string]time.Time
	lastGC  time.Time
	maxSize int
}

func newNonceCache() *nonceCache {
	return &nonceCache{seen: make(map[string]time.Time), lastGC: time.Now(), maxSize: 200000}
}

// check 返回 false 表示 nonce 已被使用过（重放）。
func (c *nonceCache) check(nonce string) bool {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if now.Sub(c.lastGC) > time.Minute {
		for k, t := range c.seen {
			if now.Sub(t) > v2NonceCacheTTL {
				delete(c.seen, k)
			}
		}
		c.lastGC = now
	}
	if len(c.seen) >= c.maxSize {
		for k := range c.seen {
			delete(c.seen, k)
			if len(c.seen) < c.maxSize/2 {
				break
			}
		}
	}
	if _, dup := c.seen[nonce]; dup {
		return false
	}
	c.seen[nonce] = now
	return true
}

// v2SignMiddleware 校验 /v2/* 请求的 HMAC 签名链。
// 未启用（V2_SIGN_REQUIRED=0）时直接放行，便于联调。
func (a *API) v2SignMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.cfg.V2SignRequired {
			next.ServeHTTP(w, r)
			return
		}

		// 免签名豁免路径：放行，后续 authMiddleware 仍会校验 Bearer JWT
		if v2UnsignedPaths.MatchString(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// 网关内部重放：签名已在 /v2/gateway 入口校验过
		if r.Header.Get(hdrGatewayVerified) == "1" {
			sessionID := strings.TrimSpace(r.Header.Get(hdrV2Session))
			if keys, ok := a.sessions.Get(sessionID); ok {
				ctx := withSessionKeys(r.Context(), secure.SessionKeys{EncKey: keys.EncKey, MacKey: keys.MacKey})
				ctx = withDeviceID(ctx, strings.TrimSpace(r.Header.Get(hdrV2Device)))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			writeError(w, http.StatusUnauthorized, "invalid_session", "invalid session")
			return
		}

		sessionID := strings.TrimSpace(r.Header.Get(hdrV2Session))
		if sessionID == "" {
			writeError(w, http.StatusUnauthorized, "missing_session", "missing session")
			return
		}
		keys, ok := a.sessions.Get(sessionID)
		if !ok {
			writeError(w, http.StatusUnauthorized, "invalid_session", "invalid session")
			return
		}

		tsRaw := strings.TrimSpace(r.Header.Get(hdrV2Ts))
		nonce := strings.TrimSpace(r.Header.Get(hdrV2Nonce))
		sign := strings.TrimSpace(r.Header.Get(hdrV2Sign))
		if tsRaw == "" || nonce == "" || sign == "" {
			writeError(w, http.StatusUnauthorized, "missing_signature", "missing signature headers")
			return
		}

		ts, err := strconv.ParseInt(tsRaw, 10, 64)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "bad_signature", "invalid timestamp")
			return
		}
		if diff := time.Since(time.Unix(ts, 0)); diff > v2ClockSkew || diff < -v2ClockSkew {
			writeError(w, http.StatusUnauthorized, "bad_signature", "timestamp out of range")
			return
		}
		if !a.v2Nonces.check(sessionID + "|" + nonce) {
			writeError(w, http.StatusUnauthorized, "bad_signature", "nonce reused")
			return
		}

		if !verifyV2Signature(keys.MacKey, r.Method, r.URL.Path, tsRaw, nonce, sign) {
			writeError(w, http.StatusUnauthorized, "bad_signature", "bad signature")
			return
		}

		// 校验通过：把会话密钥与设备号塞进上下文，供后续加密/业务使用
		ctx := withSessionKeys(r.Context(), secure.SessionKeys{EncKey: keys.EncKey, MacKey: keys.MacKey})
		ctx = withDeviceID(ctx, strings.TrimSpace(r.Header.Get(hdrV2Device)))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---- 上下文传递：会话密钥 / 设备号 ----

const (
	ctxKeySessionKeys ctxKey = "v2SessionKeys"
	ctxKeyDeviceID    ctxKey = "v2DeviceID"
)

func withSessionKeys(ctx context.Context, keys secure.SessionKeys) context.Context {
	return context.WithValue(ctx, ctxKeySessionKeys, keys)
}

func sessionKeysFrom(ctx context.Context) (secure.SessionKeys, bool) {
	keys, ok := ctx.Value(ctxKeySessionKeys).(secure.SessionKeys)
	return keys, ok
}

func withDeviceID(ctx context.Context, deviceID string) context.Context {
	return context.WithValue(ctx, ctxKeyDeviceID, deviceID)
}

func deviceIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyDeviceID).(string)
	return id
}

func verifyV2Signature(macKey []byte, method, path, ts, nonce, sign string) bool {
	signing := strings.ToUpper(method) + "\n" + path + "\n" + ts + "\n" + nonce
	mac := hmac.New(sha256.New, macKey)
	_, _ = mac.Write([]byte(signing))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	// 同时接受 base64url 无填充（客户端标准）与标准 base64 带填充
	candidate := strings.TrimRight(sign, "=")
	if hmac.Equal([]byte(candidate), []byte(expected)) {
		return true
	}
	expectedStd := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sign), []byte(expectedStd))
}
