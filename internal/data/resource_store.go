package data

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type ResourceSection struct {
	ID            string    `db:"id"`
	Name          string    `db:"name"`
	OwnerID       string    `db:"owner_id"`
	Created       time.Time `db:"created_at"`
	OwnerUID      string    `db:"uid"`
	OwnerName     string    `db:"display_name"`
	OwnerTitle    string    `db:"user_title"`
	OwnerAvatar   string    `db:"avatar_url"`
	ResourceCount int       `db:"resource_count"`
}

type ResourceItem struct {
	ID             string    `db:"id"`
	SectionID      string    `db:"section_id"`
	UploaderID     string    `db:"uploader_id"`
	Name           string    `db:"name"`
	URL            string    `db:"url"`
	SizeBytes      int64     `db:"size_bytes"`
	Created        time.Time `db:"created_at"`
	UploaderUID    string    `db:"uid"`
	UploaderName   string    `db:"display_name"`
	UploaderTitle  string    `db:"user_title"`
	UploaderAvatar string    `db:"avatar_url"`
}

type ResourceComment struct {
	ID        string    `db:"id"`
	ItemID    string    `db:"item_id"`
	UserID    string    `db:"user_id"`
	Body      string    `db:"body"`
	Created   time.Time `db:"created_at"`
	FromUID   string    `db:"uid"`
	FromName  string    `db:"display_name"`
	FromTitle string    `db:"user_title"`
	AvatarURL string    `db:"avatar_url"`
}

type ResourceStore struct {
	db *sqlx.DB
}

func NewResourceStore(db *sqlx.DB) *ResourceStore {
	return &ResourceStore{db: db}
}

func (s *ResourceStore) SumUploaderSize(ctx context.Context, uploaderID string) (int64, error) {
	if uploaderID == "" {
		return 0, nil
	}
	var total int64
	err := s.db.GetContext(ctx, &total, `
SELECT COALESCE(SUM(size_bytes), 0)
FROM resource_items
WHERE uploader_id = $1`, uploaderID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return total, nil
}

func (s *ResourceStore) CreateSection(ctx context.Context, section *ResourceSection) error {
	const q = `
INSERT INTO resource_sections (id, name, owner_id, created_at)
VALUES (:id, :name, :owner_id, CURRENT_TIMESTAMP)`
	_, err := s.db.NamedExecContext(ctx, q, section)
	return err
}

func (s *ResourceStore) CountSectionsByOwner(ctx context.Context, ownerID string) (int, error) {
	var count int
	err := s.db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM resource_sections
WHERE owner_id = $1`, ownerID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}

func (s *ResourceStore) ListSections(ctx context.Context, limit, offset int) ([]ResourceSection, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	const q = `
SELECT s.id, s.name, s.owner_id, s.created_at,
       u.uid, u.display_name, u.user_title, u.avatar_url,
       COUNT(i.id) AS resource_count
FROM resource_sections s
JOIN users u ON u.id = s.owner_id
LEFT JOIN resource_items i ON i.section_id = s.id
GROUP BY s.id
ORDER BY s.created_at DESC
LIMIT $1 OFFSET $2`
	var sections []ResourceSection
	if err := s.db.SelectContext(ctx, &sections, q, limit, offset); err != nil {
		return nil, err
	}
	return sections, nil
}

func (s *ResourceStore) GetSectionByID(ctx context.Context, id string) (*ResourceSection, error) {
	var section ResourceSection
	err := s.db.GetContext(ctx, &section, `
SELECT id, name, owner_id, created_at
FROM resource_sections
WHERE id = $1`, id)
	if err == nil {
		return &section, nil
	}
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return nil, err
}

func (s *ResourceStore) DeleteSection(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM resource_sections WHERE id = $1`, id)
	return err
}

func (s *ResourceStore) CreateItem(ctx context.Context, item *ResourceItem) error {
	const q = `
INSERT INTO resource_items (id, section_id, uploader_id, name, url, size_bytes, created_at)
VALUES (:id, :section_id, :uploader_id, :name, :url, :size_bytes, CURRENT_TIMESTAMP)`
	_, err := s.db.NamedExecContext(ctx, q, item)
	return err
}

