package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type EmojiPlazaStore struct {
	db *sqlx.DB
}

type EmojiPlazaItem struct {
	ID          string    `db:"id"`
	OwnerID     string    `db:"owner_id"`
	Name        string    `db:"name"`
	MediaURL    string    `db:"media_url"`
	PackageURL  string    `db:"package_url"`
	CoverURL    string    `db:"cover_url"`
	ItemCount   int       `db:"item_count"`
	IsGIF       int       `db:"is_gif"`
	SizeBytes   int64     `db:"size_bytes"`
	CreatedAt   time.Time `db:"created_at"`
	OwnerUID    string    `db:"owner_uid"`
	OwnerName   string    `db:"owner_name"`
	OwnerTitle  string    `db:"owner_title"`
	OwnerAvatar string    `db:"owner_avatar"`
}

func NewEmojiPlazaStore(db *sqlx.DB) *EmojiPlazaStore {
	return &EmojiPlazaStore{db: db}
}

func (s *EmojiPlazaStore) Create(ctx context.Context, item *EmojiPlazaItem) error {
	const q = `
INSERT INTO emoji_plaza_items (id, owner_id, name, media_url, package_url, cover_url, item_count, is_gif, size_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := s.db.ExecContext(ctx, q,
		item.ID,
		item.OwnerID,
		item.Name,
		item.MediaURL,
		item.PackageURL,
		item.CoverURL,
		item.ItemCount,
		item.IsGIF,
		item.SizeBytes,
	)
	return err
}

func (s *EmojiPlazaStore) ExistsByOwnerAndMediaURL(ctx context.Context, ownerID, mediaURL string) (bool, error) {
	const q = `SELECT id FROM emoji_plaza_items WHERE owner_id = $1 AND media_url = $2 LIMIT 1`
	var id string
	err := s.db.GetContext(ctx, &id, q, ownerID, mediaURL)
	if err == nil {
		return id != "", nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *EmojiPlazaStore) ExistsByOwnerAndPackageURL(ctx context.Context, ownerID, packageURL string) (bool, error) {
	const q = `SELECT id FROM emoji_plaza_items WHERE owner_id = $1 AND package_url = $2 LIMIT 1`
	var id string
	err := s.db.GetContext(ctx, &id, q, ownerID, packageURL)
	if err == nil {
		return id != "", nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *EmojiPlazaStore) GetByID(ctx context.Context, id string) (*EmojiPlazaItem, error) {
	const q = `
SELECT e.id, e.owner_id, e.name, e.media_url, e.package_url, e.cover_url, e.item_count, e.is_gif, e.size_bytes, e.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM emoji_plaza_items e
JOIN users u ON u.id = e.owner_id
WHERE e.id = $1
LIMIT 1`
	var item EmojiPlazaItem
	if err := s.db.GetContext(ctx, &item, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *EmojiPlazaStore) List(ctx context.Context, query string, limit, offset int) ([]EmojiPlazaItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	q := strings.TrimSpace(query)
	rows := make([]EmojiPlazaItem, 0)
	if q == "" {
		const listSQL = `
SELECT e.id, e.owner_id, e.name, e.media_url, e.package_url, e.cover_url, e.item_count, e.is_gif, e.size_bytes, e.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM emoji_plaza_items e
JOIN users u ON u.id = e.owner_id
	ORDER BY e.created_at DESC, e.id DESC
	LIMIT $1 OFFSET $2`
		err := s.db.SelectContext(ctx, &rows, listSQL, limit, offset)
		return rows, err
	}

	like := "%" + strings.ToLower(q) + "%"
	const searchSQL = `
SELECT e.id, e.owner_id, e.name, e.media_url, e.package_url, e.cover_url, e.item_count, e.is_gif, e.size_bytes, e.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM emoji_plaza_items e
JOIN users u ON u.id = e.owner_id
WHERE lower(e.name) LIKE $1
	ORDER BY e.created_at DESC, e.id DESC
	LIMIT $2 OFFSET $3`
	err := s.db.SelectContext(ctx, &rows, searchSQL, like, limit, offset)
	return rows, err
}

func (s *EmojiPlazaStore) ListByOwner(ctx context.Context, ownerID, query string, limit, offset int) ([]EmojiPlazaItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	q := strings.TrimSpace(query)
	rows := make([]EmojiPlazaItem, 0)
	if q == "" {
		const listSQL = `
SELECT e.id, e.owner_id, e.name, e.media_url, e.package_url, e.cover_url, e.item_count, e.is_gif, e.size_bytes, e.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM emoji_plaza_items e
JOIN users u ON u.id = e.owner_id
WHERE e.owner_id = $1
ORDER BY e.created_at DESC, e.id DESC
LIMIT $2 OFFSET $3`
		err := s.db.SelectContext(ctx, &rows, listSQL, ownerID, limit, offset)
		return rows, err
	}

	like := "%" + strings.ToLower(q) + "%"
	const searchSQL = `
SELECT e.id, e.owner_id, e.name, e.media_url, e.package_url, e.cover_url, e.item_count, e.is_gif, e.size_bytes, e.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM emoji_plaza_items e
JOIN users u ON u.id = e.owner_id
WHERE e.owner_id = $1 AND lower(e.name) LIKE $2
ORDER BY e.created_at DESC, e.id DESC
LIMIT $3 OFFSET $4`
	err := s.db.SelectContext(ctx, &rows, searchSQL, ownerID, like, limit, offset)
	return rows, err
}

func (s *EmojiPlazaStore) Count(ctx context.Context, query string) (int, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		var total int
		err := s.db.GetContext(ctx, &total, `SELECT COUNT(1) FROM emoji_plaza_items`)
		return total, err
	}
	like := "%" + strings.ToLower(q) + "%"
	var total int
	err := s.db.GetContext(ctx, &total, `SELECT COUNT(1) FROM emoji_plaza_items WHERE lower(name) LIKE $1`, like)
	return total, err
}

func (s *EmojiPlazaStore) CountByOwner(ctx context.Context, ownerID, query string) (int, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		var total int
		err := s.db.GetContext(ctx, &total, `SELECT COUNT(1) FROM emoji_plaza_items WHERE owner_id = $1`, ownerID)
		return total, err
	}
	like := "%" + strings.ToLower(q) + "%"
	var total int
	err := s.db.GetContext(ctx, &total, `SELECT COUNT(1) FROM emoji_plaza_items WHERE owner_id = $1 AND lower(name) LIKE $2`, ownerID, like)
	return total, err
}

func (s *EmojiPlazaStore) DeleteByID(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM emoji_plaza_items WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected <= 0 {
		return ErrNotFound
	}
	return nil
}

func (s *EmojiPlazaStore) DeleteByOwner(ctx context.Context, id, ownerID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM emoji_plaza_items WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected <= 0 {
		return ErrNotFound
	}
	return nil
}
