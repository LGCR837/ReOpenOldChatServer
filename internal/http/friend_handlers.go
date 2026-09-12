package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/data"
)

type friendRequestRequest struct {
	ToUID string `json:"to_uid"`
}

type friendRequestResponse struct {
	RequestID string `json:"request_id"`
}

type friendRespondRequest struct {
	RequestID string `json:"request_id"`
	Accept    bool   `json:"accept"`
}

type friendListItem struct {
	ID            string `json:"id"`
	UID           string `json:"uid"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	RemarkName    string `json:"remark_name"`
	UserTitle     string `json:"user_title"`
	AvatarURL     string `json:"avatar_url"`
	FriendAddedAt int64  `json:"friend_added_at"`
}

type friendListResponse struct {
	Friends []friendListItem `json:"friends"`
}

type friendRequestItem struct {
	ID              string `json:"id"`
	Status          int16  `json:"status"`
	FromUID         string `json:"from_uid"`
	FromUsername    string `json:"from_username"`
	FromDisplayName string `json:"from_display_name"`
	FromTitle       string `json:"from_title"`
	AvatarURL       string `json:"avatar_url"`
}

type friendRequestsResponse struct {
	Requests []friendRequestItem `json:"requests"`
}

func (a *API) handleFriendRequest(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req friendRequestRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	toUID := strings.ToUpper(strings.TrimSpace(req.ToUID))
	if !isValidUID(toUID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_ = a.friendReqs.DeletePendingBefore(ctx, time.Now().Add(-24*time.Hour))
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
		writeError(w, http.StatusBadRequest, "invalid_uid", "cannot friend yourself")
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
	if areFriends {
		writeError(w, http.StatusConflict, "already_friends", "already friends")
		return
	}

	pending, err := a.friendReqs.PendingBetween(ctx, currentUser.ID, targetUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if pending {
		writeError(w, http.StatusConflict, "request_pending", "对方正在处理你的好友请求")
		return
	}

	id := nanoid.New()

	fr := &data.FriendRequest{
		ID:         id,
		FromUserID: currentUser.ID,
		ToUserID:   targetUser.ID,
		Status:     data.FriendRequestPending,
	}

	if err := a.friendReqs.Create(ctx, fr); err != nil {
		if isUniqueViolation(err, "idx_friend_req_pending") {
			writeError(w, http.StatusConflict, "request_pending", "对方正在处理你的好友请求")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, friendRequestResponse{RequestID: id})
}

func (a *API) handleFriendList(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	friends, err := a.friends.ListFriendUsers(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := friendListResponse{Friends: make([]friendListItem, 0, len(friends))}
	for _, f := range friends {
		resp.Friends = append(resp.Friends, friendListItem{
			ID:            f.ID,
			UID:           f.UID,
			Username:      f.Username,
			DisplayName:   f.DisplayName,
			RemarkName:    f.RemarkName,
			UserTitle:     f.UserTitle,
			AvatarURL:     f.AvatarURL,
			FriendAddedAt: f.FriendAddedAt.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleFriendRequests(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_ = a.friendReqs.DeletePendingBefore(ctx, time.Now().Add(-24*time.Hour))

	requests, err := a.friendReqs.ListIncoming(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := friendRequestsResponse{Requests: make([]friendRequestItem, 0, len(requests))}
	for _, req := range requests {
		resp.Requests = append(resp.Requests, friendRequestItem{
			ID:              req.ID,
			Status:          req.Status,
			FromUID:         req.FromUID,
			FromUsername:    req.FromUsername,
			FromDisplayName: req.FromDisplayName,
			FromTitle:       req.FromTitle,
			AvatarURL:       req.FromAvatarURL,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleFriendRespond(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req friendRespondRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if req.Accept {
		fromUserID, err := a.friendReqs.AcceptAndAddFriend(ctx, requestID, claims.Subject)
		if err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusNotFound, "request_not_found", "request not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		// 通知申请者好友请求已被接受
		go a.sendFriendAcceptedNotification(fromUserID, claims.Subject)
	} else {
		fromUserID, err := a.friendReqs.Deny(ctx, requestID, claims.Subject)
		if err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusNotFound, "request_not_found", "request not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		// 通知申请者好友请求已被拒绝
		go a.sendFriendRejectedNotification(fromUserID, claims.Subject)
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

type friendDeleteRequest struct {
	FriendUID string `json:"friend_uid"`
}

type friendRemarkRequest struct {
	FriendUID  string `json:"friend_uid"`
	RemarkName string `json:"remark_name"`
}

func (a *API) handleFriendRemark(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req friendRemarkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	friendUID := strings.ToUpper(strings.TrimSpace(req.FriendUID))
	if !isValidUID(friendUID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}
	remark := strings.TrimSpace(req.RemarkName)
	if len([]rune(remark)) > 32 {
		writeError(w, http.StatusBadRequest, "remark_too_long", "remark too long")
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

	if currentUser.UID == friendUID {
		writeError(w, http.StatusBadRequest, "invalid_uid", "cannot set remark for yourself")
		return
	}

	friendUser, err := a.users.GetByUID(ctx, friendUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if err := a.friends.SetRemark(ctx, currentUser.ID, friendUser.ID, remark); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_friends", "not friends")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleFriendDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req friendDeleteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	friendUID := strings.ToUpper(strings.TrimSpace(req.FriendUID))
	if !isValidUID(friendUID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
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

	if currentUser.UID == friendUID {
		writeError(w, http.StatusBadRequest, "invalid_uid", "cannot delete yourself")
		return
	}

	friendUser, err := a.users.GetByUID(ctx, friendUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	areFriends, err := a.friends.AreFriends(ctx, currentUser.ID, friendUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !areFriends {
		writeError(w, http.StatusNotFound, "not_friends", "not friends")
		return
	}

	if err := a.friends.RemoveFriendship(ctx, currentUser.ID, friendUser.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) sendFriendAcceptedNotification(toUserID, fromUserID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 获取接受者和申请者的信息
	accepter, err := a.users.GetByID(ctx, fromUserID)
	if err != nil {
		return
	}

	applicant, err := a.users.GetByID(ctx, toUserID)
	if err != nil {
		return
	}

	// 获取或创建与系统的私信线程
	threadID, err := a.direct.GetThreadID(ctx, data.SystemNotificationUID, applicant.ID)
	if err != nil {
		newID := nanoid.New()
		threadID, err = a.direct.GetOrCreateThread(ctx, data.SystemNotificationUID, applicant.ID, newID)
		if err != nil {
			return
		}
	}

	// 发送私信通知给申请者
	msg := &data.DirectMessage{
		ID:       nanoid.New(),
		ThreadID: threadID,
		SenderID: data.SystemNotificationUID,
		Body:     "好友申请已通过：" + accepter.DisplayName + " 接受了你的好友申请",
		MsgType:  "text",
	}

	_ = a.direct.CreateMessage(ctx, msg)
}

func (a *API) sendFriendRejectedNotification(toUserID, fromUserID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 获取拒绝者和申请者的信息
	rejecter, err := a.users.GetByID(ctx, fromUserID)
	if err != nil {
		return
	}

	applicant, err := a.users.GetByID(ctx, toUserID)
	if err != nil {
		return
	}

	// 获取或创建与系统的私信线程
	threadID, err := a.direct.GetThreadID(ctx, data.SystemNotificationUID, applicant.ID)
	if err != nil {
		newID := nanoid.New()
		threadID, err = a.direct.GetOrCreateThread(ctx, data.SystemNotificationUID, applicant.ID, newID)
		if err != nil {
			return
		}
	}

	// 发送私信通知给申请者
	msg := &data.DirectMessage{
		ID:       nanoid.New(),
		ThreadID: threadID,
		SenderID: data.SystemNotificationUID,
		Body:     "好友申请被拒绝：" + rejecter.DisplayName + " 拒绝了你的好友申请",
		MsgType:  "text",
	}

	_ = a.direct.CreateMessage(ctx, msg)
}
