package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/data"
)

const maxVoiceDurationMS = 60000

type directSendRequest struct {
	ToUID      string `json:"to_uid"`
	Body       string `json:"body"`
	MsgType    string `json:"msg_type"`
	MediaURL   string `json:"media_url"`
	ThumbURL   string `json:"thumb_url"`
	DurationMS int    `json:"duration_ms"`
}

type directMessageResponse struct {
	ID          string `json:"id"`
	ThreadID    string `json:"thread_id"`
	FromUID     string `json:"from_uid"`
	Body        string `json:"body"`
	MsgType     string `json:"msg_type"`
	MediaURL    string `json:"media_url,omitempty"`
	ThumbURL    string `json:"thumb_url,omitempty"`
	DurationMS  int    `json:"duration_ms,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	DeliveredAt *int64 `json:"delivered_at,omitempty"`
	ReadAt      *int64 `json:"read_at,omitempty"`
}

type directMessagesResponse struct {
	Messages        []directMessageResponse `json:"messages"`
	EffectiveOffset int                     `json:"effective_offset,omitempty"`
}

type directUnreadRequest struct {
	Limit int `json:"limit"`
}

type directUnreadMessageResponse struct {
	ID          string `json:"id"`
	ThreadID    string `json:"thread_id"`
	FromUID     string `json:"from_uid"`
	PeerUID     string `json:"peer_uid"`
	Body        string `json:"body"`
	MsgType     string `json:"msg_type"`
	MediaURL    string `json:"media_url,omitempty"`
	ThumbURL    string `json:"thumb_url,omitempty"`
	DurationMS  int    `json:"duration_ms,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	DeliveredAt *int64 `json:"delivered_at,omitempty"`
	ReadAt      *int64 `json:"read_at,omitempty"`
}

type directUnreadResponse struct {
	Messages []directUnreadMessageResponse `json:"messages"`
}

type directReadRequest struct {
	WithUID string `json:"with_uid"`
}

