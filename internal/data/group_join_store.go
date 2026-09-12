package data

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

const (
	GroupJoinPending  int16 = 0
	GroupJoinAccepted int16 = 1
	GroupJoinDenied   int16 = 2
)

type GroupJoinStore struct {
	db *sqlx.DB
}

func NewGroupJoinStore(db *sqlx.DB) *GroupJoinStore {
	return &GroupJoinStore{db: db}
}

func (s *GroupJoinStore) Create(ctx context.Context, req *GroupJoinRequest) error {
	const q = `
INSERT INTO group_join_requests (id, group_id, user_id, status, created_at)
VALUES (:id, :group_id, :user_id, :status, CURRENT_TIMESTAMP)`

	_, err := s.db.NamedExecContext(ctx, q, req)
	return err
}

func (s *GroupJoinStore) Pending(ctx context.Context, groupID, userID string) (bool, error) {
	const q = `
SELECT 1
FROM group_join_requests
WHERE group_id = $1 AND user_id = $2 AND status = 0
LIMIT 1`

	var exists int
	if err := s.db.GetContext(ctx, &exists, q, groupID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *GroupJoinStore) GetByID(ctx context.Context, id string) (*GroupJoinRequest, error) {
	const q = `
SELECT id, group_id, user_id, status, created_at
FROM group_join_requests
WHERE id = $1
LIMIT 1`

	var req GroupJoinRequest
	if err := s.db.GetContext(ctx, &req, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &req, nil
}

func (s *GroupJoinStore) AcceptAndAdd(ctx context.Context, id string) (string, string, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", "", err
	}

	var groupID, userID string
	row := tx.QueryRowContext(ctx, `
UPDATE group_join_requests
SET status = $1, responded_at = CURRENT_TIMESTAMP
WHERE id = $2 AND status = 0
RETURNING group_id, user_id
`, GroupJoinAccepted, id)
	if err := row.Scan(&groupID, &userID); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO group_members (group_id, user_id, role, joined_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING
`, groupID, userID, GroupRoleMember)
	if err != nil {
		_ = tx.Rollback()
		return "", "", err
	}

	if err := tx.Commit(); err != nil {
		return "", "", err
	}

	return groupID, userID, nil
}

func (s *GroupJoinStore) Deny(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE group_join_requests
SET status = $1, responded_at = CURRENT_TIMESTAMP
WHERE id = $2 AND status = 0
`, GroupJoinDenied, id)
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

func (s *GroupJoinStore) ListPendingByGroup(ctx context.Context, groupID string) ([]GroupJoinRequestEntry, error) {
	const q = `
SELECT gj.id, gj.user_id, u.uid, u.username, u.display_name, u.user_title, u.avatar_url, gj.created_at
FROM group_join_requests gj
JOIN users u ON u.id = gj.user_id
WHERE gj.group_id = $1 AND gj.status = 0
ORDER BY gj.created_at DESC`

	var items []GroupJoinRequestEntry
	if err := s.db.SelectContext(ctx, &items, q, groupID); err != nil {
		return nil, err
	}
	return items, nil
}
