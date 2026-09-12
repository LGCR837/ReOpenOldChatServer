package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"ReOpenOldChatServer/internal/data"
)

// lookupPublicID 用 uid/ncuid 定位用户，ncuid 不可变故优先。
func (a *API) lookupPublicID(ctx context.Context, uid, ncuid string) (*data.User, error) {
	key, byNCUID := uid, false
	if ncuid != "" {
		key, byNCUID = ncuid, true
	}
	if byNCUID {
		return a.users.GetByNCUID(ctx, key)
	}
	return a.users.GetByUID(ctx, key)
}

// resolveUID 带缓存地取用户 uid，查不到时返回空串（不阻断消息列表）。
func (a *API) resolveUID(ctx context.Context, userID string, cache map[string]string) string {
	if uid, ok := cache[userID]; ok {
		return uid
	}
	uid := ""
	if user, err := a.users.GetByID(ctx, userID); err == nil {
		uid = user.UID
	}
	cache[userID] = uid
	return uid
}

// handleGroupMessagesAfter 群消息增量拉取。after 为空时回退到最后 N 条，始终正序返回。
func (a *API) handleGroupMessagesAfter(w http.ResponseWriter, r *http.Request) {
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
	afterID := strings.TrimSpace(r.URL.Query().Get("after"))
	limit := parseLimit(r.URL.Query().Get("limit"))

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

	msgs, err := a.groupMsgs.ListAfter(ctx, groupID, afterID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	uidCache := map[string]string{claims.Subject: claims.UID}
	resp := make([]groupMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, groupMessageResponse{
			ID:         msg.ID,
			GroupID:    msg.GroupID,
			FromUID:    a.resolveUID(ctx, msg.SenderID, uidCache),
			FromNCUID:  msg.SenderNCUID,
			Body:       msg.Body,
			MsgType:    msgType,
			MediaURL:   msg.MediaURL,
			ThumbURL:   msg.ThumbURL,
			DurationMS: msg.DurationMS,
			CreatedAt:  msg.Created.Unix(),
		})
	}

	// server_group_seq 用消息总数表示；本轮取满 limit 说明后面大概率还有
	serverSeq, err := a.groupMsgs.CountByGroup(ctx, groupID)
	if err != nil {
		serverSeq = 0
	}
	writeJSON(w, http.StatusOK, groupMessagesResponse{
		Messages:       resp,
		ServerGroupSeq: serverSeq,
		HasMore:        len(msgs) == limit,
		NextGroupSeq:   serverSeq,
	})
}

// handleDirectMessagesAfter 私聊消息增量拉取。thread_id 可直接给，也可给 with_uid/with_ncuid 由服务端换算。
func (a *API) handleDirectMessagesAfter(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	q := r.URL.Query()
	threadID := strings.TrimSpace(q.Get("thread_id"))
	afterID := strings.TrimSpace(q.Get("after"))
	limit := parseLimit(q.Get("limit"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if threadID == "" {
		lookupKey, lookupByNCUID := strings.ToUpper(strings.TrimSpace(q.Get("with_uid"))), false
		if nc := strings.ToUpper(strings.TrimSpace(q.Get("with_ncuid"))); nc != "" {
			lookupKey, lookupByNCUID = nc, true
		}
		if !isValidUID(lookupKey) {
			writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
			return
		}
		var target *data.User
		var err error
		if lookupByNCUID {
			target, err = a.users.GetByNCUID(ctx, lookupKey)
		} else {
			target, err = a.users.GetByUID(ctx, lookupKey)
		}
		if err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusNotFound, "user_not_found", "user not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		threadID, err = a.direct.GetThreadID(ctx, claims.Subject, target.ID)
		if err != nil {
			if err != data.ErrNotFound {
				writeError(w, http.StatusInternalServerError, "db_error", "internal error")
				return
			}
			writeJSON(w, http.StatusOK, directMessagesResponse{Messages: []directMessageResponse{}})
			return
		}
	}

	msgs, err := a.direct.ListAfter(ctx, threadID, afterID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	uidCache := map[string]string{claims.Subject: claims.UID}
	resp := make([]directMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, directMessageResponse{
			ID:         msg.ID,
			ThreadID:   msg.ThreadID,
			FromUID:    a.resolveUID(ctx, msg.SenderID, uidCache),
			FromNCUID:  msg.SenderNCUID,
			Body:       msg.Body,
			MsgType:    msgType,
			MediaURL:   msg.MediaURL,
			ThumbURL:   msg.ThumbURL,
			DurationMS: msg.DurationMS,
			CreatedAt:  msg.Created.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, directMessagesResponse{Messages: resp})
}
