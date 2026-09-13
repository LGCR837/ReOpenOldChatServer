package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"ReOpenOldChatServer/internal/data"
	"github.com/aidarkhanov/nanoid"
)

// 签到墙：字段与响应格式按官方服务器实测逆向（2026-09-13，测试帖「喵」）。
// 要点：发帖必须当日已签到（403 checkin_required）；每日一条（409 already_posted）；
// created_at 为 RFC3339（time.Time 默认序列化）；官方无分页参数。

type wallUser struct {
	ID          string `json:"id"`
	UID         string `json:"uid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UserTitle   string `json:"user_title"`
	AvatarURL   string `json:"avatar_url"`
	Signature   string `json:"signature"`
	CoverURL    string `json:"cover_url"`
	BanCount    int    `json:"ban_count"`
}

func wallUserFromAuthor(a data.WallAuthor) wallUser {
	return wallUser{
		ID: a.ID, UID: a.UID, Username: a.Username, DisplayName: a.DisplayName,
		UserTitle: a.UserTitle, AvatarURL: a.AvatarURL, Signature: a.Signature,
		CoverURL: a.CoverURL, BanCount: a.BanCount,
	}
}

type wallPostItem struct {
	ID           string    `json:"id"`
	CheckinDate  string    `json:"checkin_date"`
	MessageType  string    `json:"message_type"`
	ContentText  string    `json:"content_text"`
	ImageURL     string    `json:"image_url"`
	ThumbURL     string    `json:"thumb_url"`
	CreatedAt    time.Time `json:"created_at"`
	LikeCount    int       `json:"like_count"`
	CommentCount int       `json:"comment_count"`
	LikedByMe    bool      `json:"liked_by_me"`
	User         wallUser  `json:"user"`
}

type wallCommentItem struct {
	ID        string    `json:"id"`
	PostID    string    `json:"post_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	User      wallUser  `json:"user"`
}

