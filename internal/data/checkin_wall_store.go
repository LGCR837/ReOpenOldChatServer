package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// 签到墙：每日签到后可发一条动态（官方 /v1 独有），他人可点赞与评论。
// 每人每天一条由 UNIQUE(user_id, checkin_date) 保证；点赞一人一次由复合主键保证。

var ErrAlreadyPosted = errors.New("already posted today")

type CheckinWallPost struct {
	ID          string    `db:"id"`
	UserID      string    `db:"user_id"`
	CheckinDate string    `db:"checkin_date"`
	MessageType string    `db:"message_type"`
	ContentText string    `db:"content_text"`
	ImageURL    string    `db:"image_url"`
	ThumbURL    string    `db:"thumb_url"`
	CreatedAt   time.Time `db:"created_at"`
}

type CheckinWallComment struct {
	ID        string    `db:"id"`
	PostID    string    `db:"post_id"`
	UserID    string    `db:"user_id"`
	Body      string    `db:"body"`
	CreatedAt time.Time `db:"created_at"`
}

type CheckinWallStore struct {
	db *sqlx.DB
}

func NewCheckinWallStore(db *sqlx.DB) *CheckinWallStore {
	return &CheckinWallStore{db: db}
}

// CreatePost 插入动态；同日已有帖子时返回 ErrAlreadyPosted。
// 与 DailyCheckIn 同范式：ON CONFLICT DO NOTHING + RowsAffected，避免依赖驱动错误类型。
func (s *CheckinWallStore) CreatePost(ctx context.Context, post *CheckinWallPost) error {
	res, err := s.db.NamedExecContext(ctx, `
INSERT INTO checkin_wall_posts (id, user_id, checkin_date, message_type, content_text, image_url, thumb_url, created_at)
VALUES (:id, :user_id, :checkin_date, :message_type, :content_text, :image_url, :thumb_url, CURRENT_TIMESTAMP)
ON CONFLICT(user_id, checkin_date) DO NOTHING`, post)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAlreadyPosted
	}
	return nil
}

