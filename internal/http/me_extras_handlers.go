package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"ReOpenOldChatServer/internal/data"
)

// presenceIdleTTL 超过该时长未再上报即视为离线。客户端按心跳周期上报，
// 允许抖动，故取值明显大于心跳间隔。
const presenceIdleTTL = 5 * time.Minute

type presenceEntry struct {
	status    string
	updatedAt time.Time
}

// presenceStore 在线状态是高频、易失的数据，放内存不落库，避免每次心跳都写盘。
type presenceStore struct {
	mu      sync.Mutex
	entries map[string]presenceEntry
}

func newPresenceStore() *presenceStore {
	return &presenceStore{entries: make(map[string]presenceEntry)}
}

func (s *presenceStore) set(userID, status string, now time.Time) {
	if userID == "" {
		return
	}
	s.mu.Lock()
	s.entries[userID] = presenceEntry{status: status, updatedAt: now}
	s.mu.Unlock()
}

// get 返回有效状态；过期条目顺手删除并回落 offline。
func (s *presenceStore) get(userID string, now time.Time) string {
	if userID == "" {
		return "offline"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[userID]
	if !ok {
		return "offline"
	}
	if now.Sub(entry.updatedAt) > presenceIdleTTL {
		delete(s.entries, userID)
		return "offline"
	}
	return entry.status
}

// normalizePresenceStatus 只接受官方枚举，其余一律视为非法（返回空串由调用方报错）。
func normalizePresenceStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "online":
		return "online"
	case "offline":
		return "offline"
	case "busy":
		return "busy"
	case "away":
		return "away"
	}
	return ""
}

type mePresenceRequest struct {
	Status string `json:"status"`
}

// handleMePresence 记录在线状态并广播给好友，供其渲染在线状态。
// 「离线」也照常广播，否则对端会一直显示上一次的在线状态。
func (a *API) handleMePresence(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req mePresenceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	status := normalizePresenceStatus(req.Status)
	if status == "" {
		writeError(w, http.StatusBadRequest, "invalid_status", "status must be online|offline|busy|away")
		return
	}

	a.presence.set(claims.Subject, status, time.Now())

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	friendIDs, err := a.friends.ListFriendIDs(ctx, claims.Subject)
	if err == nil && len(friendIDs) > 0 {
		payload, mErr := json.Marshal(wsEnvelope{
			Type: "presence",
			Data: map[string]any{"uid": claims.UID, "ncuid": claims.NCUID, "status": status},
		})
		if mErr == nil {
			for _, id := range friendIDs {
				a.wsHub.BroadcastToUser(id, payload)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

type meScratchResponse struct {
	AlreadyScratched bool   `json:"already_scratched"`
	ScratchDate      string `json:"scratch_date"`
	Slots            []int  `json:"slots"`
	TotalReward      int    `json:"total_reward"`
	CoinBalance      int    `json:"coin_balance"`
}

func toMeScratchResponse(res *data.UserDailyScratchResult) meScratchResponse {
	slots := res.Slots
	if slots == nil {
		slots = []int{}
	}
	return meScratchResponse{
		AlreadyScratched: res.AlreadyScratched,
		ScratchDate:      res.ScratchDate,
		Slots:            slots,
		TotalReward:      res.TotalReward,
		CoinBalance:      res.CoinBalance,
	}
}

// handleMeScratchGet 查今日状态，不掷点。
func (a *API) handleMeScratchGet(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	res, err := a.users.GetDailyScratch(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toMeScratchResponse(res))
}

// handleMeScratchPost 开奖；今天已开过则返回首次结果，不重复发币。
func (a *API) handleMeScratchPost(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	res, err := a.users.DailyScratch(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toMeScratchResponse(res))
}

type meGroupInvitePreferenceRequest struct {
	Reject *bool `json:"reject"`
}

// handleMeGroupInvitePreferenceGet 查群邀请偏好。
func (a *API) handleMeGroupInvitePreferenceGet(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reject, err := a.users.GroupInviteReject(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reject": reject})
}

// handleMeGroupInvitePreferenceSet 设置群邀请偏好。reject 用指针区分「未传」与「传 false」。
func (a *API) handleMeGroupInvitePreferenceSet(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req meGroupInvitePreferenceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	if req.Reject == nil {
		writeError(w, http.StatusBadRequest, "invalid_reject", "reject is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.users.SetGroupInviteReject(ctx, claims.Subject, *req.Reject); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reject": *req.Reject})
}
