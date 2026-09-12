package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type MusicPlazaStore struct {
	db *sqlx.DB
}

type MusicPlazaItem struct {
	ID          string    `db:"id"`
	OwnerID     string    `db:"owner_id"`
	Name        string    `db:"name"`
	SongURL     string    `db:"song_url"`
	CoverURL    string    `db:"cover_url"`
	LyricsURL   string    `db:"lyrics_url"`
	SizeBytes   int64     `db:"size_bytes"`
	DurationMS  int       `db:"duration_ms"`
	PlayCount   int       `db:"play_count"`
	CreatedAt   time.Time `db:"created_at"`
	OwnerUID    string    `db:"owner_uid"`
	OwnerName   string    `db:"owner_name"`
	OwnerTitle  string    `db:"owner_title"`
	OwnerAvatar string    `db:"owner_avatar"`
}

type MusicPlazaComment struct {
	ID        string    `db:"id"`
	ItemID    string    `db:"item_id"`
	UserID    string    `db:"user_id"`
	Body      string    `db:"body"`
	CreatedAt time.Time `db:"created_at"`
	FromUID   string    `db:"uid"`
	FromName  string    `db:"display_name"`
	FromTitle string    `db:"user_title"`
	AvatarURL string    `db:"avatar_url"`
}

func NewMusicPlazaStore(db *sqlx.DB) *MusicPlazaStore {
	return &MusicPlazaStore{db: db}
}

func (s *MusicPlazaStore) Create(ctx context.Context, item *MusicPlazaItem) error {
	const q = `
INSERT INTO music_plaza_items (id, owner_id, name, song_url, cover_url, lyrics_url, size_bytes, duration_ms)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err := s.db.ExecContext(ctx, q,
		item.ID,
		item.OwnerID,
		item.Name,
		item.SongURL,
		item.CoverURL,
		item.LyricsURL,
		item.SizeBytes,
		item.DurationMS,
	)
	return err
}

func (s *MusicPlazaStore) ExistsByOwnerAndSongURL(ctx context.Context, ownerID, songURL string) (bool, error) {
	const q = `SELECT id FROM music_plaza_items WHERE owner_id = $1 AND song_url = $2 LIMIT 1`
	var id string
	err := s.db.GetContext(ctx, &id, q, ownerID, songURL)
	if err == nil {
		return id != "", nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *MusicPlazaStore) GetByID(ctx context.Context, id string) (*MusicPlazaItem, error) {
	const q = `
SELECT m.id, m.owner_id, m.name, m.song_url, m.cover_url, m.lyrics_url, m.size_bytes, m.duration_ms, m.play_count, m.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM music_plaza_items m
JOIN users u ON u.id = m.owner_id
WHERE m.id = $1
LIMIT 1`
	var item MusicPlazaItem
	if err := s.db.GetContext(ctx, &item, q, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (s *MusicPlazaStore) UpdateLyricsURL(ctx context.Context, id, lyricsURL string) error {
	const q = `UPDATE music_plaza_items SET lyrics_url = $2 WHERE id = $1`
	res, err := s.db.ExecContext(ctx, q, id, lyricsURL)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected <= 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MusicPlazaStore) DeleteByOwner(ctx context.Context, id, ownerID string) error {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM music_plaza_items
WHERE id = $1 AND owner_id = $2
`, id, ownerID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected <= 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MusicPlazaStore) DeleteBatchByOwner(ctx context.Context, itemIDs []string, ownerID string) (int64, error) {
	if len(itemIDs) == 0 {
		return 0, nil
	}
	query, args, err := sqlx.In(`
DELETE FROM music_plaza_items
WHERE id IN (?) AND owner_id = ?
`, itemIDs, ownerID)
	if err != nil {
		return 0, err
	}
	query = s.db.Rebind(query)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}

func (s *MusicPlazaStore) List(ctx context.Context, query string, limit, offset int) ([]MusicPlazaItem, error) {
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
	rows := make([]MusicPlazaItem, 0)
	if q == "" {
		const listSQL = `
SELECT m.id, m.owner_id, m.name, m.song_url, m.cover_url, m.lyrics_url, m.size_bytes, m.duration_ms, m.play_count, m.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM music_plaza_items m
JOIN users u ON u.id = m.owner_id
ORDER BY m.created_at DESC
LIMIT $1 OFFSET $2`
		err := s.db.SelectContext(ctx, &rows, listSQL, limit, offset)
		return rows, err
	}

	like := "%" + strings.ToLower(q) + "%"
	const searchSQL = `
SELECT m.id, m.owner_id, m.name, m.song_url, m.cover_url, m.lyrics_url, m.size_bytes, m.duration_ms, m.play_count, m.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM music_plaza_items m
JOIN users u ON u.id = m.owner_id
WHERE lower(m.name) LIKE $1
ORDER BY m.created_at DESC
LIMIT $2 OFFSET $3`
	err := s.db.SelectContext(ctx, &rows, searchSQL, like, limit, offset)
	return rows, err
}

