package data

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type FavoriteItem struct {
	ID        string    `db:"id"`
	UserID    string    `db:"user_id"`
	FavType   string    `db:"fav_type"`
	TargetID  string    `db:"target_id"`
	Title     string    `db:"title"`
	Subtitle  string    `db:"subtitle"`
	MediaURL  string    `db:"media_url"`
	ExtraJSON string    `db:"extra_json"`
	CreatedAt time.Time `db:"created_at"`
}

type FavoriteStore struct {
	db *sqlx.DB
}

func NewFavoriteStore(db *sqlx.DB) *FavoriteStore {
	return &FavoriteStore{db: db}
}

func (s *FavoriteStore) Upsert(ctx context.Context, item *FavoriteItem) error {
	if s == nil || s.db == nil || item == nil {
		return ErrNotFound
	}
	if item.ID == "" {
		item.ID = NewID()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO user_favorites (id, user_id, fav_type, target_id, title, subtitle, media_url, extra_json, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)
ON CONFLICT(user_id, fav_type, target_id)
DO UPDATE SET
    title = excluded.title,
    subtitle = excluded.subtitle,
    media_url = excluded.media_url,
    extra_json = excluded.extra_json,
    created_at = CURRENT_TIMESTAMP
`, item.ID, item.UserID, item.FavType, item.TargetID, item.Title, item.Subtitle, item.MediaURL, item.ExtraJSON)
	return err
}

func (s *FavoriteStore) Delete(ctx context.Context, userID, favType, targetID string) error {
	if s == nil || s.db == nil {
		return ErrNotFound
	}
	_, err := s.db.ExecContext(ctx, `
DELETE FROM user_favorites
WHERE user_id = $1 AND fav_type = $2 AND target_id = $3
`, userID, favType, targetID)
	return err
}

func (s *FavoriteStore) List(ctx context.Context, userID, favType string, limit, offset int) ([]FavoriteItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	favType = strings.TrimSpace(strings.ToLower(favType))
	rows := make([]FavoriteItem, 0)
	if favType == "" {
		err := s.db.SelectContext(ctx, &rows, `
SELECT id, user_id, fav_type, target_id, title, subtitle, media_url, extra_json, created_at
FROM user_favorites
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3
`, userID, limit, offset)
		return rows, err
	}
	err := s.db.SelectContext(ctx, &rows, `
SELECT id, user_id, fav_type, target_id, title, subtitle, media_url, extra_json, created_at
FROM user_favorites
WHERE user_id = $1 AND fav_type = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4
`, userID, favType, limit, offset)
	return rows, err
}

func (s *FavoriteStore) Count(ctx context.Context, userID, favType string) (int, error) {
	favType = strings.TrimSpace(strings.ToLower(favType))
	var total int
	if favType == "" {
		err := s.db.GetContext(ctx, &total, `
SELECT COUNT(1)
FROM user_favorites
WHERE user_id = $1
`, userID)
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return total, err
	}
	err := s.db.GetContext(ctx, &total, `
SELECT COUNT(1)
FROM user_favorites
WHERE user_id = $1 AND fav_type = $2
`, userID, favType)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return total, err
}
