package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/auth"
	"metrochat/internal/data"
)

type externalAuthRequest struct {
	Account  string `json:"account"`
	UserID   string `json:"user_id"`
	UID      string `json:"uid"`
	Password string `json:"password"`
}

type externalGroupListResponse struct {
	Groups []externalGroupItem `json:"groups"`
}

type externalGroupItem struct {
	ID      string `json:"id"`
	GroupID string `json:"group_id"`
	Name    string `json:"name"`
	Role    int16  `json:"role"`
}

type externalFriendListResponse struct {
	Friends []externalFriendItem `json:"friends"`
}

type externalFriendItem struct {
	ID          string `json:"id"`
	UID         string `json:"uid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UserTitle   string `json:"user_title"`
	AvatarURL   string `json:"avatar_url"`
}

type externalDirectSendRequest struct {
	externalAuthRequest
	ToUID      string `json:"to_uid"`
	Body       string `json:"body"`
	MsgType    string `json:"msg_type"`
	MediaURL   string `json:"media_url"`
	ThumbURL   string `json:"thumb_url"`
	DurationMS int    `json:"duration_ms"`
}

type externalGroupSendRequest struct {
	externalAuthRequest
	GroupID    string `json:"group_id"`
	Body       string `json:"body"`
	MsgType    string `json:"msg_type"`
	MediaURL   string `json:"media_url"`
	ThumbURL   string `json:"thumb_url"`
	DurationMS int    `json:"duration_ms"`
}

func (a *externalAuthRequest) applyQuery(r *http.Request) {
	q := r.URL.Query()
	if a.Account == "" {
		a.Account = q.Get("account")
	}
	if a.Account == "" {
		a.Account = q.Get("username")
	}
	if a.Account == "" {
		a.Account = q.Get("email")
	}
	if a.UserID == "" {
		a.UserID = q.Get("user_id")
	}
	if a.UID == "" {
		a.UID = q.Get("uid")
	}
	if a.Password == "" {
		a.Password = q.Get("password")
	}
}

func (a *externalAuthRequest) normalize() {
	a.Account = strings.TrimSpace(a.Account)
	a.UserID = strings.TrimSpace(a.UserID)
	a.UID = strings.TrimSpace(a.UID)
	a.Password = strings.TrimSpace(a.Password)
}

func (a *API) handleExternalGroupList(w http.ResponseWriter, r *http.Request) {
	var req externalAuthRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	user, ok := a.authenticateExternal(w, r, &req)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	groups, err := a.groups.ListByUser(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := externalGroupListResponse{Groups: make([]externalGroupItem, 0, len(groups))}
	for _, g := range groups {
		resp.Groups = append(resp.Groups, externalGroupItem{
			ID:      g.ID,
			GroupID: g.ID,
			Name:    g.Name,
			Role:    g.Role,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleExternalFriendList(w http.ResponseWriter, r *http.Request) {
	var req externalAuthRequest
	if err := decodeOptionalJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	user, ok := a.authenticateExternal(w, r, &req)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	friends, err := a.friends.ListFriendUsers(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := externalFriendListResponse{Friends: make([]externalFriendItem, 0, len(friends))}
	for _, f := range friends {
		resp.Friends = append(resp.Friends, externalFriendItem{
			ID:          f.ID,
			UID:         f.UID,
			Username:    f.Username,
			DisplayName: f.DisplayName,
			UserTitle:   f.UserTitle,
			AvatarURL:   f.AvatarURL,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleExternalDirectSend(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var req externalDirectSendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	req.applyQuery(r)

	user, ok := a.authenticateExternal(w, r, &req.externalAuthRequest)
	if !ok {
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
	case "image", "voice":
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

	if user.UID == toUID {
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

	areFriends, err := a.friends.AreFriends(ctx, user.ID, targetUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !areFriends {
		writeError(w, http.StatusForbidden, "not_friends", "not friends")
		return
	}

	threadID, err := a.direct.GetThreadID(ctx, user.ID, targetUser.ID)
	if err != nil {
		if err == data.ErrNotFound {
			newID := nanoid.New()
			threadID, err = a.direct.GetOrCreateThread(ctx, user.ID, targetUser.ID, newID)
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
		SenderID:   user.ID,
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
		FromUID:    user.UID,
		Body:       body,
		MsgType:    msgType,
		MediaURL:   mediaURL,
		ThumbURL:   thumbURL,
		DurationMS: req.DurationMS,
		CreatedAt:  time.Now().Unix(),
	}
	writeJSON(w, http.StatusCreated, resp)
	chatLogf("%s DM %s -> %s | %s", time.Now().Format("15:04:05"), user.UID, targetUser.UID, formatChatPreview(msgType, body))

	env := wsEnvelope{
		Type: "direct_message",
		Data: resp,
	}
	payload, err := json.Marshal(env)
	if err == nil {
		a.wsHub.BroadcastToUser(targetUser.ID, payload)
	}
}

func (a *API) handleExternalGroupSend(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var req externalGroupSendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	req.applyQuery(r)

	user, ok := a.authenticateExternal(w, r, &req.externalAuthRequest)
	if !ok {
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
	case "image", "voice":
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

	role, err := a.groups.GetRole(ctx, groupID, user.ID)
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
		SenderID:   user.ID,
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
		FromUID:    user.UID,
		Body:       body,
		MsgType:    msgType,
		MediaURL:   mediaURL,
		ThumbURL:   thumbURL,
		DurationMS: req.DurationMS,
		CreatedAt:  time.Now().Unix(),
	}
	writeJSON(w, http.StatusCreated, resp)
	chatLogf("%s GRP %s | %s: %s", time.Now().Format("15:04:05"), groupID, user.UID, formatChatPreview(msgType, body))

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
		if memberID == user.ID {
			continue
		}
		a.wsHub.BroadcastToUser(memberID, payload)
	}
}

func (a *API) authenticateExternal(w http.ResponseWriter, r *http.Request, req *externalAuthRequest) (*data.User, bool) {
	if req == nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return nil, false
	}
	req.applyQuery(r)
	req.normalize()
	if req.Password == "" {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return nil, false
	}
	key := req.Account
	if key == "" {
		key = req.UID
	}
	if key == "" {
		key = req.UserID
	}
	if key == "" {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return nil, false
	}
	if !a.idLimiter.Allow("ext:" + key) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return nil, false
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	user, err := a.resolveExternalUser(ctx, req)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			_, _ = auth.VerifyPassword(req.Password, dummyHash)
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
			return nil, false
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return nil, false
	}

	ok, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return nil, false
	}

	if banned, err := a.devices.IsUserBanned(ctx, user.ID); err == nil && banned {
		writeError(w, http.StatusForbidden, "user_banned", "user banned")
		return nil, false
	}

	a.maybeRehashPassword(user, req.Password)

	return user, true
}

func (a *API) resolveExternalUser(ctx context.Context, req *externalAuthRequest) (*data.User, error) {
	if req.UserID != "" {
		return a.users.GetByID(ctx, req.UserID)
	}
	if req.UID != "" {
		return a.users.GetByUID(ctx, strings.ToUpper(req.UID))
	}
	if req.Account == "" {
		return nil, data.ErrNotFound
	}

	user, err := a.users.GetByEmailOrUsername(ctx, req.Account)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, data.ErrNotFound) {
		return nil, err
	}

	candidateUID := strings.ToUpper(req.Account)
	if isValidUID(candidateUID) {
		return a.users.GetByUID(ctx, candidateUID)
	}

	return a.users.GetByID(ctx, req.Account)
}

func decodeOptionalJSON(r *http.Request, dst interface{}) error {
	if r == nil || r.Body == nil {
		return nil
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}