func (s *ResourceStore) ListItemsBySection(ctx context.Context, sectionID string, limit, offset int) ([]ResourceItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	const q = `
SELECT i.id, i.section_id, i.uploader_id, i.name, i.url, i.size_bytes, i.created_at,
       u.uid, u.display_name, u.user_title, u.avatar_url
FROM resource_items i
JOIN users u ON u.id = i.uploader_id
WHERE i.section_id = $1
ORDER BY i.created_at DESC
LIMIT $2 OFFSET $3`
	var items []ResourceItem
	if err := s.db.SelectContext(ctx, &items, q, sectionID, limit, offset); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *ResourceStore) SearchItems(ctx context.Context, keyword, sectionID string, limit, offset int) ([]ResourceItem, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []ResourceItem{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	pattern := "%" + strings.ToLower(keyword) + "%"
	query := `
SELECT i.id, i.section_id, i.uploader_id, i.name, i.url, i.size_bytes, i.created_at,
       u.uid, u.display_name, u.user_title, u.avatar_url
FROM resource_items i
JOIN users u ON u.id = i.uploader_id
WHERE LOWER(i.name) LIKE ?`
	args := []interface{}{pattern}
	if sectionID != "" {
		query += " AND i.section_id = ?"
		args = append(args, sectionID)
	}
	query += " ORDER BY i.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	query = s.db.Rebind(query)
	var items []ResourceItem
	if err := s.db.SelectContext(ctx, &items, query, args...); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *ResourceStore) GetItemByID(ctx context.Context, id string) (*ResourceItem, error) {
	var item ResourceItem
	err := s.db.GetContext(ctx, &item, `
SELECT id, section_id, uploader_id, name, url, size_bytes, created_at
FROM resource_items
WHERE id = $1`, id)
	if err == nil {
		return &item, nil
	}
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return nil, err
}

func (s *ResourceStore) DeleteItem(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM resource_items WHERE id = $1`, id)
	return err
}

func (s *ResourceStore) Like(ctx context.Context, itemID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO resource_likes (item_id, user_id)
VALUES ($1, $2)`, itemID, userID)
	return err
}

func (s *ResourceStore) Unlike(ctx context.Context, itemID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM resource_likes WHERE item_id = $1 AND user_id = $2`, itemID, userID)
	return err
}

func (s *ResourceStore) CountLikes(ctx context.Context, itemIDs []string) (map[string]int, error) {
	if len(itemIDs) == 0 {
		return map[string]int{}, nil
	}
	query, args, err := sqlx.In(`
SELECT item_id, COUNT(*) AS cnt
FROM resource_likes
WHERE item_id IN (?)
GROUP BY item_id`, itemIDs)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	rows := []struct {
		ItemID string `db:"item_id"`
		Count  int    `db:"cnt"`
	}{}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.ItemID] = row.Count
	}
	return out, nil
}

func (s *ResourceStore) CountComments(ctx context.Context, itemIDs []string) (map[string]int, error) {
	if len(itemIDs) == 0 {
		return map[string]int{}, nil
	}
	query, args, err := sqlx.In(`
SELECT item_id, COUNT(*) AS cnt
FROM resource_comments
WHERE item_id IN (?)
GROUP BY item_id`, itemIDs)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	rows := []struct {
		ItemID string `db:"item_id"`
		Count  int    `db:"cnt"`
	}{}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.ItemID] = row.Count
	}
	return out, nil
}

func (s *ResourceStore) LikedBy(ctx context.Context, itemIDs []string, userID string) (map[string]bool, error) {
	if len(itemIDs) == 0 {
		return map[string]bool{}, nil
	}
	query, args, err := sqlx.In(`
SELECT item_id
FROM resource_likes
WHERE item_id IN (?) AND user_id = ?`, itemIDs, userID)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	var rows []struct {
		ItemID string `db:"item_id"`
	}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.ItemID] = true
	}
	return out, nil
}

func (s *ResourceStore) AddComment(ctx context.Context, comment *ResourceComment) error {
	const q = `
INSERT INTO resource_comments (id, item_id, user_id, body, created_at)
VALUES (:id, :item_id, :user_id, :body, CURRENT_TIMESTAMP)`
	_, err := s.db.NamedExecContext(ctx, q, comment)
	return err
}

func (s *ResourceStore) ListComments(ctx context.Context, itemID string, limit int, before time.Time) ([]ResourceComment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if before.IsZero() {
		before = time.Now()
	}
	const q = `
SELECT c.id, c.item_id, c.user_id, c.body, c.created_at,
       u.uid, u.display_name, u.user_title, u.avatar_url
FROM resource_comments c
JOIN users u ON u.id = c.user_id
WHERE c.item_id = $1 AND c.created_at < $2
ORDER BY c.created_at DESC
LIMIT $3`
	var comments []ResourceComment
	if err := s.db.SelectContext(ctx, &comments, q, itemID, before, limit); err != nil {
		return nil, err
	}
	return comments, nil
}

func (s *ResourceStore) GetCommentByID(ctx context.Context, id string) (*ResourceComment, error) {
	var c ResourceComment
	err := s.db.GetContext(ctx, &c, `
SELECT id, item_id, user_id, body, created_at
FROM resource_comments
WHERE id = $1`, id)
	if err == nil {
		return &c, nil
	}
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return nil, err
}

func (s *ResourceStore) DeleteComment(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM resource_comments WHERE id = $1`, id)
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