func (s *MusicPlazaStore) Count(ctx context.Context, query string) (int, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		var total int
		err := s.db.GetContext(ctx, &total, `SELECT COUNT(1) FROM music_plaza_items`)
		return total, err
	}
	like := "%" + strings.ToLower(q) + "%"
	var total int
	err := s.db.GetContext(ctx, &total, `SELECT COUNT(1) FROM music_plaza_items WHERE lower(name) LIKE $1`, like)
	return total, err
}

func (s *MusicPlazaStore) ListByOwner(ctx context.Context, ownerID, query string, limit, offset int) ([]MusicPlazaItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	rows := make([]MusicPlazaItem, 0)
	q := strings.TrimSpace(query)
	if q == "" {
		const listSQL = `
SELECT m.id, m.owner_id, m.name, m.song_url, m.cover_url, m.lyrics_url, m.size_bytes, m.duration_ms, m.play_count, m.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM music_plaza_items m
JOIN users u ON u.id = m.owner_id
WHERE m.owner_id = $1
ORDER BY m.created_at DESC
LIMIT $2 OFFSET $3`
		err := s.db.SelectContext(ctx, &rows, listSQL, ownerID, limit, offset)
		return rows, err
	}

	like := "%" + strings.ToLower(q) + "%"
	const searchSQL = `
SELECT m.id, m.owner_id, m.name, m.song_url, m.cover_url, m.lyrics_url, m.size_bytes, m.duration_ms, m.play_count, m.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM music_plaza_items m
JOIN users u ON u.id = m.owner_id
WHERE m.owner_id = $1 AND lower(m.name) LIKE $2
ORDER BY m.created_at DESC
LIMIT $3 OFFSET $4`
	err := s.db.SelectContext(ctx, &rows, searchSQL, ownerID, like, limit, offset)
	return rows, err
}

func (s *MusicPlazaStore) CountByOwner(ctx context.Context, ownerID, query string) (int, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		var total int
		err := s.db.GetContext(ctx, &total, `SELECT COUNT(1) FROM music_plaza_items WHERE owner_id = $1`, ownerID)
		return total, err
	}
	like := "%" + strings.ToLower(q) + "%"
	var total int
	err := s.db.GetContext(ctx, &total, `SELECT COUNT(1) FROM music_plaza_items WHERE owner_id = $1 AND lower(name) LIKE $2`, ownerID, like)
	return total, err
}

func (s *MusicPlazaStore) IncreasePlayCount(ctx context.Context, itemID string) (int, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE music_plaza_items
SET play_count = play_count + 1
WHERE id = $1
`, itemID)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if affected <= 0 {
		return 0, ErrNotFound
	}
	var count int
	if err := s.db.GetContext(ctx, &count, `SELECT play_count FROM music_plaza_items WHERE id = $1`, itemID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return count, nil
}

func (s *MusicPlazaStore) IncreasePlayCountWithLog(ctx context.Context, itemID, userID string) (int, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	res, err := tx.ExecContext(ctx, `
UPDATE music_plaza_items
SET play_count = play_count + 1
WHERE id = $1
`, itemID)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if affected <= 0 {
		return 0, ErrNotFound
	}

	if userID != "" {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO music_plaza_play_logs (id, item_id, user_id, created_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
`, NewID(), itemID, userID); err != nil {
			return 0, err
		}
	}

	var count int
	if err := tx.GetContext(ctx, &count, `SELECT play_count FROM music_plaza_items WHERE id = $1`, itemID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *MusicPlazaStore) ListByPlayCount(ctx context.Context, limit int) ([]MusicPlazaItem, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	rows := make([]MusicPlazaItem, 0)
	const listSQL = `
SELECT m.id, m.owner_id, m.name, m.song_url, m.cover_url, m.lyrics_url, m.size_bytes, m.duration_ms, m.play_count, m.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM music_plaza_items m
JOIN users u ON u.id = m.owner_id
ORDER BY m.play_count DESC, m.created_at DESC
LIMIT $1`
	err := s.db.SelectContext(ctx, &rows, listSQL, limit)
	return rows, err
}

