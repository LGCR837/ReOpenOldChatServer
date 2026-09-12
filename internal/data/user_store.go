package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("not found")
var ErrUIDTooSoon = errors.New("uid change too soon")

type UserStore struct {
	db *sqlx.DB
}

type UserAdminRow struct {
	ID              string    `db:"id"`
	UID             string    `db:"uid"`
	Username        string    `db:"username"`
	Email           string    `db:"email"`
	UserTitle       string    `db:"user_title"`
	CoinBalance     int       `db:"coin_balance"`
	ReputationScore int       `db:"reputation_score"`
	CreatedAt       time.Time `db:"created_at"`
	Banned          int       `db:"banned"`
}

type UserActiveRow struct {
	ID           string    `db:"id"`
	UID          string    `db:"uid"`
	Username     string    `db:"username"`
	Email        string    `db:"email"`
	LastActivity time.Time `db:"last_activity"`
	DirectCount  int       `db:"direct_count"`
	GroupCount   int       `db:"group_count"`
	MomentCount  int       `db:"moment_count"`
	MessageCount int       `db:"message_count"`
}

type UserDailyCheckInResult struct {
	CheckinDate      string
	AlreadyChecked   bool
	CoinReward       int
	ReputationReward int
	CoinBalance      int
	ReputationScore  int
}

