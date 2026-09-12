package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	FriendRequestPending  int16 = 0
	FriendRequestAccepted int16 = 1
	FriendRequestDenied   int16 = 2
)

type FriendRequestStore struct {
	db *sqlx.DB
}

func NewFriendRequestStore(db *sqlx.DB) *FriendRequestStore {
	return &FriendRequestStore{db: db}
}

func (s *FriendRequestStore) Create(ctx context.Context, fr *FriendRequest) error {
	const q = `
INSERT INTO friend_requests (
	id, from_user_id, to_user_id, status, created_at
) VALUES (
	:id, :from_user_id, :to_user_id, :status, CURRENT_TIMESTAMP
)`

	_, err := s.db.NamedExecContext(ctx, q, fr)
	return err
}

func (s *FriendRequestStore) PendingBetween(ctx context.Context, aID, bID string) (bool, error) {
	const q = `
SELECT 1
FROM friend_requests
WHERE status = 0 AND (
	(from_user_id = $1 AND to_user_id = $2) OR
	(from_user_id = $2 AND to_user_id = $1)
)
LIMIT 1`

	var exists int
	err := s.db.GetContext(ctx, &exists, q, aID, bID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *FriendRequestStore) Accept(ctx context.Context, id, toUserID string) (string, error) {
	return s.updateStatus(ctx, id, toUserID, FriendRequestAccepted)
}

func (s *FriendRequestStore) AcceptAndAddFriend(ctx context.Context, id, toUserID string) (string, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", err
	}

	var fromID string
	row := tx.QueryRowContext(ctx, `
UPDATE friend_requests
SET status = $1, responded_at = CURRENT_TIMESTAMP
WHERE id = $2 AND to_user_id = $3 AND status = 0
RETURNING from_user_id
`, FriendRequestAccepted, id, toUserID)
	if err := row.Scan(&fromID); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO friends (user_id, friend_user_id, created_at)
VALUES ($1, $2, CURRENT_TIMESTAMP), ($2, $1, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING
`, toUserID, fromID)
	if err != nil {
		_ = tx.Rollback()
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return fromID, nil
}

func (s *FriendRequestStore) Deny(ctx context.Context, id, toUserID string) (string, error) {
	return s.updateStatus(ctx, id, toUserID, FriendRequestDenied)
}

func (s *FriendRequestStore) DeletePendingBefore(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM friend_requests
WHERE status = 0 AND created_at < $1
`, cutoff)
	return err
}

func (s *FriendRequestStore) ListIncoming(ctx context.Context, toUserID string) ([]FriendRequestEntry, error) {
	const q = `
SELECT fr.id, fr.status, fr.created_at, fr.responded_at,
       u.id AS from_user_id, u.uid AS from_uid, u.username AS from_username,
       u.display_name AS from_display_name, u.user_title AS from_user_title, u.avatar_url AS from_avatar_url
FROM friend_requests fr
JOIN users u ON u.id = fr.from_user_id
WHERE fr.to_user_id = $1
ORDER BY fr.created_at DESC`

	var items []FriendRequestEntry
	if err := s.db.SelectContext(ctx, &items, q, toUserID); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *FriendRequestStore) updateStatus(ctx context.Context, id, toUserID string, status int16) (string, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", err
	}

	var fromID string
	row := tx.QueryRowContext(ctx, `
UPDATE friend_requests
SET status = $1, responded_at = CURRENT_TIMESTAMP
WHERE id = $2 AND to_user_id = $3 AND status = 0
RETURNING from_user_id
`, status, id, toUserID)
	if err := row.Scan(&fromID); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return fromID, nil
}
