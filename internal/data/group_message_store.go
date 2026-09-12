package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type GroupMessage struct {
	ID         string    `db:"id"`
	GroupID    string    `db:"group_id"`
	SenderID   string    `db:"sender_id"`
	Body       string    `db:"body"`
	MsgType    string    `db:"msg_type"`
	MediaURL   string    `db:"media_url"`
	ThumbURL   string    `db:"thumb_url"`
	DurationMS int       `db:"duration_ms"`
	Created    time.Time `db:"created_at"`
}

type UnreadGroupMessage struct {
	ID         string    `db:"id"`
	GroupID    string    `db:"group_id"`
	SenderID   string    `db:"sender_id"`
	SenderUID  string    `db:"sender_uid"`
	Body       string    `db:"body"`
	MsgType    string    `db:"msg_type"`
	MediaURL   string    `db:"media_url"`
	ThumbURL   string    `db:"thumb_url"`
	DurationMS int       `db:"duration_ms"`
	Created    time.Time `db:"created_at"`
}

type GroupMessageStore struct {
	db *sqlx.DB
}

func NewGroupMessageStore(db *sqlx.DB) *GroupMessageStore {
	return &GroupMessageStore{db: db}
}

func (s *GroupMessageStore) Create(ctx context.Context, m *GroupMessage) error {
	const q = `
INSERT INTO group_messages (id, group_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at)
VALUES (:id, :group_id, :sender_id, :body, :msg_type, :media_url, :thumb_url, :duration_ms, CURRENT_TIMESTAMP)`

	_, err := s.db.NamedExecContext(ctx, q, m)
	return err
}

func (s *GroupMessageStore) GetByID(ctx context.Context, messageID string) (*GroupMessage, error) {
	var m GroupMessage
	err := s.db.GetContext(ctx, &m, `
SELECT id, group_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at
FROM group_messages
WHERE id = $1
LIMIT 1`, messageID)
	if err == nil {
		return &m, nil
	}
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return nil, err
}

func (s *GroupMessageStore) ListByGroup(ctx context.Context, groupID string, limit int, before time.Time) ([]GroupMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if before.IsZero() {
		before = time.Now()
	}

	const q = `
SELECT id, group_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at
FROM group_messages
WHERE group_id = $1 AND created_at < $2
ORDER BY created_at DESC, id DESC
LIMIT $3`

	var msgs []GroupMessage
	if err := s.db.SelectContext(ctx, &msgs, q, groupID, before, limit); err != nil {
		return nil, err
	}
	return msgs, nil
}

// DeleteMessage deletes a group message by ID and sender ID
func (s *GroupMessageStore) DeleteMessage(ctx context.Context, messageID, senderID string) error {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM group_messages
WHERE id = $1 AND sender_id = $2
`, messageID, senderID)
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

// DeleteMessageByID deletes a group message by ID regardless of sender.
func (s *GroupMessageStore) DeleteMessageByID(ctx context.Context, messageID string) error {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM group_messages
WHERE id = $1
`, messageID)
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

// ListByGroupWithOffset returns group messages using offset-based pagination
func (s *GroupMessageStore) ListByGroupWithOffset(ctx context.Context, groupID string, limit, offset int) ([]GroupMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	const q = `
SELECT id, group_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at
FROM group_messages
WHERE group_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3`

	var msgs []GroupMessage
	if err := s.db.SelectContext(ctx, &msgs, q, groupID, limit, offset); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (s *GroupMessageStore) SearchByGroupWithOffset(ctx context.Context, groupID, keyword, kind string, limit, offset int) ([]GroupMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	kw := "%" + keyword + "%"

	var msgs []GroupMessage
	if kind == "text" {
		const qText = `
SELECT id, group_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at
FROM group_messages
WHERE group_id = $1 AND (body LIKE $2 OR media_url LIKE $2) AND msg_type = 'text'
ORDER BY created_at DESC, id DESC
LIMIT $3 OFFSET $4`
		if err := s.db.SelectContext(ctx, &msgs, qText, groupID, kw, limit, offset); err != nil {
			return nil, err
		}
		return msgs, nil
	}
	if kind == "media" {
		const qMedia = `
SELECT id, group_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at
FROM group_messages
WHERE group_id = $1 AND (body LIKE $2 OR media_url LIKE $2)
  AND msg_type IN ('image', 'video', 'voice', 'resource')
ORDER BY created_at DESC, id DESC
LIMIT $3 OFFSET $4`
		if err := s.db.SelectContext(ctx, &msgs, qMedia, groupID, kw, limit, offset); err != nil {
			return nil, err
		}
		return msgs, nil
	}

	const qAll = `
SELECT id, group_id, sender_id, body, msg_type, media_url, thumb_url, duration_ms, created_at
FROM group_messages
WHERE group_id = $1 AND (body LIKE $2 OR media_url LIKE $2)
ORDER BY created_at DESC, id DESC
LIMIT $3 OFFSET $4`
	if err := s.db.SelectContext(ctx, &msgs, qAll, groupID, kw, limit, offset); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (s *GroupMessageStore) GetMessageOffset(ctx context.Context, groupID, messageID string) (int, error) {
	var exists int
	const qExists = `
SELECT COUNT(1)
FROM group_messages
WHERE group_id = $1 AND id = $2`
	if err := s.db.GetContext(ctx, &exists, qExists, groupID, messageID); err != nil {
		return 0, err
	}
	if exists <= 0 {
		return 0, ErrNotFound
	}

	const qOffset = `
SELECT COUNT(1)
FROM group_messages
WHERE group_id = $1
  AND (
    created_at > (
      SELECT created_at
      FROM group_messages
      WHERE group_id = $1 AND id = $2
      LIMIT 1
    )
    OR (
      created_at = (
        SELECT created_at
        FROM group_messages
        WHERE group_id = $1 AND id = $2
        LIMIT 1
      )
      AND id > $2
    )
  )`

	var offset int
	if err := s.db.GetContext(ctx, &offset, qOffset, groupID, messageID); err != nil {
		return 0, err
	}
	if offset < 0 {
		offset = 0
	}
	return offset, nil
}

func (s *GroupMessageStore) ListUnreadByUser(ctx context.Context, userID string, limit int) ([]UnreadGroupMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
SELECT gm.id, gm.group_id, gm.sender_id, su.uid AS sender_uid,
       gm.body, gm.msg_type, gm.media_url, gm.thumb_url, gm.duration_ms, gm.created_at
FROM group_messages gm
JOIN group_members m ON m.group_id = gm.group_id
JOIN users su ON gm.sender_id = su.id
WHERE m.user_id = $1
  AND gm.sender_id != $1
  AND gm.created_at > COALESCE(m.last_read_at, m.joined_at)
ORDER BY gm.created_at DESC
LIMIT $2`

	var msgs []UnreadGroupMessage
	if err := s.db.SelectContext(ctx, &msgs, q, userID, limit); err != nil {
		return nil, err
	}
	return msgs, nil
}

func (s *GroupMessageStore) UpdateMessageBody(ctx context.Context, messageID, body string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE group_messages
SET body = $1
WHERE id = $2
`, body, messageID)
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