// GetPostByUserDate 取某用户某天的帖子，用于回显 my_post。
func (s *CheckinWallStore) GetPostByUserDate(ctx context.Context, userID, date string) (*CheckinWallPost, error) {
	var p CheckinWallPost
	err := s.db.GetContext(ctx, &p, `SELECT id, user_id, checkin_date, message_type, content_text, image_url, thumb_url, created_at
FROM checkin_wall_posts WHERE user_id = ? AND checkin_date = ?`, userID, date)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// ListPosts 全量列表，最新的在前（官方无分页参数，量级为每日一条×活跃用户，给个上限兜底）。
func (s *CheckinWallStore) ListPosts(ctx context.Context, limit int) ([]CheckinWallPost, error) {
	posts := []CheckinWallPost{}
	err := s.db.SelectContext(ctx, &posts, `SELECT id, user_id, checkin_date, message_type, content_text, image_url, thumb_url, created_at
FROM checkin_wall_posts ORDER BY created_at DESC LIMIT ?`, limit)
	return posts, err
}

func (s *CheckinWallStore) Like(ctx context.Context, postID, userID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO checkin_wall_likes (post_id, user_id, created_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, postID, userID)
	return err
}

func (s *CheckinWallStore) Unlike(ctx context.Context, postID, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM checkin_wall_likes WHERE post_id = ? AND user_id = ?`, postID, userID)
	return err
}

// CountLikes 批量点赞计数，key 为 post_id。
func (s *CheckinWallStore) CountLikes(ctx context.Context, postIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(postIDs) == 0 {
		return out, nil
	}
	q, args, err := sqlx.In(`SELECT post_id, COUNT(1) AS n FROM checkin_wall_likes WHERE post_id IN (?) GROUP BY post_id`, postIDs)
	if err != nil {
		return out, err
	}
	rows := []struct {
		PostID string `db:"post_id"`
		N      int    `db:"n"`
	}{}
	if err := s.db.SelectContext(ctx, &rows, s.db.Rebind(q), args...); err != nil {
		return out, err
	}
	for _, r := range rows {
		out[r.PostID] = r.N
	}
	return out, nil
}

// LikedBy 批量判断当前用户是否已点赞，key 为 post_id。
func (s *CheckinWallStore) LikedBy(ctx context.Context, postIDs []string, userID string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(postIDs) == 0 {
		return out, nil
	}
	q, args, err := sqlx.In(`SELECT post_id FROM checkin_wall_likes WHERE post_id IN (?) AND user_id = ?`, postIDs, userID)
	if err != nil {
		return out, err
	}
	ids := []string{}
	if err := s.db.SelectContext(ctx, &ids, s.db.Rebind(q), args...); err != nil {
		return out, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// ListLikesByPost 点赞列表（官方响应含点赞者资料与时间，按点赞时间倒序）。
func (s *CheckinWallStore) ListLikesByPost(ctx context.Context, postID string, limit int) ([]WallLiker, error) {
	likers := []WallLiker{}
	err := s.db.SelectContext(ctx, &likers, `
SELECT l.user_id AS id, u.uid AS uid, u.username AS username, u.display_name AS display_name,
       u.user_title AS user_title, u.avatar_url AS avatar_url, l.created_at AS created_at
FROM checkin_wall_likes l JOIN users u ON u.id = l.user_id
WHERE l.post_id = ? ORDER BY l.created_at DESC LIMIT ?`, postID, limit)
	return likers, err
}

func (s *CheckinWallStore) AddComment(ctx context.Context, c *CheckinWallComment) error {
	_, err := s.db.NamedExecContext(ctx, `
INSERT INTO checkin_wall_comments (id, post_id, user_id, body, created_at)
VALUES (:id, :post_id, :user_id, :body, CURRENT_TIMESTAMP)`, c)
	return err
}

func (s *CheckinWallStore) GetCommentByID(ctx context.Context, id string) (*CheckinWallComment, error) {
	var c CheckinWallComment
	err := s.db.GetContext(ctx, &c, `SELECT id, post_id, user_id, body, created_at FROM checkin_wall_comments WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// ListCommentsByPost 评论列表，最旧在前（官方样例单条无排序依据，取评论惯例）。
func (s *CheckinWallStore) ListCommentsByPost(ctx context.Context, postID string, limit int) ([]WallCommentRow, error) {
	rows := []WallCommentRow{}
	err := s.db.SelectContext(ctx, &rows, `
SELECT c.id, c.post_id, c.body, c.created_at,
       u.uid AS uid, u.username AS username, u.display_name AS display_name,
       u.user_title AS user_title, u.avatar_url AS avatar_url
FROM checkin_wall_comments c JOIN users u ON u.id = c.user_id
WHERE c.post_id = ? ORDER BY c.created_at ASC LIMIT ?`, postID, limit)
	return rows, err
}

// CountComments 批量评论计数，key 为 post_id。
func (s *CheckinWallStore) CountComments(ctx context.Context, postIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(postIDs) == 0 {
		return out, nil
	}
	q, args, err := sqlx.In(`SELECT post_id, COUNT(1) AS n FROM checkin_wall_comments WHERE post_id IN (?) GROUP BY post_id`, postIDs)
	if err != nil {
		return out, err
	}
	rows := []struct {
		PostID string `db:"post_id"`
		N      int    `db:"n"`
	}{}
	if err := s.db.SelectContext(ctx, &rows, s.db.Rebind(q), args...); err != nil {
		return out, err
	}
	for _, r := range rows {
		out[r.PostID] = r.N
	}
	return out, nil
}

func (s *CheckinWallStore) GetPostByID(ctx context.Context, id string) (*CheckinWallPost, error) {
	var p CheckinWallPost
	err := s.db.GetContext(ctx, &p, `SELECT id, user_id, checkin_date, message_type, content_text, image_url, thumb_url, created_at
FROM checkin_wall_posts WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// HasCheckinOn 判断用户某日是否签到过（发墙前置条件）。
func (s *CheckinWallStore) HasCheckinOn(ctx context.Context, userID, date string) (bool, error) {
	var n int
	err := s.db.GetContext(ctx, &n, `SELECT COUNT(1) FROM user_daily_checkins WHERE user_id = ? AND checkin_date = ?`, userID, date)
	return n > 0, err
}

// CountCheckins 用户历史签到总次数（官方 wall 响应的 checkin_count）。
func (s *CheckinWallStore) CountCheckins(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.GetContext(ctx, &n, `SELECT COUNT(1) FROM user_daily_checkins WHERE user_id = ?`, userID)
	return n, err
}

// 帖子作者聚合信息（官方 user 对象带资料与 ban_count）。
type WallAuthor struct {
	ID          string    `db:"id"`
	UID         string    `db:"uid"`
	Username    string    `db:"username"`
	DisplayName string    `db:"display_name"`
	UserTitle   string    `db:"user_title"`
	AvatarURL   string    `db:"avatar_url"`
	Signature   string    `db:"signature"`
	CoverURL    string    `db:"cover_url"`
	BanCount    int       `db:"ban_count"`
	CreatedAt   time.Time `db:"created_at"`
}

// GetAuthors 批量取作者资料；ban_count 取当前处于封禁期的次数（本地无封禁历史列）。
func (s *CheckinWallStore) GetAuthors(ctx context.Context, userIDs []string) (map[string]WallAuthor, error) {
	out := map[string]WallAuthor{}
	if len(userIDs) == 0 {
		return out, nil
	}
	q, args, err := sqlx.In(`
SELECT u.id, u.uid, u.username, u.display_name, u.user_title, u.avatar_url, u.signature, u.cover_url, u.created_at,
       COALESCE(b.n, 0) AS ban_count
FROM users u
LEFT JOIN (SELECT user_id, COUNT(1) AS n FROM banned_users
           WHERE banned_until IS NULL OR banned_until > CURRENT_TIMESTAMP
           GROUP BY user_id) b ON b.user_id = u.id
WHERE u.id IN (?)`, userIDs)
	if err != nil {
		return out, err
	}
	rows := []WallAuthor{}
	if err := s.db.SelectContext(ctx, &rows, s.db.Rebind(q), args...); err != nil {
		return out, err
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// WallCommentRow 评论列表行（评论者资料不含 signature/cover_url/ban_count，与官方一致）。
type WallCommentRow struct {
	ID          string    `db:"id"`
	PostID      string    `db:"post_id"`
	Body        string    `db:"body"`
	CreatedAt   time.Time `db:"created_at"`
	UID         string    `db:"uid"`
	Username    string    `db:"username"`
	DisplayName string    `db:"display_name"`
	UserTitle   string    `db:"user_title"`
	AvatarURL   string    `db:"avatar_url"`
}

// WallLiker 点赞列表行。
type WallLiker struct {
	ID          string    `db:"id"`
	UID         string    `db:"uid"`
	Username    string    `db:"username"`
	DisplayName string    `db:"display_name"`
	UserTitle   string    `db:"user_title"`
	AvatarURL   string    `db:"avatar_url"`
	CreatedAt   time.Time `db:"created_at"`
}