func (s *MusicPlazaStore) ListByPlayCountSince(ctx context.Context, limit int, since time.Time) ([]MusicPlazaItem, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	rows := make([]MusicPlazaItem, 0)
	const listSQL = `
SELECT m.id, m.owner_id, m.name, m.song_url, m.cover_url, m.lyrics_url, m.size_bytes, m.duration_ms,
       p.play_count AS play_count,
       m.created_at,
       u.uid AS owner_uid,
       u.display_name AS owner_name,
       u.user_title AS owner_title,
       u.avatar_url AS owner_avatar
FROM music_plaza_items m
JOIN users u ON u.id = m.owner_id
JOIN (
    SELECT item_id, COUNT(*) AS play_count
    FROM music_plaza_play_logs
    WHERE created_at >= $1
    GROUP BY item_id
) p ON p.item_id = m.id
ORDER BY p.play_count DESC, m.created_at DESC
LIMIT $2`
	err := s.db.SelectContext(ctx, &rows, listSQL, since, limit)
	return rows, err
}

func (s *MusicPlazaStore) Like(ctx context.Context, itemID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO music_plaza_likes (item_id, user_id)
VALUES ($1, $2)
`, itemID, userID)
	return err
}

func (s *MusicPlazaStore) Unlike(ctx context.Context, itemID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM music_plaza_likes
WHERE item_id = $1 AND user_id = $2
`, itemID, userID)
	return err
}

func (s *MusicPlazaStore) CountLikes(ctx context.Context, itemIDs []string) (map[string]int, error) {
	if len(itemIDs) == 0 {
		return map[string]int{}, nil
	}
	query, args, err := sqlx.In(`
SELECT item_id, COUNT(*) AS cnt
FROM music_plaza_likes
WHERE item_id IN (?)
GROUP BY item_id
`, itemIDs)
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

func (s *MusicPlazaStore) CountComments(ctx context.Context, itemIDs []string) (map[string]int, error) {
	if len(itemIDs) == 0 {
		return map[string]int{}, nil
	}
	query, args, err := sqlx.In(`
SELECT item_id, COUNT(*) AS cnt
FROM music_plaza_comments
WHERE item_id IN (?)
GROUP BY item_id
`, itemIDs)
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

func (s *MusicPlazaStore) LikedBy(ctx context.Context, itemIDs []string, userID string) (map[string]bool, error) {
	if len(itemIDs) == 0 {
		return map[string]bool{}, nil
	}
	query, args, err := sqlx.In(`
SELECT item_id
FROM music_plaza_likes
WHERE item_id IN (?) AND user_id = ?
`, itemIDs, userID)
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

func (s *MusicPlazaStore) AddComment(ctx context.Context, comment *MusicPlazaComment) error {
	if comment == nil {
		return ErrNotFound
	}
	_, err := s.db.NamedExecContext(ctx, `
INSERT INTO music_plaza_comments (id, item_id, user_id, body, created_at)
VALUES (:id, :item_id, :user_id, :body, CURRENT_TIMESTAMP)
`, comment)
	return err
}

func (s *MusicPlazaStore) ListComments(ctx context.Context, itemID string, limit int, before time.Time) ([]MusicPlazaComment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if before.IsZero() {
		before = time.Now()
	}
	var comments []MusicPlazaComment
	err := s.db.SelectContext(ctx, &comments, `
SELECT c.id, c.item_id, c.user_id, c.body, c.created_at,
       u.uid, u.display_name, u.user_title, u.avatar_url
FROM music_plaza_comments c
JOIN users u ON u.id = c.user_id
WHERE c.item_id = $1 AND c.created_at < $2
ORDER BY c.created_at DESC
LIMIT $3
`, itemID, before, limit)
	if err != nil {
		return nil, err
	}
	return comments, nil
}

func (s *MusicPlazaStore) GetCommentByID(ctx context.Context, id string) (*MusicPlazaComment, error) {
	var c MusicPlazaComment
	err := s.db.GetContext(ctx, &c, `
SELECT id, item_id, user_id, body, created_at
FROM music_plaza_comments
WHERE id = $1
`, id)
	if err == nil {
		return &c, nil
	}
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return nil, err
}

func (s *MusicPlazaStore) DeleteComment(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM music_plaza_comments
WHERE id = $1
`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected <= 0 {
		return ErrNotFound
	}
	return nil
}