func (a *API) handleDirectSend(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req directSendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	toUID := strings.ToUpper(strings.TrimSpace(req.ToUID))
	body := strings.TrimSpace(req.Body)
	msgType := strings.ToLower(strings.TrimSpace(req.MsgType))
	if msgType == "" {
		msgType = "text"
	}
	mediaURL := strings.TrimSpace(req.MediaURL)
	thumbURL := strings.TrimSpace(req.ThumbURL)
	if msgType == "resource" && isResourceUploadURL(mediaURL) {
		writeError(w, http.StatusForbidden, "resource_share_disabled", "resource share disabled")
		return
	}
	if !isValidUID(toUID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}
	switch msgType {
	case "text":
		if !isValidMessage(body) {
			writeError(w, http.StatusBadRequest, "invalid_body", "invalid message")
			return
		}
	case "video":
		if !a.cfg.VideoEnabled {
			writeError(w, http.StatusForbidden, "video_disabled", "video send disabled")
			return
		}
		if mediaURL == "" || len(mediaURL) > 1024 {
			writeError(w, http.StatusBadRequest, "invalid_media", "invalid media url")
			return
		}
		if body == "" {
			body = placeholderBody(msgType)
		}
	case "image", "voice", "resource":
		if mediaURL == "" || len(mediaURL) > 1024 {
			writeError(w, http.StatusBadRequest, "invalid_media", "invalid media url")
			return
		}
		if msgType == "voice" {
			if req.DurationMS <= 0 {
				writeError(w, http.StatusBadRequest, "invalid_duration", "invalid duration")
				return
			}
			if req.DurationMS > maxVoiceDurationMS {
				writeError(w, http.StatusBadRequest, "duration_too_long", "voice duration too long")
				return
			}
		}
		if body == "" {
			body = placeholderBody(msgType)
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid_type", "invalid message type")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	currentUser, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if currentUser.UID == toUID {
		writeError(w, http.StatusBadRequest, "invalid_uid", "cannot message yourself")
		return
	}

	targetUser, err := a.users.GetByUID(ctx, toUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	areFriends, err := a.friends.AreFriends(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !areFriends {
		writeError(w, http.StatusForbidden, "not_friends", "not friends")
		return
	}

	threadID, err := a.direct.GetThreadID(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		if err == data.ErrNotFound {
			newID := nanoid.New()
			threadID, err = a.direct.GetOrCreateThread(ctx, currentUser.ID, targetUser.ID, newID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "db_error", "internal error")
				return
			}
		} else {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
	}

	msgID := nanoid.New()

	msg := &data.DirectMessage{
		ID:         msgID,
		ThreadID:   threadID,
		SenderID:   currentUser.ID,
		Body:       body,
		MsgType:    msgType,
		MediaURL:   mediaURL,
		ThumbURL:   thumbURL,
		DurationMS: req.DurationMS,
	}
	if err := a.direct.CreateMessage(ctx, msg); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := directMessageResponse{
		ID:         msgID,
		ThreadID:   threadID,
		FromUID:    currentUser.UID,
		Body:       body,
		MsgType:    msgType,
		MediaURL:   mediaURL,
		ThumbURL:   thumbURL,
		DurationMS: req.DurationMS,
		CreatedAt:  time.Now().Unix(),
	}
	writeJSON(w, http.StatusCreated, resp)
	chatLogf("%s DM %s -> %s | %s", time.Now().Format("15:04:05"), currentUser.UID, targetUser.UID, formatChatPreview(msgType, body))

	env := wsEnvelope{
		Type: "direct_message",
		Data: resp,
	}
	payload, err := json.Marshal(env)
	if err == nil {
		a.wsHub.BroadcastToUser(targetUser.ID, payload)
	}
}

func (a *API) handleDirectMessages(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	toUID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("with_uid")))
	if !isValidUID(toUID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	before := parseBefore(r.URL.Query().Get("before"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	currentUser, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	targetUser, err := a.users.GetByUID(ctx, toUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	areFriends, err := a.friends.AreFriends(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !areFriends {
		writeError(w, http.StatusForbidden, "not_friends", "not friends")
		return
	}

	threadID, err := a.direct.GetThreadID(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		if err == data.ErrNotFound {
			writeJSON(w, http.StatusOK, directMessagesResponse{Messages: []directMessageResponse{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	msgs, err := a.direct.ListMessages(ctx, threadID, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	_ = a.direct.MarkDelivered(ctx, threadID, currentUser.ID)

	resp := make([]directMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		fromUID := targetUser.UID
		if msg.SenderID == currentUser.ID {
			fromUID = currentUser.UID
		}
		var deliveredAt *int64
		if msg.DeliveredAt != nil {
			ts := msg.DeliveredAt.Unix()
			deliveredAt = &ts
		}
		var readAt *int64
		if msg.ReadAt != nil {
			ts := msg.ReadAt.Unix()
			readAt = &ts
		}
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, directMessageResponse{
			ID:          msg.ID,
			ThreadID:    msg.ThreadID,
			FromUID:     fromUID,
			Body:        msg.Body,
			MsgType:     msgType,
			MediaURL:    msg.MediaURL,
			ThumbURL:    msg.ThumbURL,
			DurationMS:  msg.DurationMS,
			CreatedAt:   msg.Created.Unix(),
			DeliveredAt: deliveredAt,
			ReadAt:      readAt,
		})
	}

	writeJSON(w, http.StatusOK, directMessagesResponse{Messages: resp})
}

func (a *API) handleDirectMessagesV2(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	toUID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("with_uid")))
	if !isValidUID(toUID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))
	anchorMessageID := strings.TrimSpace(r.URL.Query().Get("anchor_message_id"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	currentUser, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	targetUser, err := a.users.GetByUID(ctx, toUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	areFriends, err := a.friends.AreFriends(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !areFriends {
		writeError(w, http.StatusForbidden, "not_friends", "not friends")
		return
	}

	threadID, err := a.direct.GetThreadID(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		if err == data.ErrNotFound {
			writeJSON(w, http.StatusOK, directMessagesResponse{Messages: []directMessageResponse{}, EffectiveOffset: offset})
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if anchorMessageID != "" {
		anchorOffset, err := a.direct.GetMessageOffset(ctx, threadID, anchorMessageID)
		if err != nil {
			if err != data.ErrNotFound {
				writeError(w, http.StatusInternalServerError, "db_error", "internal error")
				return
			}
		} else {
			offset = anchorOffset - (limit / 2)
			if offset < 0 {
				offset = 0
			}
		}
	}

	msgs, err := a.direct.ListMessagesWithOffset(ctx, threadID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	_ = a.direct.MarkDelivered(ctx, threadID, currentUser.ID)

	resp := make([]directMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		fromUID := targetUser.UID
		if msg.SenderID == currentUser.ID {
			fromUID = currentUser.UID
		}
		var deliveredAt *int64
		if msg.DeliveredAt != nil {
			ts := msg.DeliveredAt.Unix()
			deliveredAt = &ts
		}
		var readAt *int64
		if msg.ReadAt != nil {
			ts := msg.ReadAt.Unix()
			readAt = &ts
		}
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, directMessageResponse{
			ID:          msg.ID,
			ThreadID:    msg.ThreadID,
			FromUID:     fromUID,
			Body:        msg.Body,
			MsgType:     msgType,
			MediaURL:    msg.MediaURL,
			ThumbURL:    msg.ThumbURL,
			DurationMS:  msg.DurationMS,
			CreatedAt:   msg.Created.Unix(),
			DeliveredAt: deliveredAt,
			ReadAt:      readAt,
		})
	}

	writeJSON(w, http.StatusOK, directMessagesResponse{Messages: resp, EffectiveOffset: offset})
}

func (a *API) handleDirectMessagesSearch(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	toUID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("with_uid")))
	if !isValidUID(toUID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if keyword == "" {
		writeJSON(w, http.StatusOK, directMessagesResponse{Messages: []directMessageResponse{}})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind != "" && kind != "all" && kind != "text" && kind != "media" {
		kind = "all"
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	currentUser, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	targetUser, err := a.users.GetByUID(ctx, toUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	areFriends, err := a.friends.AreFriends(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !areFriends {
		writeError(w, http.StatusForbidden, "not_friends", "not friends")
		return
	}

	threadID, err := a.direct.GetThreadID(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		if err == data.ErrNotFound {
			writeJSON(w, http.StatusOK, directMessagesResponse{Messages: []directMessageResponse{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	msgs, err := a.direct.SearchMessagesWithOffset(ctx, threadID, keyword, kind, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := make([]directMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		fromUID := targetUser.UID
		if msg.SenderID == currentUser.ID {
			fromUID = currentUser.UID
		}
		var deliveredAt *int64
		if msg.DeliveredAt != nil {
			ts := msg.DeliveredAt.Unix()
			deliveredAt = &ts
		}
		var readAt *int64
		if msg.ReadAt != nil {
			ts := msg.ReadAt.Unix()
			readAt = &ts
		}
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, directMessageResponse{
			ID:          msg.ID,
			ThreadID:    msg.ThreadID,
			FromUID:     fromUID,
			Body:        msg.Body,
			MsgType:     msgType,
			MediaURL:    msg.MediaURL,
			ThumbURL:    msg.ThumbURL,
			DurationMS:  msg.DurationMS,
			CreatedAt:   msg.Created.Unix(),
			DeliveredAt: deliveredAt,
			ReadAt:      readAt,
		})
	}

	writeJSON(w, http.StatusOK, directMessagesResponse{Messages: resp})
}

func (a *API) handleDirectUnread(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req directUnreadRequest
	_ = decodeJSON(w, r, &req)
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	unread, err := a.direct.ListUnreadByUser(ctx, claims.Subject, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	_ = a.direct.MarkDeliveredByUser(ctx, claims.Subject)

	resp := make([]directUnreadMessageResponse, 0, len(unread))
	for _, msg := range unread {
		var deliveredAt *int64
		if msg.DeliveredAt != nil {
			ts := msg.DeliveredAt.Unix()
			deliveredAt = &ts
		}
		var readAt *int64
		if msg.ReadAt != nil {
			ts := msg.ReadAt.Unix()
			readAt = &ts
		}
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, directUnreadMessageResponse{
			ID:          msg.ID,
			ThreadID:    msg.ThreadID,
			FromUID:     msg.SenderUID,
			PeerUID:     msg.PeerUID,
			Body:        msg.Body,
			MsgType:     msgType,
			MediaURL:    msg.MediaURL,
			ThumbURL:    msg.ThumbURL,
			DurationMS:  msg.DurationMS,
			CreatedAt:   msg.Created.Unix(),
			DeliveredAt: deliveredAt,
			ReadAt:      readAt,
		})
	}

	writeJSON(w, http.StatusOK, directUnreadResponse{Messages: resp})
}

func (a *API) handleDirectRead(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req directReadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	toUID := strings.ToUpper(strings.TrimSpace(req.WithUID))
	if !isValidUID(toUID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	currentUser, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	targetUser, err := a.users.GetByUID(ctx, toUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	threadID, err := a.direct.GetThreadID(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		if err == data.ErrNotFound {
			writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if err := a.direct.MarkRead(ctx, threadID, currentUser.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})

	env := wsEnvelope{
		Type: "direct_read",
		Data: map[string]interface{}{
			"thread_id":  threadID,
			"reader_uid": currentUser.UID,
			"read_at":    time.Now().Unix(),
		},
	}
	if payload, err := json.Marshal(env); err == nil {
		a.wsHub.BroadcastToUser(targetUser.ID, payload)
	}
}

func (a *API) handleDirectMessageDelete(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	// 从URL路径中提取消息ID
	messageID := strings.TrimPrefix(r.URL.Path, "/v1/direct/messages/")
	if messageID == "" || messageID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "invalid_message_id", "invalid message id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	msg, err := a.direct.GetMessageByID(ctx, messageID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "message_not_found", "message not found or not yours")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if msg.SenderID != claims.Subject {
		writeError(w, http.StatusNotFound, "message_not_found", "message not found or not yours")
		return
	}
	thread, err := a.direct.GetThreadByID(ctx, msg.ThreadID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "thread_not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	sender, err := a.users.GetByID(ctx, msg.SenderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	// 删除消息（只能删除自己发送的消息）
	if err := a.direct.DeleteMessage(ctx, messageID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "message_not_found", "message not found or not yours")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})

	peerID := thread.UserAID
	if peerID == msg.SenderID {
		peerID = thread.UserBID
	}
	env := wsEnvelope{
		Type: "direct_recall",
		Data: map[string]interface{}{
			"message_id": msg.ID,
			"thread_id":  msg.ThreadID,
			"from_uid":   sender.UID,
		},
	}
	if payload, err := json.Marshal(env); err == nil {
		a.wsHub.BroadcastToUser(msg.SenderID, payload)
		a.wsHub.BroadcastToUser(peerID, payload)
	}
}

func isValidMessage(body string) bool {
	if len(body) < 1 || len(body) > 2000 {
		return false
	}
	return true
}

func placeholderBody(msgType string) string {
	switch msgType {
	case "image":
		return "[图片]"
	case "voice":
		return "[语音]"
	case "video":
		return "[视频]"
	case "resource":
		return "[资源]"
	default:
		return ""
	}
}

func parseLimit(raw string) int {
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
	if val > 100 {
		return 100
	}
	return val
}

func parseOffset(raw string) int {
	if raw == "" {
		return 0
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	if val < 0 {
		return 0
	}
	return val
}

func parseBefore(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(val, 0)
}

// wsEnvelope keeps the WebSocket payload format consistent.
type wsEnvelope struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}
