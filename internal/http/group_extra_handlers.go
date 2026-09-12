package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"metrochat/internal/data"
)

type groupListItem struct {
	GroupID               string `json:"group_id"`
	Name                  string `json:"name"`
	AvatarURL             string `json:"avatar_url"`
	JoinApproval          bool   `json:"join_approval"`
	GlobalMute            bool   `json:"global_mute"`
	Announcement          string `json:"announcement"`
	AnnouncementMode      int16  `json:"announcement_mode"`
	AnnouncementUpdatedAt int64  `json:"announcement_updated_at"`
	AnnouncementReadAt    int64  `json:"announcement_read_at"`
	MemberCount           int    `json:"member_count"`
	Role                  int16  `json:"role"`
}

type groupListResponse struct {
	Groups []groupListItem `json:"groups"`
}

type groupMemberItem struct {
	UID         string `json:"uid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UserTitle   string `json:"user_title"`
	AvatarURL   string `json:"avatar_url"`
	Role        int16  `json:"role"`
	JoinedAt    int64  `json:"joined_at"`
}

type groupMembersResponse struct {
	Members []groupMemberItem `json:"members"`
}

type groupInviteRequest struct {
	GroupID string `json:"group_id"`
	UserUID string `json:"user_uid"`
}

type groupAdminRequest struct {
	GroupID string `json:"group_id"`
	UserUID string `json:"user_uid"`
	Admin   bool   `json:"admin"`
}

type groupAvatarRequest struct {
	GroupID   string `json:"group_id"`
	AvatarURL string `json:"avatar_url"`
}

type groupJoinRequestItem struct {
	RequestID   string `json:"request_id"`
	UID         string `json:"uid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UserTitle   string `json:"user_title"`
	AvatarURL   string `json:"avatar_url"`
	CreatedAt   int64  `json:"created_at"`
}

type groupJoinRequestsResponse struct {
	Requests []groupJoinRequestItem `json:"requests"`
}

func (a *API) handleGroupList(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	groups, err := a.groups.ListByUser(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := groupListResponse{Groups: make([]groupListItem, 0, len(groups))}
	for _, g := range groups {
		readAt := int64(0)
		if g.AnnouncementReadAt.Valid {
			readAt = g.AnnouncementReadAt.Time.Unix()
		}
		updatedAt := int64(0)
		if !g.AnnouncementUpdatedAt.IsZero() {
			updatedAt = g.AnnouncementUpdatedAt.Unix()
		}
		resp.Groups = append(resp.Groups, groupListItem{
			GroupID:               g.ID,
			Name:                  g.Name,
			AvatarURL:             g.AvatarURL,
			JoinApproval:          g.JoinApproval,
			GlobalMute:            g.GlobalMute,
			Announcement:          g.Announcement,
			AnnouncementMode:      g.AnnouncementMode,
			AnnouncementUpdatedAt: updatedAt,
			AnnouncementReadAt:    readAt,
			MemberCount:           g.MemberCount,
			Role:                  g.Role,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleGroupMembers(w http.ResponseWriter, r *http.Request) {
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

	members, err := a.groups.ListMembers(ctx, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := groupMembersResponse{Members: make([]groupMemberItem, 0, len(members))}
	for _, m := range members {
		resp.Members = append(resp.Members, groupMemberItem{
			UID:         m.UID,
			Username:    m.Username,
			DisplayName: m.DisplayName,
			UserTitle:   m.UserTitle,
			AvatarURL:   m.AvatarURL,
			Role:        m.Role,
			JoinedAt:    m.JoinedAt.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleGroupInvite(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupInviteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
	userUID := strings.ToUpper(strings.TrimSpace(req.UserUID))
	if !isValidGroupID(groupID) || !isValidUID(userUID) {
		writeError(w, http.StatusBadRequest, "invalid_input", "invalid input")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	actorRole, err := a.groups.GetRole(ctx, groupID, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_admin", "not allowed")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if actorRole < data.GroupRoleAdmin {
		writeError(w, http.StatusForbidden, "not_admin", "not allowed")
		return
	}

	targetUser, err := a.users.GetByUID(ctx, userUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	areFriends, err := a.friends.AreFriends(ctx, claims.Subject, targetUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !areFriends {
		writeError(w, http.StatusForbidden, "not_friends", "not friends")
		return
	}

	added, err := a.groups.AddMemberIfAbsent(ctx, groupID, targetUser.ID, data.GroupRoleMember)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !added {
		writeError(w, http.StatusConflict, "already_member", "already member")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleGroupAdmin(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupAdminRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
	userUID := strings.ToUpper(strings.TrimSpace(req.UserUID))
	if !isValidGroupID(groupID) || !isValidUID(userUID) {
		writeError(w, http.StatusBadRequest, "invalid_input", "invalid input")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	actorRole, err := a.groups.GetRole(ctx, groupID, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_owner", "not allowed")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if actorRole != data.GroupRoleOwner {
		writeError(w, http.StatusForbidden, "not_owner", "not allowed")
		return
	}

	targetUser, err := a.users.GetByUID(ctx, userUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	targetRole, err := a.groups.GetRole(ctx, groupID, targetUser.ID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "member_not_found", "member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if targetRole == data.GroupRoleOwner {
		writeError(w, http.StatusForbidden, "cannot_update_owner", "not allowed")
		return
	}

	newRole := data.GroupRoleMember
	if req.Admin {
		newRole = data.GroupRoleAdmin
	}
	if err := a.groups.UpdateRole(ctx, groupID, targetUser.ID, newRole); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "member_not_found", "member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleGroupAvatar(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupAvatarRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
	avatarURL := strings.TrimSpace(req.AvatarURL)
	if !isValidGroupID(groupID) || !isValidAvatarURL(avatarURL) {
		writeError(w, http.StatusBadRequest, "invalid_input", "invalid input")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	role, err := a.groups.GetRole(ctx, groupID, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_owner", "not allowed")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if role != data.GroupRoleOwner {
		writeError(w, http.StatusForbidden, "not_owner", "not allowed")
		return
	}

	if err := a.groups.UpdateAvatar(ctx, groupID, avatarURL); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "group_not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleGroupJoinRequests(w http.ResponseWriter, r *http.Request) {
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
	role, err := a.groups.GetRole(ctx, groupID, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusForbidden, "not_admin", "not allowed")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if role < data.GroupRoleAdmin {
		writeError(w, http.StatusForbidden, "not_admin", "not allowed")
		return
	}

	requests, err := a.groupJoins.ListPendingByGroup(ctx, groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := groupJoinRequestsResponse{Requests: make([]groupJoinRequestItem, 0, len(requests))}
	for _, item := range requests {
		resp.Requests = append(resp.Requests, groupJoinRequestItem{
			RequestID:   item.ID,
			UID:         item.UID,
			Username:    item.Username,
			DisplayName: item.DisplayName,
			UserTitle:   item.UserTitle,
			AvatarURL:   item.AvatarURL,
			CreatedAt:   item.CreatedAt.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}
