package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"strings"
)

// 网关内部重放标记。外部传入会先被删除，可信任。
const hdrGatewayVerified = "X-V2-Gateway-Verified"

type v2GatewayRequest struct {
	M string          `json:"m"`
	P string          `json:"p"`
	Q string          `json:"q"`
	B json.RawMessage `json:"b"`
}

// handleV2Gateway 把 {m,p,q,b} 折叠请求重放到内部路由，响应统一 HTTP 200 + {code,body}。
// 客户端签名针对 /v2/gateway 本身，故内部重放带 verified 标记跳过二次签名校验。
func (a *API) handleV2Gateway(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var req v2GatewayRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	if !strings.HasPrefix(req.P, "/v2/") {
		writeError(w, http.StatusBadRequest, "bad_path", "path must start with /v2/")
		return
	}

	method := strings.ToUpper(req.M)
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		writeError(w, http.StatusBadRequest, "bad_method", "unsupported method")
		return
	}

	target := req.P
	if req.Q != "" {
		target += "?" + req.Q
	}

	// 必须重置 chi RouteContext：外层已写入 /v2/gateway 的路由状态，复用会让内部重放误匹配
	inner, err := http.NewRequestWithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chi.NewRouteContext()), method, target, io.NopCloser(bytes.NewReader(req.B)))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_path", "invalid path")
		return
	}
	inner.Header = r.Header.Clone()
	inner.Host = r.Host
	inner.RemoteAddr = r.RemoteAddr
	inner.Header.Del(hdrGatewayVerified)
	inner.Header.Set(hdrGatewayVerified, "1")
	inner.Header.Set("Content-Type", "application/json")
	inner.Header.Del("Accept-Encoding") // 重放结果还要二次序列化，不能先 gzip

	rec := newBufferWriter()
	a.router.ServeHTTP(rec, inner)

	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	// 空 body 不能塞进 json.RawMessage，否则 marshal 失败导致整个响应丢失
	body := bytes.TrimSpace(rec.buf.Bytes())
	switch {
	case len(body) == 0:
		body = []byte("null")
	case !json.Valid(body):
		// 非 JSON（如 chi 的 404 纯文本）直接塞 RawMessage 会让整个响应 marshal 失败
		body, _ = json.Marshal(string(body))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"code": status,
		"body": json.RawMessage(body),
	})
}
