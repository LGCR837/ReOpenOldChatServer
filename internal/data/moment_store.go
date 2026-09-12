package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type Moment struct {
	ID       string    `db:"id"`
	UserID   string    `db:"user_id"`
	Body     string    `db:"body"`
	ImageURL string    `db:"image_url"`
	Created  time.Time `db:"created_at"`
}

type MomentComment struct {
	ID        string    `db:"id"`
	MomentID  string    `db:"moment_id"`
	UserID    string    `db:"user_id"`
	Body      string    `db:"body"`
	Created   time.Time `db:"created_at"`
	FromUID   string    `db:"uid"`
	FromName  string    `db:"display_name"`
	FromTitle string    `db:"user_title"`
	AvatarURL string    `db:"avatar_url"`
}

type MomentStore struct {
	db *sqlx.DB
}

func NewMomentStore(db *sqlx.DB) *MomentStore {
	return &MomentStore{db: db}
}

func (s *MomentStore) Create(ctx context.Context, m *Moment) error {
	const q = `
INSERT INTO moments (id, user_id, body, image_url, created_at)
VALUES (:id, :user_id, :body, :image_url, CURRENT_TIMESTAMP)`

	_, err := s.db.NamedExecContext(ctx, q, m)
	return err
}

func (s *MomentStore) ListByUsers(ctx context.Context, userIDs []string, limit int, before time.Time) ([]Moment, error) {
	if len(userIDs) == 0 {
		return []Moment{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if before.IsZero() {
		before = time.Now()
	}

	query, args, err := sqlx.In(`
SELECT id, user_id, body, image_url, created_at
FROM moments
WHERE user_id IN (?) AND created_at < ?
ORDER BY created_at DESC
LIMIT ?`, userIDs, before, limit)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)

	var moments []Moment
	if err := s.db.SelectContext(ctx, &moments, query, args...); err != nil {
		return nil, err
	}
	return moments, nil
}

func (s *MomentStore) ListByUsersWithOffset(ctx context.Context, userIDs []string, limit, offset int) ([]Moment, error) {
	if len(userIDs) == 0 {
		return []Moment{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query, args, err := sqlx.In(`
SELECT id, user_id, body, image_url, created_at
FROM moments
WHERE user_id IN (?)
ORDER BY created_at DESC
LIMIT ? OFFSET ?`, userIDs, limit, offset)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)

	var moments []Moment
	if err := s.db.SelectContext(ctx, &moments, query, args...); err != nil {
		return nil, err
	}
	return moments, nil
}

func (s *MomentStore) GetByID(ctx context.Context, id string) (*Moment, error) {
	var m Moment
	err := s.db.GetContext(ctx, &m, `
SELECT id, user_id, body, image_url, created_at
FROM moments
WHERE id = $1`, id)
	if err == nil {
		return &m, nil
	}
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return nil, err
}

func (s *MomentStore) GetCommentByID(ctx context.Context, id string) (*MomentComment, error) {
	var c MomentComment
	err := s.db.GetContext(ctx, &c, `
SELECT id, moment_id, user_id, body, created_at
FROM moment_comments
WHERE id = $1`, id)
	if err == nil {
		return &c, nil
	}
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return nil, err
}

func (s *MomentStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM moments WHERE id = $1`, id)
	return err
}

func (s *MomentStore) DeleteComment(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
DELETE FROM moment_comments WHERE id = $1`, id)
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

func (s *MomentStore) Like(ctx context.Context, momentID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO moment_likes (moment_id, user_id)
VALUES ($1, $2)`, momentID, userID)
	return err
}

func (s *MomentStore) Unlike(ctx context.Context, momentID, userID string) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM moment_likes WHERE moment_id = $1 AND user_id = $2`, momentID, userID)
	return err
}

func (s *MomentStore) CountLikes(ctx context.Context, momentIDs []string) (map[string]int, error) {
	if len(momentIDs) == 0 {
		return map[string]int{}, nil
	}
	query, args, err := sqlx.In(`
SELECT moment_id, COUNT(*) AS cnt
FROM moment_likes
WHERE moment_id IN (?)
GROUP BY moment_id`, momentIDs)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	rows := []struct {
		MomentID string `db:"moment_id"`
		Count    int    `db:"cnt"`
	}{}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.MomentID] = row.Count
	}
	return out, nil
}

func (s *MomentStore) CountComments(ctx context.Context, momentIDs []string) (map[string]int, error) {
	if len(momentIDs) == 0 {
		return map[string]int{}, nil
	}
	query, args, err := sqlx.In(`
SELECT moment_id, COUNT(*) AS cnt
FROM moment_comments
WHERE moment_id IN (?)
GROUP BY moment_id`, momentIDs)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	rows := []struct {
		MomentID string `db:"moment_id"`
		Count    int    `db:"cnt"`
	}{}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.MomentID] = row.Count
	}
	return out, nil
}

func (s *MomentStore) LikedBy(ctx context.Context, momentIDs []string, userID string) (map[string]bool, error) {
	if len(momentIDs) == 0 {
		return map[string]bool{}, nil
	}
	query, args, err := sqlx.In(`
SELECT moment_id
FROM moment_likes
WHERE moment_id IN (?) AND user_id = ?`, momentIDs, userID)
	if err != nil {
		return nil, err
	}
	query = s.db.Rebind(query)
	var rows []struct {
		MomentID string `db:"moment_id"`
	}
	if err := s.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.MomentID] = true
	}
	return out, nil
}

func (s *MomentStore) AddComment(ctx context.Context, comment *MomentComment) error {
	const q = `
INSERT INTO moment_comments (id, moment_id, user_id, body, created_at)
VALUES (:id, :moment_id, :user_id, :body, CURRENT_TIMESTAMP)`
	_, err := s.db.NamedExecContext(ctx, q, comment)
	return err
}

func (s *MomentStore) ListComments(ctx context.Context, momentID string, limit int, before time.Time) ([]MomentComment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if before.IsZero() {
		before = time.Now()
	}
	const q = `
SELECT c.id, c.moment_id, c.user_id, c.body, c.created_at,
u.uid, u.display_name, u.user_title, u.avatar_url
FROM moment_comments c
JOIN users u ON u.id = c.user_id
WHERE c.moment_id = $1 AND c.created_at < $2
ORDER BY c.created_at DESC
LIMIT $3`
	var comments []MomentComment
	if err := s.db.SelectContext(ctx, &comments, q, momentID, before, limit); err != nil {
		return nil, err
	}
	return comments, nil
}
