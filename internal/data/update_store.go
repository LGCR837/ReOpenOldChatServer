package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// MaxPTSGap 是客户端本地 pts 落后上限，超过就要求全量重同步而不是补差。
const MaxPTSGap = 10000

// AccountUpdate 是账号级事件流的一行。pts 按收件人独立递增，不是全局单调。
type AccountUpdate struct {
	ID        int64     `db:"id"`
	UserID    string    `db:"user_id"`
	PTS       int64     `db:"pts"`
	PTSCount  int       `db:"pts_count"`
	EventType string    `db:"event_type"`
	Payload   string    `db:"payload"`
	Created   time.Time `db:"created_at"`
}

type UpdateStore struct {
	db *sqlx.DB
}

func NewUpdateStore(db *sqlx.DB) *UpdateStore {
	return &UpdateStore{db: db}
}

// Append 给单个收件人推进 pts 并写入事件。pts 取自 pts_state 的当前值 +1，
// 递增与插入共用一个事务，避免并发发送时两方拿到同一个 pts。
func (s *UpdateStore) Append(ctx context.Context, userID, eventType, payload string, ptsCount int) error {
	if userID == "" {
		return nil
	}
	if ptsCount <= 0 {
		ptsCount = 1
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	key := ptsKey(userID)
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO pts_state (key, value) VALUES ($1, 0)`, key); err != nil {
		return err
	}
	var next int64
	if err := tx.QueryRowxContext(ctx, `SELECT value + $2 FROM pts_state WHERE key = $1`, key, ptsCount).Scan(&next); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pts_state SET value = $2 WHERE key = $1`, key, next); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO account_updates (user_id, pts, pts_count, event_type, payload, created_at)
VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)`, userID, next, ptsCount, eventType, payload); err != nil {
		return err
	}
	return tx.Commit()
}

// AppendMany 给多个收件人各发一份同内容事件（如群消息发给全体成员）。
// 单个收件人失败不影响其余，返回值仅用于日志观察。
func (s *UpdateStore) AppendMany(ctx context.Context, userIDs []string, eventType, payload string, ptsCount int) {
	for _, id := range userIDs {
		_ = s.Append(ctx, id, eventType, payload, ptsCount)
	}
}

// CurrentPTS 返回该账号当前 pts，无记录时视作 0。
func (s *UpdateStore) CurrentPTS(ctx context.Context, userID string) (int64, error) {
	var pts int64
	err := s.db.GetContext(ctx, &pts, `SELECT value FROM pts_state WHERE key = $1`, ptsKey(userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return pts, nil
}

// ListAfter 返回 pts 严格大于 after 的事件，按 pts 升序。limit+1 由调用方判断 has_more。
func (s *UpdateStore) ListAfter(ctx context.Context, userID string, after int64, limit int) ([]AccountUpdate, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var updates []AccountUpdate
	err := s.db.SelectContext(ctx, &updates, `
SELECT id, user_id, pts, pts_count, event_type, payload, created_at
FROM account_updates
WHERE user_id = $1 AND pts > $2
ORDER BY pts
LIMIT $3`, userID, after, limit)
	return updates, err
}

// MinPTS 返回该账号现存最早事件的 pts；无事件时返回 0 与 found=false。
// 用于判断客户端请求的位置是否已被归档清理。
func (s *UpdateStore) MinPTS(ctx context.Context, userID string) (int64, bool, error) {
	var pts int64
	err := s.db.GetContext(ctx, &pts, `SELECT MIN(pts) FROM account_updates WHERE user_id = $1`, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return pts, true, nil
}

func ptsKey(userID string) string {
	return "account:" + userID
}