func NewUserStore(db *sqlx.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Create(ctx context.Context, u *User) error {
	if u != nil && u.CoinBalance < 0 {
		u.CoinBalance = 0
	}
	const q = `
INSERT INTO users (
	id, uid, uid_changed_at, email, username, display_name, user_title, user_title_price,
	avatar_url, signature, cover_url, password_hash, token_version, coin_balance, reputation_score
) VALUES (
	:id, :uid, CURRENT_TIMESTAMP, :email, :username, :display_name, :user_title, :user_title_price,
	:avatar_url, :signature, :cover_url, :password_hash, :token_version, :coin_balance, :reputation_score
)`

	_, err := s.db.NamedExecContext(ctx, q, u)
	return err
}

func (s *UserStore) GetByEmailOrUsername(ctx context.Context, identifier string) (*User, error) {
	const q = `
SELECT id, uid, uid_changed_at, email, username, display_name, user_title, user_title_price, avatar_url, signature, cover_url, password_hash, token_version, coin_balance, reputation_score, created_at, updated_at
FROM users
WHERE email = $1 OR username = $1
LIMIT 1`

	var u User
	if err := s.db.GetContext(ctx, &u, q, identifier); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &u, nil
}

func (s *UserStore) GetByID(ctx context.Context, id string) (*User, error) {
	const q = `
SELECT id, uid, uid_changed_at, email, username, display_name, user_title, user_title_price, avatar_url, signature, cover_url, password_hash, token_version, coin_balance, reputation_score, created_at, updated_at
FROM users
WHERE id = $1
LIMIT 1`

	var u User
	if err := s.db.GetContext(ctx, &u, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &u, nil
}

func (s *UserStore) GetByUID(ctx context.Context, uid string) (*User, error) {
	const q = `
SELECT id, uid, uid_changed_at, email, username, display_name, user_title, user_title_price, avatar_url, signature, cover_url, password_hash, token_version, coin_balance, reputation_score, created_at, updated_at
FROM users
WHERE uid = $1
LIMIT 1`

	var u User
	if err := s.db.GetContext(ctx, &u, q, uid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &u, nil
}

func (s *UserStore) UpdateUID(ctx context.Context, id, newUID string, cutoff time.Time) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET uid = $1, uid_changed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $2 AND (uid_changed_at <= $3 OR uid_changed_at = created_at)
`, newUID, id, cutoff)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrUIDTooSoon
	}
	return nil
}

// UpdateUIDForce updates UID without enforcing cooldown.
func (s *UserStore) UpdateUIDForce(ctx context.Context, id, newUID string) error {
	if id == "" || newUID == "" {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET uid = $1, uid_changed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, newUID, id)
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

func (s *UserStore) UpdateProfile(ctx context.Context, id, displayName, avatarURL, signature, coverURL string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET display_name = $1, avatar_url = $2, signature = $3, cover_url = $4, updated_at = CURRENT_TIMESTAMP
WHERE id = $5
`, displayName, avatarURL, signature, coverURL, id)
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

func (s *UserStore) UpdateTitleByUID(ctx context.Context, uid, title string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET user_title = $1, updated_at = CURRENT_TIMESTAMP
WHERE uid = $2
`, title, uid)
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

func (s *UserStore) UpdateTitleWithPriceByUID(ctx context.Context, uid, title string, price int) error {
	if title == "" {
		price = 0
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET user_title = $1, user_title_price = $2, updated_at = CURRENT_TIMESTAMP
WHERE uid = $3
`, title, price, uid)
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

func (s *UserStore) UpdateCoinByUID(ctx context.Context, uid string, balance int) error {
	if balance < 0 {
		balance = 0
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET coin_balance = $1, updated_at = CURRENT_TIMESTAMP
WHERE uid = $2
`, balance, uid)
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

func (s *UserStore) DailyCheckIn(ctx context.Context, userID string, coinReward, reputationReward int) (*UserDailyCheckInResult, error) {
	if userID == "" {
		return nil, ErrNotFound
	}
	if coinReward < 0 {
		coinReward = 0
	}
	today := time.Now().Local().Format("2006-01-02")
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	checkinID := NewID()
	res, err := tx.ExecContext(ctx, `
INSERT INTO user_daily_checkins (
    id, user_id, checkin_date, coin_reward, reputation_reward, created_at
) VALUES (
    $1, $2, $3, $4, $5, CURRENT_TIMESTAMP
)
ON CONFLICT(user_id, checkin_date)
DO NOTHING
`, checkinID, userID, today, coinReward, reputationReward)
	if err != nil {
		return nil, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}

	alreadyChecked := rows == 0
	appliedCoinReward := 0
	appliedReputationReward := 0
	if !alreadyChecked {
		appliedCoinReward = coinReward
		appliedReputationReward = reputationReward
		_, err = tx.ExecContext(ctx, `
UPDATE users
SET coin_balance = coin_balance + $1,
    reputation_score = reputation_score + $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $3
`, appliedCoinReward, appliedReputationReward, userID)
		if err != nil {
			return nil, err
		}
	}

	var balance int
	var reputation int
	err = tx.QueryRowContext(ctx, `
SELECT coin_balance, reputation_score
FROM users
WHERE id = $1
`, userID).Scan(&balance, &reputation)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil

	return &UserDailyCheckInResult{
		CheckinDate:      today,
		AlreadyChecked:   alreadyChecked,
		CoinReward:       appliedCoinReward,
		ReputationReward: appliedReputationReward,
		CoinBalance:      balance,
		ReputationScore:  reputation,
	}, nil
}

func (s *UserStore) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET password_hash = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, passwordHash, id)
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

func (s *UserStore) DeleteByID(ctx context.Context, id string) error {
	if id == "" {
		return ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
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

func (s *UserStore) ListActiveUsers(ctx context.Context, start, end time.Time, limit int) ([]UserActiveRow, error) {
	if limit <= 0 {
		limit = 200
	}
	if start.After(end) {
		start, end = end, start
	}
	startUTC := start.UTC().Format("2006-01-02 15:04:05")
	endUTC := end.UTC().Format("2006-01-02 15:04:05")
	const q = `
WITH activity AS (
	SELECT sender_id AS user_id, created_at AS ts, 'direct' AS src
	FROM direct_messages
	WHERE created_at >= $1 AND created_at <= $2
	UNION ALL
	SELECT sender_id AS user_id, created_at AS ts, 'group' AS src
	FROM group_messages
	WHERE created_at >= $1 AND created_at <= $2
)
SELECT u.id, u.uid, u.username, u.email,
       MAX(activity.ts) AS last_activity,
       SUM(CASE WHEN activity.src = 'direct' THEN 1 ELSE 0 END) AS direct_count,
       SUM(CASE WHEN activity.src = 'group' THEN 1 ELSE 0 END) AS group_count,
       0 AS moment_count,
       COUNT(1) AS message_count
FROM activity
JOIN users u ON u.id = activity.user_id
GROUP BY u.id, u.uid, u.username, u.email
ORDER BY message_count DESC, last_activity DESC
LIMIT $3`
	rows := []UserActiveRow{}
	if err := s.db.SelectContext(ctx, &rows, q, startUTC, endUTC, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *UserStore) IncrementTokenVersion(ctx context.Context, id string) (int, error) {
	if id == "" {
		return 0, ErrNotFound
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE users
SET token_version = token_version + 1, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
`, id)
	if err != nil {
		return 0, err
	}
	var v int
	if err := s.db.GetContext(ctx, &v, `SELECT token_version FROM users WHERE id = $1`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return v, nil
}

func (s *UserStore) GetTokenVersion(ctx context.Context, id string) (int, error) {
	if id == "" {
		return 0, ErrNotFound
	}
	var v int
	if err := s.db.GetContext(ctx, &v, `SELECT token_version FROM users WHERE id = $1`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return v, nil
}

func (s *UserStore) Count(ctx context.Context) (int, error) {
	var count int
	if err := s.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM users WHERE id != 'SYSTEM'`); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *UserStore) ListRecent(ctx context.Context, limit int) ([]UserAdminRow, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	const q = `
SELECT u.id, u.uid, u.username, u.email, u.user_title, u.coin_balance, u.reputation_score, u.created_at,
CASE WHEN b.user_id IS NULL THEN 0 ELSE 1 END AS banned
FROM users u
LEFT JOIN banned_users b ON b.user_id = u.id
ORDER BY u.created_at DESC
LIMIT $1`
	rows := []UserAdminRow{}
	if err := s.db.SelectContext(ctx, &rows, q, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *UserStore) ListAll(ctx context.Context) ([]UserAdminRow, error) {
	const q = `
SELECT u.id, u.uid, u.username, u.email, u.user_title, u.coin_balance, u.reputation_score, u.created_at,
CASE WHEN b.user_id IS NULL THEN 0 ELSE 1 END AS banned
FROM users u
LEFT JOIN banned_users b ON b.user_id = u.id
ORDER BY u.created_at DESC`
	rows := []UserAdminRow{}
	if err := s.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, err
	}
	return rows, nil
}
