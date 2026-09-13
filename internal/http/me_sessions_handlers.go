package httpapi

import (
	"context"
	"net/http"
	"time"
)

// 会话列表（本地增强，官方无此端点）：回答「当前哪些设备还登录着」。
// 历史设备累积视图另有 GET /me/devices（user_login_devices，官方格式）。

type sessionItem struct {
	ID         string `json:"id"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name,omitempty"`
	Platform   string `json:"platform,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	LastSeen   int64  `json:"last_seen"`
	Current    bool   `json:"current"`
}

// handleMeSessions GET /v1|/v2/me/sessions — 当前用户全部活跃会话，活跃时间倒序，当前会话带 current 标记。
func (a *API) handleMeSessions(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	sessions, err := a.loginSessions.ListActive(ctx, claims.Subject, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	items := make([]sessionItem, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, sessionItem{
			ID:         s.JTI,
			DeviceID:   s.DeviceID,
			DeviceName: s.DeviceName,
			Platform:   s.Platform,
			AppVersion: s.AppVersion,
			CreatedAt:  s.CreatedAt.Unix(),
			LastSeen:   s.LastSeen.Unix(),
			Current:    s.JTI == claims.ID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": items})
}
