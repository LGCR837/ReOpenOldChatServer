package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"ReOpenOldChatServer/internal/data"
)

type groupInvitationItem struct {
	InvitationID  string `json:"invitation_id"`
	GroupID       string `json:"group_id"`
	GroupName     string `json:"group_name"`
	InviterUID    string `json:"inviter_uid"`
	InviterNCUID  string `json:"inviter_ncuid"`
	InviterName   string `json:"inviter_name"`
	InviterAvatar string `json:"inviter_avatar_url,omitempty"`
	CreatedAt     int64  `json:"created_at"`
}

type groupInvitationsResponse struct {
	Invitations []groupInvitationItem `json:"invitations"`
}

// handleGroupInvitations 列出我收到的待处理入群邀请。
func (a *API) handleGroupInvitations(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := a.groups.ListPendingInvitations(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := groupInvitationsResponse{Invitations: make([]groupInvitationItem, 0, len(rows))}
	for _, it := range rows {
		resp.Invitations = append(resp.Invitations, groupInvitationItem{
			InvitationID:  it.ID,
			GroupID:       it.GroupID,
			GroupName:     it.GroupName,
			InviterUID:    it.InviterUID,
			InviterNCUID:  it.InviterNCUID,
			InviterName:   it.InviterName,
			InviterAvatar: it.InviterAvatar,
			CreatedAt:     it.InvitationTime.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

type groupInvitationRespondRequest struct {
	InvitationID string `json:"invitation_id"`
	Accept       bool   `json:"accept"`
}

// handleGroupInvitationRespond 接受或拒绝入群邀请。接受时才真正入群。
func (a *API) handleGroupInvitationRespond(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req groupInvitationRespondRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	invitationID := strings.TrimSpace(req.InvitationID)
	if invitationID == "" {
		writeError(w, http.StatusBadRequest, "invalid_invitation", "invitation_id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// 先取一次用于校验归属，避免把「不属于我的邀请」和「已响应过的邀请」都报成 not_found
	inv, err := a.groups.GetInvitation(ctx, invitationID)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "invitation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	groupID, err := a.groups.RespondInvitation(ctx, invitationID, claims.Subject, req.Accept)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusConflict, "already_responded", "invitation already responded")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if req.Accept {
		if _, err := a.groups.AddMemberIfAbsent(ctx, groupID, claims.Subject, data.GroupRoleMember); err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		a.emitGroupMembershipChange(ctx, groupID, claims.Subject)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"group_id":   groupID,
		"group_name": inv.GroupName,
		"accepted":   req.Accept,
		"invitation": invitationID,
	})
}

// handleGroupMembersLookup 群内成员搜索（uid/ncuid/username/display_name 模糊匹配）。
// query 为空时退化为前 limit 个成员。
func (a *API) handleGroupMembersLookup(w http.ResponseWriter, r *http.Request) {
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
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	limit := parseLimitUpTo(r.URL.Query().Get("limit"), 200)

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

	members, err := a.groups.SearchMembers(ctx, groupID, query, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := groupMembersResponse{Members: make([]groupMemberItem, 0, len(members))}
	for _, m := range members {
		resp.Members = append(resp.Members, groupMemberItem{
			UID:         m.UID,
			NCUID:       m.NCUID,
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
