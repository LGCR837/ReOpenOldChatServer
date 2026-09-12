package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

type RefreshToken struct {
	ID         string         `db:"id"`
	UserID     string         `db:"user_id"`
	TokenHash  string         `db:"token_hash"`
	CreatedAt  time.Time      `db:"created_at"`
	ExpiresAt  time.Time      `db:"expires_at"`
	RevokedAt  sql.NullTime   `db:"revoked_at"`
	ReplacedBy sql.NullString `db:"replaced_by"`
}

type RefreshStore struct {
	db *sqlx.DB
}

func NewRefreshStore(db *sqlx.DB) *RefreshStore {
	return &RefreshStore{db: db}
}

func (s *RefreshStore) Create(ctx context.Context, t *RefreshToken) error {
	const q = `
INSERT INTO refresh_tokens (
	id, user_id, token_hash, created_at, expires_at
) VALUES (
	:id, :user_id, :token_hash, CURRENT_TIMESTAMP, :expires_at
)`

	_, err := s.db.NamedExecContext(ctx, q, t)
	return err
}

func (s *RefreshStore) GetByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	const q = `
SELECT id, user_id, token_hash, created_at, expires_at, revoked_at, replaced_by
FROM refresh_tokens
WHERE token_hash = $1
LIMIT 1`

	var t RefreshToken
	if err := s.db.GetContext(ctx, &t, q, hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &t, nil
}

func (s *RefreshStore) Rotate(ctx context.Context, oldID string, newToken *RefreshToken) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `
UPDATE refresh_tokens
SET revoked_at = CURRENT_TIMESTAMP, replaced_by = $2
WHERE id = $1 AND revoked_at IS NULL
`, oldID, newToken.ID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if rows == 0 {
		_ = tx.Rollback()
		return ErrNotFound
	}

	if _, err := tx.NamedExecContext(ctx, `
INSERT INTO refresh_tokens (
	id, user_id, token_hash, created_at, expires_at
) VALUES (
	:id, :user_id, :token_hash, CURRENT_TIMESTAMP, :expires_at
)`, newToken); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (s *RefreshStore) Revoke(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE refresh_tokens
SET revoked_at = CURRENT_TIMESTAMP
WHERE id = $1 AND revoked_at IS NULL
`, id)
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

func (s *RefreshStore) RevokeAllByUser(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE refresh_tokens
SET revoked_at = CURRENT_TIMESTAMP
WHERE user_id = $1 AND revoked_at IS NULL
`, userID)
	return err
}
