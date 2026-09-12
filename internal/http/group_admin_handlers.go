package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"ReOpenOldChatServer/internal/data"
)

type groupKickRequest struct {
	GroupID string `json:"group_id"`
	UserUID string `json:"user_uid"`
	ToNCUID string `json:"to_ncuid"`
	NCUID   string `json:"ncuid"`
}

type groupRenameRequest struct {
	GroupID string `json:"group_id"`
	Name    string `json:"name"`
}

type groupSettingsRequest struct {
	GroupID      string `json:"group_id"`
	JoinApproval bool   `json:"join_approval"`
	GlobalMute   bool   `json:"global_mute"`
}

type groupAnnouncementRequest struct {
	GroupID          string `json:"group_id"`
	Announcement     string `json:"announcement"`
	AnnouncementMode int16  `json:"announcement_mode"`
}

type groupAnnouncementReadRequest struct {
	GroupID string `json:"group_id"`
}

func (a *API) handleGroupKick(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupKickRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
	userUID := strings.ToUpper(strings.TrimSpace(req.UserUID))
	userNCUID := groupTargetNCUID(req.ToNCUID, req.NCUID)
	if !isValidGroupID(groupID) || (userNCUID == "" && !isValidUID(userUID)) {
		writeError(w, http.StatusBadRequest, "invalid_input", "invalid input")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	targetUser, err := a.lookupPublicID(ctx, userUID, userNCUID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

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
		writeError(w, http.StatusForbidden, "cannot_kick_owner", "not allowed")
		return
	}
	if actorRole == data.GroupRoleAdmin && targetRole == data.GroupRoleAdmin {
		writeError(w, http.StatusForbidden, "cannot_kick_admin", "not allowed")
		return
	}

	if err := a.groups.RemoveMember(ctx, groupID, targetUser.ID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "member_not_found", "member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
	a.emitGroupMembershipChange(ctx, groupID, targetUser.ID)
}

func (a *API) handleGroupRename(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupRenameRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
	name := strings.TrimSpace(req.Name)
	if !isValidGroupID(groupID) || !isValidGroupName(name) {
		writeError(w, http.StatusBadRequest, "invalid_input", "invalid input")
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

	if err := a.groups.UpdateName(ctx, groupID, name); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "group_not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleGroupSettings(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupSettingsRequest
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

	if err := a.groups.UpdateSettings(ctx, groupID, req.JoinApproval, req.GlobalMute); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "group_not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleGroupAnnouncement(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupAnnouncementRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.ToUpper(strings.TrimSpace(req.GroupID))
	announcement := strings.TrimSpace(req.Announcement)
	if !isValidGroupID(groupID) {
		writeError(w, http.StatusBadRequest, "invalid_group_id", "invalid group id")
		return
	}
	mode := req.AnnouncementMode
	if mode != 0 && mode != 1 {
		mode = 0
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

	if err := a.groups.UpdateAnnouncement(ctx, groupID, announcement, mode); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "group_not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleGroupAnnouncementRead(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupAnnouncementReadRequest
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

	if err := a.groups.MarkAnnouncementRead(ctx, groupID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_member", "not a member")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

type groupDissolveRequest struct {
	GroupID string `json:"group_id"`
}

func (a *API) handleGroupDissolve(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupDissolveRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	groupID := strings.TrimSpace(req.GroupID)
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "invalid_group_id", "invalid group id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	_, err := a.groups.GetByID(ctx, groupID)
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
			writeError(w, http.StatusForbidden, "not_owner", "not allowed")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if role != data.GroupRoleOwner {
		writeError(w, http.StatusForbidden, "not_owner", "only owner can dissolve group")
		return
	}

	// 解散前先取成员名单：群删掉后 group_members 会被级联清空，届时无从得知通知谁
	memberIDs := a.groupMemberIDs(ctx, groupID)

	if err := a.groups.Delete(ctx, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
	a.emitAccountEventMany(memberIDs, "GROUP_MEMBERSHIP_CHANGE", map[string]any{
		"group_id":  groupID,
		"hint_type": "dissolved",
	}, 1)
}
