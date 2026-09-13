package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// GroupInvitationEntry 是「我收到的入群邀请」列表项，带群名与邀请人信息便于直接渲染。
type GroupInvitationEntry struct {
	ID             string    `db:"id"`
	GroupID        string    `db:"group_id"`
	GroupName      string    `db:"group_name"`
	InviterID      string    `db:"inviter_id"`
	InviterUID     string    `db:"inviter_uid"`
	InviterNCUID   string    `db:"inviter_ncuid"`
	InviterName    string    `db:"inviter_display_name"`
	InviterAvatar  string    `db:"inviter_avatar_url"`
	InvitationTime time.Time `db:"created_at"`
}

const groupInvitationColumns = `
gi.id, gi.group_id, g.name AS group_name,
gi.inviter_id, u.uid AS inviter_uid, u.ncuid AS inviter_ncuid,
u.display_name AS inviter_display_name, u.avatar_url AS inviter_avatar_url,
gi.created_at`

// CreateInvitation 落一条待处理群邀请，返回 (id, 是否新建)。
// 同一群对同一人已有 pending 时靠部分唯一索引挡掉，返回 added=false 而非报错。
func (s *GroupStore) CreateInvitation(ctx context.Context, groupID, inviterID, inviteeID string) (string, bool, error) {
	if groupID == "" || inviterID == "" || inviteeID == "" {
		return "", false, ErrNotFound
	}
	id := NewID()
	res, err := s.db.ExecContext(ctx, `
INSERT INTO group_invitations (id, group_id, inviter_id, invitee_id, status, created_at)
VALUES ($1, $2, $3, $4, 'pending', CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING
`, id, groupID, inviterID, inviteeID)
	if err != nil {
		return "", false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return "", false, err
	}
	return id, rows > 0, nil
}

// ListPendingInvitations 列出某人所有待处理邀请。
func (s *GroupStore) ListPendingInvitations(ctx context.Context, inviteeID string) ([]GroupInvitationEntry, error) {
	if inviteeID == "" {
		return nil, ErrNotFound
	}
	q := `
SELECT` + groupInvitationColumns + `
FROM group_invitations gi
JOIN groups g ON g.id = gi.group_id
JOIN users u ON u.id = gi.inviter_id
WHERE gi.invitee_id = $1 AND gi.status = 'pending'
ORDER BY gi.created_at DESC
LIMIT 200`

	var out []GroupInvitationEntry
	if err := s.db.SelectContext(ctx, &out, q, inviteeID); err != nil {
		return nil, err
	}
	return out, nil
}

// GetInvitation 取单条邀请，供响应前校验归属与状态。
func (s *GroupStore) GetInvitation(ctx context.Context, invitationID string) (*GroupInvitationEntry, error) {
	if invitationID == "" {
		return nil, ErrNotFound
	}
	q := `
SELECT` + groupInvitationColumns + `
FROM group_invitations gi
JOIN groups g ON g.id = gi.group_id
JOIN users u ON u.id = gi.inviter_id
WHERE gi.id = $1`

	var out GroupInvitationEntry
	if err := s.db.GetContext(ctx, &out, q, invitationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

// RespondInvitation 接受/拒绝邀请。WHERE 带 invitee_id 与 status='pending'，
// 保证只有受邀本人能响应、且不能重复响应（并发下也只会成功一次）。
func (s *GroupStore) RespondInvitation(ctx context.Context, invitationID, inviteeID string, accept bool) (string, error) {
	if invitationID == "" || inviteeID == "" {
		return "", ErrNotFound
	}
	status := "rejected"
	if accept {
		status = "accepted"
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE group_invitations
SET status = $1, responded_at = CURRENT_TIMESTAMP
WHERE id = $2 AND invitee_id = $3 AND status = 'pending'
`, status, invitationID, inviteeID)
	if err != nil {
		return "", err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if rows == 0 {
		return "", ErrNotFound
	}

	var groupID string
	if err := s.db.GetContext(ctx, &groupID, `SELECT group_id FROM group_invitations WHERE id = $1`, invitationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return groupID, nil
}

// SearchMembers 在群内按 uid/ncuid/username/display_name 模糊搜索。
// query 为空时退化为前 limit 个成员，避免客户端分页逻辑要分两套。
func (s *GroupStore) SearchMembers(ctx context.Context, groupID, query string, limit int) ([]GroupMemberEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query = strings.TrimSpace(query)

	const base = `
SELECT u.id, u.uid, u.ncuid, u.username, u.display_name, u.user_title, u.avatar_url, gm.role, gm.joined_at
FROM group_members gm
JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = $1`

	var (
		q     string
		args  []any
		order = `
ORDER BY gm.role DESC, u.username
LIMIT $2`
	)
	if query == "" {
		q = base + order
		args = []any{groupID, limit}
	} else {
		q = base + ` AND (u.uid LIKE $2 OR u.ncuid LIKE $2 OR u.username LIKE $2 OR u.display_name LIKE $2)` + `
ORDER BY gm.role DESC, u.username
LIMIT $3`
		args = []any{groupID, "%" + query + "%", limit}
	}

	var members []GroupMemberEntry
	if err := s.db.SelectContext(ctx, &members, q, args...); err != nil {
		return nil, err
	}
	return members, nil
}
