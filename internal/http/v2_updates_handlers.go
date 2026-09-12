package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ReOpenOldChatServer/internal/data"
)

type updateEventItem struct {
	PTS      int64           `json:"pts"`
	PTSCount int             `json:"pts_count"`
	Type     string          `json:"type"`
	Date     int64           `json:"date"`
	Payload  json.RawMessage `json:"payload"`
}

type updatesDifferenceResponse struct {
	Events     []updateEventItem `json:"events"`
	HasMore    bool              `json:"has_more"`
	NextPTS    int64             `json:"next_pts"`
	Reset      bool              `json:"reset"`
	CurrentPTS int64             `json:"current_pts"`
}

// handleUpdatesDifference 账号级事件增量补差。
// reset=true 表示客户端本地 pts 已无法用增量追上，必须重新拉取全量状态。
func (a *API) handleUpdatesDifference(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	q := r.URL.Query()
	requestPTS := parseInt64Default(q.Get("pts"), 0)
	// pts 补差是客户端重连主路径，上限比通用 parseLimit(100) 放宽到 200
	limit := parseLimitUpTo(q.Get("limit"), 200)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	currentPTS, err := a.updates.CurrentPTS(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if reset, _ := a.shouldResetUpdates(ctx, claims.Subject, requestPTS, currentPTS); reset {
		writeJSON(w, http.StatusOK, updatesDifferenceResponse{
			Events:     []updateEventItem{},
			HasMore:    false,
			NextPTS:    currentPTS,
			Reset:      true,
			CurrentPTS: currentPTS,
		})
		return
	}

	// 多取一条用于判断 has_more，避免为「是否还有下一页」再打一次 COUNT
	updates, err := a.updates.ListAfter(ctx, claims.Subject, requestPTS, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	hasMore := len(updates) > limit
	if hasMore {
		updates = updates[:limit]
	}

	events := make([]updateEventItem, 0, len(updates))
	nextPTS := requestPTS
	for _, u := range updates {
		payload := json.RawMessage(u.Payload)
		if !json.Valid(payload) {
			payload = json.RawMessage(`{}`)
		}
		events = append(events, updateEventItem{
			PTS:      u.PTS,
			PTSCount: u.PTSCount,
			Type:     u.EventType,
			Date:     u.Created.Unix(),
			Payload:  payload,
		})
		nextPTS = u.PTS
	}

	writeJSON(w, http.StatusOK, updatesDifferenceResponse{
		Events:     events,
		HasMore:    hasMore,
		NextPTS:    nextPTS,
		Reset:      false,
		CurrentPTS: currentPTS,
	})
}

// shouldResetUpdates 判定客户端 pts 是否已不可增量恢复。
// 三种情形：本地领先服务端（异常）、落后超过上限、请求位置的事件已被清理。
func (a *API) shouldResetUpdates(ctx context.Context, userID string, requestPTS, currentPTS int64) (bool, string) {
	if requestPTS > currentPTS {
		return true, "request_ahead"
	}
	if currentPTS-requestPTS > data.MaxPTSGap {
		return true, "gap_too_large"
	}
	// requestPTS 落在最早事件之前，说明中间的事件已被归档清理
	if minPTS, found, err := a.updates.MinPTS(ctx, userID); err == nil && found && requestPTS > 0 && requestPTS < minPTS-1 {
		return true, "archived"
	}
	return false, ""
}

type groupEventItem struct {
	GroupSeq  int64           `json:"group_seq"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type groupEventsResponse struct {
	Events []groupEventItem `json:"events"`
}

// handleGroupEventsAfter 群事件增量。事件按群消息序号推进，
// 当前实现以消息总数作为 group_seq，故 seq 之前无独立事件表可查，返回空集合。
func (a *API) handleGroupEventsAfter(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("group_id")))
	if !isValidGroupID(groupID) {
		writeError(w, http.StatusBadRequest, "invalid_group_id", "invalid group id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if _, err := a.groups.GetRole(ctx, groupID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_member", "not a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, groupEventsResponse{Events: []groupEventItem{}})
}

func parseInt64Default(raw string, fallback int64) int64 {
	if raw == "" {
		return fallback
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return val
}

func parseLimitUpTo(raw string, max int) int {
	if raw == "" {
		return 50
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 50
	}
	if val < 1 {
		return 1
	}
	if val > max {
		return max
	}
	return val
}
