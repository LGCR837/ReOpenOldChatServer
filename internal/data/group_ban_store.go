package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type BannedGroupRow struct {
	GroupID     string       `db:"group_id"`
	Name        string       `db:"name"`
	Reason      string       `db:"reason"`
	CreatedAt   time.Time    `db:"created_at"`
	BannedUntil sql.NullTime `db:"banned_until"`
}

type GroupBanStore struct {
	db *sqlx.DB
}

func NewGroupBanStore(db *sqlx.DB) *GroupBanStore {
	return &GroupBanStore{db: db}
}

func (s *GroupBanStore) IsBanned(ctx context.Context, groupID string) (bool, error) {
	if groupID == "" {
		return false, nil
	}
	var found string
	err := s.db.GetContext(ctx, &found, `
SELECT group_id FROM banned_groups
WHERE group_id = $1 AND (banned_until IS NULL OR banned_until > CURRENT_TIMESTAMP)
`, groupID)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

func (s *GroupBanStore) Ban(ctx context.Context, groupID, reason string, durationHours int) error {
	if groupID == "" {
		return nil
	}
	var bannedUntil interface{} = nil
	if durationHours > 0 {
		bannedUntil = time.Now().Add(time.Duration(durationHours) * time.Hour)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO banned_groups (group_id, reason, created_at, banned_until)
VALUES ($1, $2, CURRENT_TIMESTAMP, $3)
ON CONFLICT(group_id) DO UPDATE SET reason = excluded.reason, created_at = CURRENT_TIMESTAMP, banned_until = excluded.banned_until
`, groupID, reason, bannedUntil)
	return err
}

func (s *GroupBanStore) Unban(ctx context.Context, groupID string) error {
	if groupID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM banned_groups WHERE group_id = $1`, groupID)
	return err
}

func (s *GroupBanStore) List(ctx context.Context, limit int) ([]BannedGroupRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []BannedGroupRow{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT b.group_id, g.name, b.reason, b.created_at, b.banned_until
FROM banned_groups b
LEFT JOIN groups g ON g.id = b.group_id
ORDER BY b.created_at DESC
LIMIT $1`, limit); err != nil {
		return nil, err
	}
	return rows, nil
}
