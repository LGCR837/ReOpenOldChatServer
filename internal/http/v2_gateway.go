package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
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

	inner, err := http.NewRequestWithContext(r.Context(), method, target, io.NopCloser(bytes.NewReader(req.B)))
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

	rec := newBufferWriter()
	a.router.ServeHTTP(rec, inner)

	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"code": status,
		"body": json.RawMessage(rec.buf.Bytes()),
	})
}
