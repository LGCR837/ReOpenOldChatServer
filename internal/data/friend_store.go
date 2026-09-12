package data

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

type FriendStore struct {
	db *sqlx.DB
}

func NewFriendStore(db *sqlx.DB) *FriendStore {
	return &FriendStore{db: db}
}

func (s *FriendStore) AreFriends(ctx context.Context, userID, otherID string) (bool, error) {
	const q = `
SELECT 1
FROM friends
WHERE user_id = $1 AND friend_user_id = $2
LIMIT 1`

	var exists int
	err := s.db.GetContext(ctx, &exists, q, userID, otherID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *FriendStore) AddPair(ctx context.Context, userID, otherID string) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO friends (user_id, friend_user_id, created_at)
VALUES ($1, $2, CURRENT_TIMESTAMP), ($2, $1, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING
`, userID, otherID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (s *FriendStore) ListFriendIDs(ctx context.Context, userID string) ([]string, error) {
	const q = `
SELECT friend_user_id
FROM friends
WHERE user_id = $1`

	var ids []string
	if err := s.db.SelectContext(ctx, &ids, q, userID); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *FriendStore) ListFriendUsers(ctx context.Context, userID string) ([]FriendUser, error) {
	const q = `
SELECT u.id, u.uid, u.username, u.display_name, f.remark_name, u.user_title, u.avatar_url,
       f.created_at AS friend_created_at
FROM friends f
JOIN users u ON u.id = f.friend_user_id
WHERE f.user_id = $1
ORDER BY u.username`

	var users []FriendUser
	if err := s.db.SelectContext(ctx, &users, q, userID); err != nil {
		return nil, err
	}
	return users, nil
}

func (s *FriendStore) SetRemark(ctx context.Context, userID, friendID, remark string) error {
	const q = `
UPDATE friends
SET remark_name = $1
WHERE user_id = $2 AND friend_user_id = $3`

	res, err := s.db.ExecContext(ctx, q, remark, userID, friendID)
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

func (s *FriendStore) RemoveFriendship(ctx context.Context, userID, friendID string) error {
	const q = `
DELETE FROM friends
WHERE (user_id = $1 AND friend_user_id = $2)
   OR (user_id = $2 AND friend_user_id = $1)`

	result, err := s.db.ExecContext(ctx, q, userID, friendID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrNotFound
	}

	return nil
}