type wallLikeItem struct {
	UID         string    `json:"uid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	UserTitle   string    `json:"user_title"`
	AvatarURL   string    `json:"avatar_url"`
	CreatedAt   time.Time `json:"created_at"`
}

func todayLocal() string {
	return time.Now().Local().Format("2006-01-02")
}

const wallListLimit = 200

// handleCheckinWallGet GET /v1/me/checkin/wall
func (a *API) handleCheckinWallGet(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	today := todayLocal()

	checkedIn, err := a.checkinWall.HasCheckinOn(ctx, claims.Subject, today)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	checkinCount, _ := a.checkinWall.CountCheckins(ctx, claims.Subject)
	myPost, _ := a.checkinWall.GetPostByUserDate(ctx, claims.Subject, today)

	posts, err := a.checkinWall.ListPosts(ctx, wallListLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	postIDs := make([]string, 0, len(posts))
	userIDs := make([]string, 0, len(posts))
	for _, p := range posts {
		postIDs = append(postIDs, p.ID)
		userIDs = append(userIDs, p.UserID)
	}
	likeCounts, _ := a.checkinWall.CountLikes(ctx, postIDs)
	commentCounts, _ := a.checkinWall.CountComments(ctx, postIDs)
	likedByMe, _ := a.checkinWall.LikedBy(ctx, postIDs, claims.Subject)
	authors, _ := a.checkinWall.GetAuthors(ctx, userIDs)

	items := make([]wallPostItem, 0, len(posts))
	for _, p := range posts {
		author := authors[p.UserID]
		items = append(items, wallPostItem{
			ID: p.ID, CheckinDate: p.CheckinDate, MessageType: p.MessageType,
			ContentText: p.ContentText, ImageURL: p.ImageURL, ThumbURL: p.ThumbURL,
			CreatedAt: p.CreatedAt.Local(),
			LikeCount: likeCounts[p.ID], CommentCount: commentCounts[p.ID],
			LikedByMe: likedByMe[p.ID], User: wallUserFromAuthor(author),
		})
	}
	resp := map[string]any{
		"already_posted":    myPost != nil,
		"checked_in":        checkedIn,
		"checkin_count":     checkinCount,
		"checkin_date":      today,
		"featured_messages": items,
		"my_post":           nil,
	}
	if myPost != nil {
		count := likeCounts[myPost.ID]
		cc := commentCounts[myPost.ID]
		lm := likedByMe[myPost.ID]
		author := authors[myPost.UserID]
		resp["my_post"] = wallPostItem{
			ID: myPost.ID, CheckinDate: myPost.CheckinDate, MessageType: myPost.MessageType,
			ContentText: myPost.ContentText, ImageURL: myPost.ImageURL, ThumbURL: myPost.ThumbURL,
			CreatedAt: myPost.CreatedAt.Local(), LikeCount: count, CommentCount: cc,
			LikedByMe: lm, User: wallUserFromAuthor(author),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleCheckinWallPost POST /v1/me/checkin/wall — 请求 {"content_text":"...","image_url":"...","thumb_url":"..."}
func (a *API) handleCheckinWallPost(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req struct {
		ContentText string `json:"content_text"`
		ImageURL    string `json:"image_url"`
		ThumbURL    string `json:"thumb_url"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	contentText := strings.TrimSpace(req.ContentText)
	imageURL := strings.TrimSpace(req.ImageURL)
	if contentText == "" && imageURL == "" {
		writeError(w, http.StatusBadRequest, "invalid_post", "post must contain either text or image")
		return
	}
	if len(contentText) > 2000 {
		writeError(w, http.StatusBadRequest, "invalid_post", "text too long")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	today := todayLocal()

	checkedIn, err := a.checkinWall.HasCheckinOn(ctx, claims.Subject, today)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !checkedIn {
		writeError(w, http.StatusForbidden, "checkin_required", "please check in first")
		return
	}
	msgType := "text"
	if imageURL != "" {
		msgType = "image"
	}
	post := &data.CheckinWallPost{
		ID:          nanoid.New(),
		UserID:      claims.Subject,
		CheckinDate: today,
		MessageType: msgType,
		ContentText: contentText,
		ImageURL:    imageURL,
		ThumbURL:    req.ThumbURL,
	}
	if err := a.checkinWall.CreatePost(ctx, post); err != nil {
		if errors.Is(err, data.ErrAlreadyPosted) {
			writeError(w, http.StatusConflict, "already_posted", "already posted today")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	// created_at 由 DB CURRENT_TIMESTAMP 生成，回读补齐，否则响应是零值时间
	if saved, err := a.checkinWall.GetPostByUserDate(ctx, claims.Subject, today); err == nil {
		post = saved
	}
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"post": wallPostItem{
		ID: post.ID, CheckinDate: post.CheckinDate, MessageType: post.MessageType,
		ContentText: post.ContentText, ImageURL: post.ImageURL, ThumbURL: post.ThumbURL,
		CreatedAt: post.CreatedAt.Local(), LikeCount: 0, CommentCount: 0, LikedByMe: false,
		User: wallUser{
			ID: user.ID, UID: user.UID, Username: user.Username, DisplayName: user.DisplayName,
			UserTitle: user.UserTitle, AvatarURL: user.AvatarURL, Signature: user.Signature,
			CoverURL: user.CoverURL,
		},
	}})
}

// likeState 点赞/取消点赞的统一响应（官方字段）。
func (a *API) wallLikeState(ctx context.Context, postID, userID string) (int, bool, error) {
	counts, err := a.checkinWall.CountLikes(ctx, []string{postID})
	if err != nil {
		return 0, false, err
	}
	liked, err := a.checkinWall.LikedBy(ctx, []string{postID}, userID)
	if err != nil {
		return 0, false, err
	}
	return counts[postID], liked[postID], nil
}

// handleCheckinWallLike POST /v1/me/checkin/wall/like
func (a *API) handleCheckinWallLike(w http.ResponseWriter, r *http.Request) {
	a.wallLikeToggle(w, r, true)
}

// handleCheckinWallUnlike POST /v1/me/checkin/wall/unlike
func (a *API) handleCheckinWallUnlike(w http.ResponseWriter, r *http.Request) {
	a.wallLikeToggle(w, r, false)
}

func (a *API) wallLikeToggle(w http.ResponseWriter, r *http.Request, like bool) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req struct {
		PostID string `json:"post_id"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	postID := strings.TrimSpace(req.PostID)
	if postID == "" {
		writeError(w, http.StatusBadRequest, "invalid_post", "invalid post")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := a.checkinWall.GetPostByID(ctx, postID); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "post not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	var err error
	if like {
		err = a.checkinWall.Like(ctx, postID, claims.Subject)
	} else {
		err = a.checkinWall.Unlike(ctx, postID, claims.Subject)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	count, liked, err := a.wallLikeState(ctx, postID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"like_count": count, "liked_by_me": liked, "post_id": postID,
	})
}

// handleCheckinWallComment POST /v1/me/checkin/wall/comment — 请求 {"post_id":"...","body":"..."}
func (a *API) handleCheckinWallComment(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req struct {
		PostID string `json:"post_id"`
		Body   string `json:"body"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	postID := strings.TrimSpace(req.PostID)
	body := strings.TrimSpace(req.Body)
	if postID == "" || body == "" {
		writeError(w, http.StatusBadRequest, "invalid_comment", "post_id and body required")
		return
	}
	if len(body) > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_comment", "body too long")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := a.checkinWall.GetPostByID(ctx, postID); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "post not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	comment := &data.CheckinWallComment{
		ID:     nanoid.New(),
		PostID: postID,
		UserID: claims.Subject,
		Body:   body,
	}
	if err := a.checkinWall.AddComment(ctx, comment); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"comment": wallCommentItem{
		ID: comment.ID, PostID: comment.PostID, Body: comment.Body,
		CreatedAt: comment.CreatedAt.Local(),
		User: wallUser{
			ID: user.ID, UID: user.UID, Username: user.Username, DisplayName: user.DisplayName,
			UserTitle: user.UserTitle, AvatarURL: user.AvatarURL,
		},
	}})
}

// handleCheckinWallComments GET /v1/me/checkin/wall/comments?post_id=
func (a *API) handleCheckinWallComments(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	postID := strings.TrimSpace(r.URL.Query().Get("post_id"))
	if postID == "" {
		writeError(w, http.StatusBadRequest, "invalid_post", "post_id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := a.checkinWall.ListCommentsByPost(ctx, postID, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	items := make([]wallCommentItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, wallCommentItem{
			ID: row.ID, PostID: row.PostID, Body: row.Body, CreatedAt: row.CreatedAt.Local(),
			User: wallUser{UID: row.UID, Username: row.Username, DisplayName: row.DisplayName,
				UserTitle: row.UserTitle, AvatarURL: row.AvatarURL},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": items})
}

// handleCheckinWallLikes GET /v1/me/checkin/wall/likes?post_id=
func (a *API) handleCheckinWallLikes(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	postID := strings.TrimSpace(r.URL.Query().Get("post_id"))
	if postID == "" {
		writeError(w, http.StatusBadRequest, "invalid_post", "post_id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := a.checkinWall.ListLikesByPost(ctx, postID, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	items := make([]wallLikeItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, wallLikeItem{
			UID: row.UID, Username: row.Username, DisplayName: row.DisplayName,
			UserTitle: row.UserTitle, AvatarURL: row.AvatarURL, CreatedAt: row.CreatedAt.Local(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"likes": items})
}
