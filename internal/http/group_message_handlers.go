package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/data"
)

type groupMessageSendRequest struct {
	GroupID    string `json:"group_id"`
	Body       string `json:"body"`
	MsgType    string `json:"msg_type"`
	MediaURL   string `json:"media_url"`
	ThumbURL   string `json:"thumb_url"`
	DurationMS int    `json:"duration_ms"`
}

type groupMessageResponse struct {
	ID         string `json:"id"`
	GroupID    string `json:"group_id"`
	FromUID    string `json:"from_uid"`
	Body       string `json:"body"`
	MsgType    string `json:"msg_type"`
	MediaURL   string `json:"media_url,omitempty"`
	ThumbURL   string `json:"thumb_url,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

type groupMessagesResponse struct {
	Messages        []groupMessageResponse `json:"messages"`
	EffectiveOffset int                    `json:"effective_offset,omitempty"`
}

type groupUnreadRequest struct {
	Limit int `json:"limit"`
}

type groupReadRequest struct {
	GroupID string `json:"group_id"`
}

type groupUnreadMessageResponse struct {
	ID         string `json:"id"`
	GroupID    string `json:"group_id"`
	FromUID    string `json:"from_uid"`
	Body       string `json:"body"`
	MsgType    string `json:"msg_type"`
	MediaURL   string `json:"media_url,omitempty"`
	ThumbURL   string `json:"thumb_url,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

type groupUnreadResponse struct {
	Messages []groupUnreadMessageResponse `json:"messages"`
}

func (a *API) handleGroupMessageSend(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupMessageSendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
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
	if !isValidGroupID(groupID) {
		writeError(w, http.StatusBadRequest, "invalid_group_id", "invalid group id")
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
	group, err := a.groups.GetByID(ctx, groupID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "group_not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	role, err := a.groups.GetRole(ctx, groupID, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_member", "not a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if group.GlobalMute && role < data.GroupRoleAdmin {
		writeError(w, http.StatusForbidden, "group_muted", "group is muted")
		return
	}

	msgID := nanoid.New()

	msg := &data.GroupMessage{
		ID:         msgID,
		GroupID:    groupID,
		SenderID:   claims.Subject,
		Body:       body,
		MsgType:    msgType,
		MediaURL:   mediaURL,
		ThumbURL:   thumbURL,
		DurationMS: req.DurationMS,
	}
	if err := a.groupMsgs.Create(ctx, msg); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := groupMessageResponse{
		ID:         msgID,
		GroupID:    groupID,
		FromUID:    claims.UID,
		Body:       body,
		MsgType:    msgType,
		MediaURL:   mediaURL,
		ThumbURL:   thumbURL,
		DurationMS: req.DurationMS,
		CreatedAt:  time.Now().Unix(),
	}
	writeJSON(w, http.StatusCreated, resp)
	chatLogf("%s GRP %s | %s: %s", time.Now().Format("15:04:05"), groupID, claims.UID, formatChatPreview(msgType, body))

	env := wsEnvelope{
		Type: "group_message",
		Data: resp,
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return
	}
	memberIDs, err := a.groups.ListMemberIDs(ctx, groupID)
	if err != nil {
		return
	}
	for _, memberID := range memberIDs {
		if memberID == claims.Subject {
			continue
		}
		a.wsHub.BroadcastToUser(memberID, payload)
	}
}

func (a *API) handleGroupMessages(w http.ResponseWriter, r *http.Request) {
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

	limit := parseLimit(r.URL.Query().Get("limit"))
	before := parseBefore(r.URL.Query().Get("before"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := a.groups.GetByID(ctx, groupID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "group_not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if _, err := a.groups.GetRole(ctx, groupID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_member", "not a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	msgs, err := a.groupMsgs.ListByGroup(ctx, groupID, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	uidCache := map[string]string{
		claims.Subject: claims.UID,
	}
	resp := make([]groupMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		fromUID := uidCache[msg.SenderID]
		if fromUID == "" {
			user, err := a.users.GetByID(ctx, msg.SenderID)
			if err != nil {
				fromUID = ""
			} else {
				fromUID = user.UID
				uidCache[msg.SenderID] = fromUID
			}
		}
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, groupMessageResponse{
			ID:         msg.ID,
			GroupID:    msg.GroupID,
			FromUID:    fromUID,
			Body:       msg.Body,
			MsgType:    msgType,
			MediaURL:   msg.MediaURL,
			ThumbURL:   msg.ThumbURL,
			DurationMS: msg.DurationMS,
			CreatedAt:  msg.Created.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, groupMessagesResponse{Messages: resp})
	if before.IsZero() {
		_ = a.groups.MarkRead(ctx, groupID, claims.Subject)
	}
}

func (a *API) handleGroupMessagesV2(w http.ResponseWriter, r *http.Request) {
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

	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))
	anchorMessageID := strings.TrimSpace(r.URL.Query().Get("anchor_message_id"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := a.groups.GetByID(ctx, groupID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "group_not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if _, err := a.groups.GetRole(ctx, groupID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_member", "not a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if anchorMessageID != "" {
		anchorOffset, err := a.groupMsgs.GetMessageOffset(ctx, groupID, anchorMessageID)
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

	msgs, err := a.groupMsgs.ListByGroupWithOffset(ctx, groupID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	uidCache := map[string]string{
		claims.Subject: claims.UID,
	}
	resp := make([]groupMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		fromUID := uidCache[msg.SenderID]
		if fromUID == "" {
			user, err := a.users.GetByID(ctx, msg.SenderID)
			if err != nil {
				fromUID = ""
			} else {
				fromUID = user.UID
				uidCache[msg.SenderID] = fromUID
			}
		}
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, groupMessageResponse{
			ID:         msg.ID,
			GroupID:    msg.GroupID,
			FromUID:    fromUID,
			Body:       msg.Body,
			MsgType:    msgType,
			MediaURL:   msg.MediaURL,
			ThumbURL:   msg.ThumbURL,
			DurationMS: msg.DurationMS,
			CreatedAt:  msg.Created.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, groupMessagesResponse{Messages: resp, EffectiveOffset: offset})
	if offset == 0 {
		_ = a.groups.MarkRead(ctx, groupID, claims.Subject)
	}
}

func (a *API) handleGroupMessagesSearch(w http.ResponseWriter, r *http.Request) {
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
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if keyword == "" {
		writeJSON(w, http.StatusOK, groupMessagesResponse{Messages: []groupMessageResponse{}})
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
	if _, err := a.groups.GetByID(ctx, groupID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "group_not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if _, err := a.groups.GetRole(ctx, groupID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_member", "not a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	msgs, err := a.groupMsgs.SearchByGroupWithOffset(ctx, groupID, keyword, kind, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	uidCache := map[string]string{
		claims.Subject: claims.UID,
	}
	resp := make([]groupMessageResponse, 0, len(msgs))
	for _, msg := range msgs {
		fromUID := uidCache[msg.SenderID]
		if fromUID == "" {
			user, err := a.users.GetByID(ctx, msg.SenderID)
			if err != nil {
				fromUID = ""
			} else {
				fromUID = user.UID
				uidCache[msg.SenderID] = fromUID
			}
		}
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, groupMessageResponse{
			ID:         msg.ID,
			GroupID:    msg.GroupID,
			FromUID:    fromUID,
			Body:       msg.Body,
			MsgType:    msgType,
			MediaURL:   msg.MediaURL,
			ThumbURL:   msg.ThumbURL,
			DurationMS: msg.DurationMS,
			CreatedAt:  msg.Created.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, groupMessagesResponse{Messages: resp})
}

func (a *API) handleGroupUnread(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupUnreadRequest
	_ = decodeJSON(w, r, &req)
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	unread, err := a.groupMsgs.ListUnreadByUser(ctx, claims.Subject, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := make([]groupUnreadMessageResponse, 0, len(unread))
	for _, msg := range unread {
		msgType := msg.MsgType
		if msgType == "" {
			msgType = "text"
		}
		resp = append(resp, groupUnreadMessageResponse{
			ID:         msg.ID,
			GroupID:    msg.GroupID,
			FromUID:    msg.SenderUID,
			Body:       msg.Body,
			MsgType:    msgType,
			MediaURL:   msg.MediaURL,
			ThumbURL:   msg.ThumbURL,
			DurationMS: msg.DurationMS,
			CreatedAt:  msg.Created.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, groupUnreadResponse{Messages: resp})
}

func (a *API) handleGroupRead(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupReadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
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

	if err := a.groups.MarkRead(ctx, groupID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}
