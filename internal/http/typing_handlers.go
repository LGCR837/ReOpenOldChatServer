package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"metrochat/internal/data"
)

const typingTTL = 8 * time.Second

type typingUpdateRequest struct {
	ChatID   string `json:"chat_id"`
	IsTyping bool   `json:"is_typing"`
	IsGroup  bool   `json:"is_group"`
}

type typingUser struct {
	UID      string `json:"uid"`
	IsTyping bool   `json:"is_typing"`
}

type typingResponse struct {
	Users []typingUser `json:"users"`
}

type typingWsPayload struct {
	ChatID   string `json:"chat_id"`
	UID      string `json:"uid"`
	IsGroup  bool   `json:"is_group"`
	IsTyping bool   `json:"is_typing"`
}

type typingEntry struct {
	UID       string
	TargetID  string
	IsGroup   bool
	IsTyping  bool
	UpdatedAt time.Time
}

type typingStore struct {
	mu      sync.Mutex
	entries map[string]*typingEntry
}

func newTypingStore() *typingStore {
	return &typingStore{entries: make(map[string]*typingEntry)}
}

func (s *typingStore) set(uid, targetID string, isGroup, isTyping bool, now time.Time) {
	if uid == "" {
		return
	}
	s.mu.Lock()
	s.entries[uid] = &typingEntry{
		UID:       uid,
		TargetID:  targetID,
		IsGroup:   isGroup,
		IsTyping:  isTyping,
		UpdatedAt: now,
	}
	s.mu.Unlock()
}

func (s *typingStore) directStatus(uid, targetUID string, now time.Time) typingUser {
	if uid == "" {
		return typingUser{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[uid]
	if !ok || entry == nil {
		return typingUser{UID: uid, IsTyping: false}
	}
	if entry.IsGroup {
		return typingUser{UID: uid, IsTyping: false}
	}
	if now.Sub(entry.UpdatedAt) > typingTTL {
		delete(s.entries, uid)
		return typingUser{UID: uid, IsTyping: false}
	}
	if entry.TargetID != targetUID {
		return typingUser{UID: uid, IsTyping: false}
	}
	return typingUser{UID: uid, IsTyping: entry.IsTyping}
}

func (s *typingStore) listGroup(groupID, skipUID string, now time.Time) []typingUser {
	users := []typingUser{}
	if groupID == "" {
		return users
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for uid, entry := range s.entries {
		if entry == nil {
			delete(s.entries, uid)
			continue
		}
		if uid == skipUID {
			continue
		}
		if !entry.IsGroup || entry.TargetID != groupID {
			continue
		}
		if now.Sub(entry.UpdatedAt) > typingTTL {
			users = append(users, typingUser{UID: uid, IsTyping: false})
			delete(s.entries, uid)
			continue
		}
		users = append(users, typingUser{UID: uid, IsTyping: entry.IsTyping})
	}
	return users
}

func (a *API) handleChatTyping(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req typingUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	chatID := strings.ToUpper(strings.TrimSpace(req.ChatID))
	if !isValidUID(chatID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}
	uid := strings.ToUpper(strings.TrimSpace(claims.UID))
	if uid == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	a.typing.set(uid, chatID, false, req.IsTyping, time.Now())
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
	a.broadcastTypingDirect(r.Context(), chatID, uid, req.IsTyping)
}

func (a *API) handleChatTypingStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	chatID := strings.ToUpper(strings.TrimSpace(chiURLParam(r, "chatId")))
	if !isValidUID(chatID) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}
	myUID := strings.ToUpper(strings.TrimSpace(claims.UID))
	if myUID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	user := a.typing.directStatus(chatID, myUID, time.Now())
	writeJSON(w, http.StatusOK, typingResponse{Users: []typingUser{user}})
}

func (a *API) handleGroupTyping(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req typingUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	groupID := strings.ToUpper(strings.TrimSpace(req.ChatID))
	if !isValidGroupID(groupID) {
		writeError(w, http.StatusBadRequest, "invalid_group_id", "invalid group id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
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
	uid := strings.ToUpper(strings.TrimSpace(claims.UID))
	if uid == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	a.typing.set(uid, groupID, true, req.IsTyping, time.Now())
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
	a.broadcastTypingGroup(r.Context(), groupID, claims.Subject, uid, req.IsTyping)
}

func (a *API) handleGroupTypingStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	groupID := strings.ToUpper(strings.TrimSpace(chiURLParam(r, "groupId")))
	if !isValidGroupID(groupID) {
		writeError(w, http.StatusBadRequest, "invalid_group_id", "invalid group id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
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
	myUID := strings.ToUpper(strings.TrimSpace(claims.UID))
	users := a.typing.listGroup(groupID, myUID, time.Now())
	writeJSON(w, http.StatusOK, typingResponse{Users: users})
}

func (a *API) broadcastTypingDirect(ctx context.Context, targetUID, senderUID string, isTyping bool) {
	targetUID = strings.ToUpper(strings.TrimSpace(targetUID))
	senderUID = strings.ToUpper(strings.TrimSpace(senderUID))
	if targetUID == "" || senderUID == "" || targetUID == senderUID {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	user, err := a.users.GetByUID(cctx, targetUID)
	if err != nil || user == nil || user.ID == "" {
		return
	}
	env := wsEnvelope{
		Type: "typing",
		Data: typingWsPayload{
			ChatID:   senderUID,
			UID:      senderUID,
			IsGroup:  false,
			IsTyping: isTyping,
		},
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return
	}
	a.wsHub.BroadcastToUser(user.ID, payload)
}

func (a *API) broadcastTypingGroup(ctx context.Context, groupID, senderID, senderUID string, isTyping bool) {
	groupID = strings.ToUpper(strings.TrimSpace(groupID))
	senderUID = strings.ToUpper(strings.TrimSpace(senderUID))
	if groupID == "" || senderUID == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	memberIDs, err := a.groups.ListMemberIDs(cctx, groupID)
	if err != nil {
		return
	}
	env := wsEnvelope{
		Type: "typing",
		Data: typingWsPayload{
			ChatID:   groupID,
			UID:      senderUID,
			IsGroup:  true,
			IsTyping: isTyping,
		},
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return
	}
	for _, memberID := range memberIDs {
		if memberID == "" || memberID == senderID {
			continue
		}
		a.wsHub.BroadcastToUser(memberID, payload)
	}
}
