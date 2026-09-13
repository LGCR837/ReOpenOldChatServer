package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// 登录会话：token 粒度的设备登录记录。JWT 的 jti 即主键；
// 每次签发 access token 落一行，cleanup-others 吊销其他行保留当前。
// 历史设备累积视图由 user_login_devices 承担（GET /me/devices），本表回答「现在谁还登录着」。

var ErrSessionRevoked = errors.New("session revoked")

type UserSession struct {
	JTI        string       `db:"jti"`
	UserID     string       `db:"user_id"`
	DeviceID   string       `db:"device_id"`
	DeviceName string       `db:"device_name"`
	Platform   string       `db:"platform"`
	AppVersion string       `db:"app_version"`
	CreatedAt  time.Time    `db:"created_at"`
	LastSeen   time.Time    `db:"last_seen"`
	RevokedAt  sql.NullTime `db:"revoked_at"`
}

type SessionStore struct {
	db *sqlx.DB
}

func NewSessionStore(db *sqlx.DB) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) Create(ctx context.Context, sess *UserSession) error {
	_, err := s.db.NamedExecContext(ctx, `
INSERT INTO user_sessions (jti, user_id, device_id, device_name, platform, app_version, created_at, last_seen)
VALUES (:jti, :user_id, :device_id, :device_name, :platform, :app_version, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, sess)
	return err
}

// IsRevoked 会话是否已被吊销。表中不存在的 jti（旧 token、表建立前签发）视为有效，
// 避免上线瞬间误杀全部存量客户端。
func (s *SessionStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	var revoked sql.NullTime
	err := s.db.GetContext(ctx, &revoked, `SELECT revoked_at FROM user_sessions WHERE jti = ?`, jti)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return revoked.Valid, nil
}

// TouchLastSeen 更新会话活跃时间（调用方负责节流）。
func (s *SessionStore) TouchLastSeen(ctx context.Context, jti string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_sessions SET last_seen = CURRENT_TIMESTAMP WHERE jti = ?`, jti)
	return err
}

func (s *SessionStore) Revoke(ctx context.Context, jti string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_sessions SET revoked_at = CURRENT_TIMESTAMP WHERE jti = ? AND revoked_at IS NULL`, jti)
	return err
}

// RevokeOthers 吊销该用户除 keepJTI 外的全部活跃会话，返回吊销条数。
func (s *SessionStore) RevokeOthers(ctx context.Context, userID, keepJTI string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE user_sessions SET revoked_at = CURRENT_TIMESTAMP
WHERE user_id = ? AND jti != ? AND revoked_at IS NULL`, userID, keepJTI)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RevokeAllByUser 吊销该用户全部活跃会话（cleanup 换发新 token 前清场，保持列表干净）。
func (s *SessionStore) RevokeAllByUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_sessions SET revoked_at = CURRENT_TIMESTAMP WHERE user_id = ? AND revoked_at IS NULL`, userID)
	return err
}

// ListActive 该用户全部活跃会话，最新活跃在前。
func (s *SessionStore) ListActive(ctx context.Context, userID string, limit int) ([]UserSession, error) {
	sessions := []UserSession{}
	err := s.db.SelectContext(ctx, &sessions, `
SELECT jti, user_id, device_id, device_name, platform, app_version, created_at, last_seen, revoked_at
FROM user_sessions WHERE user_id = ? AND revoked_at IS NULL
ORDER BY last_seen DESC LIMIT ?`, userID, limit)
	return sessions, err
}
