package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	GroupRoleMember int16 = 0
	GroupRoleAdmin  int16 = 1
	GroupRoleOwner  int16 = 2
)

type GroupStore struct {
	db *sqlx.DB
}

type GroupAdminRow struct {
	ID           string       `db:"id"`
	Name         string       `db:"name"`
	OwnerUID     string       `db:"owner_uid"`
	OwnerName    string       `db:"owner_name"`
	MemberCount  int          `db:"member_count"`
	CreatedAt    time.Time    `db:"created_at"`
	Banned       int          `db:"banned"`
	BannedUntil  sql.NullTime `db:"banned_until"`
	BannedReason string       `db:"banned_reason"`
}

func NewGroupStore(db *sqlx.DB) *GroupStore {
	return &GroupStore{db: db}
}

func (s *GroupStore) Create(ctx context.Context, g *Group, ownerID string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO groups (id, name, avatar_url, owner_id, join_approval, global_mute, announcement, announcement_mode, announcement_updated_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
`, g.ID, g.Name, g.AvatarURL, ownerID, g.JoinApproval, g.GlobalMute, g.Announcement, g.AnnouncementMode)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO group_members (group_id, user_id, role, joined_at, last_read_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
`, g.ID, ownerID, GroupRoleOwner)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (s *GroupStore) GetByID(ctx context.Context, groupID string) (*Group, error) {
	const q = `
SELECT g.id, g.name, g.avatar_url, g.owner_id, g.join_approval, g.global_mute, g.announcement, g.announcement_mode, g.announcement_updated_at, g.created_at, g.updated_at
FROM groups g
LEFT JOIN banned_groups b ON b.group_id = g.id AND (b.banned_until IS NULL OR b.banned_until > CURRENT_TIMESTAMP)
WHERE g.id = $1 AND b.group_id IS NULL
LIMIT 1`

	var g Group
	if err := s.db.GetContext(ctx, &g, q, groupID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &g, nil
}

func (s *GroupStore) GetRole(ctx context.Context, groupID, userID string) (int16, error) {
	const q = `
SELECT gm.role
FROM group_members gm
JOIN groups g ON g.id = gm.group_id
LEFT JOIN banned_groups b ON b.group_id = g.id AND (b.banned_until IS NULL OR b.banned_until > CURRENT_TIMESTAMP)
WHERE gm.group_id = $1 AND gm.user_id = $2 AND b.group_id IS NULL
LIMIT 1`

	var role int16
	if err := s.db.GetContext(ctx, &role, q, groupID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return role, nil
}

func (s *GroupStore) AddMember(ctx context.Context, groupID, userID string, role int16) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO group_members (group_id, user_id, role, joined_at, last_read_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING
`, groupID, userID, role)
	return err
}

func (s *GroupStore) AddMemberIfAbsent(ctx context.Context, groupID, userID string, role int16) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO group_members (group_id, user_id, role, joined_at, last_read_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING
`, groupID, userID, role)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func (s *GroupStore) MarkRead(ctx context.Context, groupID, userID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE group_members
SET last_read_at = CURRENT_TIMESTAMP
WHERE group_id = $1 AND user_id = $2
`, groupID, userID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupStore) RemoveMember(ctx context.Context, groupID, userID string) error {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM group_members
WHERE group_id = $1 AND user_id = $2
`, groupID, userID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupStore) UpdateName(ctx context.Context, groupID, name string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE groups
SET name = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, name, groupID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupStore) UpdateAvatar(ctx context.Context, groupID, avatarURL string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE groups
SET avatar_url = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, avatarURL, groupID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupStore) UpdateSettings(ctx context.Context, groupID string, joinApproval, globalMute bool) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE groups
SET join_approval = $1, global_mute = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $3
`, joinApproval, globalMute, groupID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupStore) UpdateAnnouncement(ctx context.Context, groupID, announcement string, mode int16) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE groups
SET announcement = $1, announcement_mode = $2, announcement_updated_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $3
`, announcement, mode, groupID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupStore) MarkAnnouncementRead(ctx context.Context, groupID, userID string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE group_members
SET announcement_read_at = CURRENT_TIMESTAMP
WHERE group_id = $1 AND user_id = $2
`, groupID, userID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupStore) ListMemberIDs(ctx context.Context, groupID string) ([]string, error) {
	const q = `
SELECT user_id
FROM group_members
WHERE group_id = $1`

	var ids []string
	if err := s.db.SelectContext(ctx, &ids, q, groupID); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *GroupStore) ListByUser(ctx context.Context, userID string) ([]GroupSummary, error) {
	const q = `
SELECT g.id, g.name, g.avatar_url, g.owner_id, g.join_approval, g.global_mute,
       g.announcement, g.announcement_mode, g.announcement_updated_at, gm.announcement_read_at,
       (SELECT COUNT(1) FROM group_members gm2 WHERE gm2.group_id = g.id) AS member_count,
       gm.role
FROM group_members gm
JOIN groups g ON g.id = gm.group_id
LEFT JOIN banned_groups b ON b.group_id = g.id AND (b.banned_until IS NULL OR b.banned_until > CURRENT_TIMESTAMP)
WHERE gm.user_id = $1 AND b.group_id IS NULL
ORDER BY g.updated_at DESC`

	var groups []GroupSummary
	if err := s.db.SelectContext(ctx, &groups, q, userID); err != nil {
		return nil, err
	}
	return groups, nil
}

func (s *GroupStore) ListAdmin(ctx context.Context, limit int) ([]GroupAdminRow, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	const q = `
SELECT g.id, g.name,
       u.uid AS owner_uid,
       u.username AS owner_name,
       (SELECT COUNT(1) FROM group_members gm2 WHERE gm2.group_id = g.id) AS member_count,
       g.created_at,
       CASE WHEN b.group_id IS NULL THEN 0 ELSE 1 END AS banned,
       b.banned_until AS banned_until,
       COALESCE(b.reason, '') AS banned_reason
FROM groups g
JOIN users u ON u.id = g.owner_id
LEFT JOIN banned_groups b ON b.group_id = g.id AND (b.banned_until IS NULL OR b.banned_until > CURRENT_TIMESTAMP)
ORDER BY g.created_at DESC
LIMIT $1`
	rows := []GroupAdminRow{}
	if err := s.db.SelectContext(ctx, &rows, q, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *GroupStore) ListMembers(ctx context.Context, groupID string) ([]GroupMemberEntry, error) {
	const q = `
SELECT u.id, u.uid, u.username, u.display_name, u.user_title, u.avatar_url, gm.role, gm.joined_at
FROM group_members gm
JOIN users u ON u.id = gm.user_id
WHERE gm.group_id = $1
ORDER BY gm.role DESC, u.username`

	var members []GroupMemberEntry
	if err := s.db.SelectContext(ctx, &members, q, groupID); err != nil {
		return nil, err
	}
	return members, nil
}

func (s *GroupStore) UpdateRole(ctx context.Context, groupID, userID string, role int16) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE group_members
SET role = $1
WHERE group_id = $2 AND user_id = $3
`, role, groupID, userID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *GroupStore) Delete(ctx context.Context, groupID string) error {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM groups
WHERE id = $1
`, groupID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
