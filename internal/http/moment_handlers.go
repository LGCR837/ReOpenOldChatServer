package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/data"
)

type momentCreateRequest struct {
	Body      string   `json:"body"`
	ImageURL  string   `json:"image_url"`
	ImageURLs []string `json:"image_urls"`
}

type momentResponse struct {
	ID         string `json:"id"`
	FromUID    string `json:"from_uid"`
	FromName   string `json:"from_name,omitempty"`
	FromTitle  string `json:"from_title,omitempty"`
	FromAvatar string `json:"from_avatar,omitempty"`
	Body       string `json:"body"`
	ImageURL   string `json:"image_url"`
	CreatedAt  int64  `json:"created_at"`
	Likes      int    `json:"likes"`
	Comments   int    `json:"comments"`
	Liked      bool   `json:"liked"`
}

type momentFeedResponse struct {
	Moments []momentResponse `json:"moments"`
}

type momentLikeRequest struct {
	MomentID string `json:"moment_id"`
}

type momentLikeResponse struct {
	MomentID string `json:"moment_id"`
	Liked    bool   `json:"liked"`
	Likes    int    `json:"likes"`
}

type momentDeleteRequest struct {
	MomentID string `json:"moment_id"`
}

type momentCommentRequest struct {
	MomentID string `json:"moment_id"`
	Body     string `json:"body"`
}

type momentCommentDeleteRequest struct {
	CommentID string `json:"comment_id"`
}

type momentCommentResponse struct {
	ID         string `json:"id"`
	MomentID   string `json:"moment_id"`
	FromUID    string `json:"from_uid"`
	FromName   string `json:"from_name"`
	FromTitle  string `json:"from_title"`
	FromAvatar string `json:"from_avatar"`
	Body       string `json:"body"`
	CreatedAt  int64  `json:"created_at"`
}

type momentCommentListResponse struct {
	Comments []momentCommentResponse `json:"comments"`
}

func (a *API) handleMomentCreate(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req momentCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	body := strings.TrimSpace(req.Body)
	imageURL, ok := normalizeMomentImageInput(req.ImageURL, req.ImageURLs)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_image_url", "invalid image url")
		return
	}
	if !isValidMomentBody(body) && imageURL == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	if body != "" && !isValidMomentBody(body) {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	id := nanoid.New()

	m := &data.Moment{
		ID:       id,
		UserID:   claims.Subject,
		Body:     body,
		ImageURL: imageURL,
	}
	if err := a.moments.Create(ctx, m); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := momentResponse{
		ID:         id,
		FromUID:    claims.UID,
		FromName:   user.DisplayName,
		FromTitle:  user.UserTitle,
		FromAvatar: user.AvatarURL,
		Body:       body,
		ImageURL:   imageURL,
		CreatedAt:  time.Now().Unix(),
		Likes:      0,
		Comments:   0,
		Liked:      false,
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (a *API) handleMomentFeed(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	before := parseBefore(r.URL.Query().Get("before"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	friendIDs, err := a.friends.ListFriendIDs(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	userIDs := make([]string, 0, len(friendIDs)+1)
	userIDs = append(userIDs, claims.Subject)
	userIDs = append(userIDs, friendIDs...)

	moments, err := a.moments.ListByUsers(ctx, userIDs, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	momentIDs := make([]string, 0, len(moments))
	for _, m := range moments {
		momentIDs = append(momentIDs, m.ID)
	}
	likeCounts, _ := a.moments.CountLikes(ctx, momentIDs)
	commentCounts, _ := a.moments.CountComments(ctx, momentIDs)
	likedMap, _ := a.moments.LikedBy(ctx, momentIDs, claims.Subject)

	type authorInfo struct {
		UID       string
		Name      string
		Title     string
		AvatarURL string
	}
	authorCache := map[string]authorInfo{
		claims.Subject: {UID: claims.UID, Name: "", Title: "", AvatarURL: ""},
	}

	resp := make([]momentResponse, 0, len(moments))
	for _, m := range moments {
		author, ok := authorCache[m.UserID]
		if !ok || author.UID == "" || (author.Name == "" && author.AvatarURL == "" && author.Title == "") {
			user, err := a.users.GetByID(ctx, m.UserID)
			if err != nil {
				author = authorInfo{UID: "", Name: "", Title: "", AvatarURL: ""}
			} else {
				author = authorInfo{UID: user.UID, Name: user.DisplayName, Title: user.UserTitle, AvatarURL: user.AvatarURL}
				authorCache[m.UserID] = author
			}
		}
		resp = append(resp, momentResponse{
			ID:         m.ID,
			FromUID:    author.UID,
			FromName:   author.Name,
			FromTitle:  author.Title,
			FromAvatar: author.AvatarURL,
			Body:       m.Body,
			ImageURL:   m.ImageURL,
			CreatedAt:  m.Created.Unix(),
			Likes:      likeCounts[m.ID],
			Comments:   commentCounts[m.ID],
			Liked:      likedMap[m.ID],
		})
	}

	writeJSON(w, http.StatusOK, momentFeedResponse{Moments: resp})
}

func (a *API) handleMomentFeedV2(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	friendIDs, err := a.friends.ListFriendIDs(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	userIDs := make([]string, 0, len(friendIDs)+1)
	userIDs = append(userIDs, claims.Subject)
	userIDs = append(userIDs, friendIDs...)

	moments, err := a.moments.ListByUsersWithOffset(ctx, userIDs, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	momentIDs := make([]string, 0, len(moments))
	for _, m := range moments {
		momentIDs = append(momentIDs, m.ID)
	}
	likeCounts, _ := a.moments.CountLikes(ctx, momentIDs)
	commentCounts, _ := a.moments.CountComments(ctx, momentIDs)
	likedMap, _ := a.moments.LikedBy(ctx, momentIDs, claims.Subject)

	type authorInfo struct {
		UID       string
		Name      string
		Title     string
		AvatarURL string
	}
	authorCache := map[string]authorInfo{
		claims.Subject: {UID: claims.UID, Name: "", Title: "", AvatarURL: ""},
	}

	resp := make([]momentResponse, 0, len(moments))
	for _, m := range moments {
		author, ok := authorCache[m.UserID]
		if !ok || author.UID == "" || (author.Name == "" && author.AvatarURL == "" && author.Title == "") {
			user, err := a.users.GetByID(ctx, m.UserID)
			if err != nil {
				author = authorInfo{UID: "", Name: "", Title: "", AvatarURL: ""}
			} else {
				author = authorInfo{UID: user.UID, Name: user.DisplayName, Title: user.UserTitle, AvatarURL: user.AvatarURL}
				authorCache[m.UserID] = author
			}
		}
		resp = append(resp, momentResponse{
			ID:         m.ID,
			FromUID:    author.UID,
			FromName:   author.Name,
			FromTitle:  author.Title,
			FromAvatar: author.AvatarURL,
			Body:       m.Body,
			ImageURL:   m.ImageURL,
			CreatedAt:  m.Created.Unix(),
			Likes:      likeCounts[m.ID],
			Comments:   commentCounts[m.ID],
			Liked:      likedMap[m.ID],
		})
	}

	writeJSON(w, http.StatusOK, momentFeedResponse{Moments: resp})
}

func (a *API) handleMomentUserFeed(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	uid := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("uid")))
	if uid == "" || !isValidUID(uid) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	before := parseBefore(r.URL.Query().Get("before"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByUID(ctx, uid)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	moments, err := a.moments.ListByUsers(ctx, []string{user.ID}, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	momentIDs := make([]string, 0, len(moments))
	for _, m := range moments {
		momentIDs = append(momentIDs, m.ID)
	}
	likeCounts, _ := a.moments.CountLikes(ctx, momentIDs)
	commentCounts, _ := a.moments.CountComments(ctx, momentIDs)
	likedMap, _ := a.moments.LikedBy(ctx, momentIDs, claims.Subject)

	resp := make([]momentResponse, 0, len(moments))
	for _, m := range moments {
		resp = append(resp, momentResponse{
			ID:         m.ID,
			FromUID:    user.UID,
			FromName:   user.DisplayName,
			FromTitle:  user.UserTitle,
			FromAvatar: user.AvatarURL,
			Body:       m.Body,
			ImageURL:   m.ImageURL,
			CreatedAt:  m.Created.Unix(),
			Likes:      likeCounts[m.ID],
			Comments:   commentCounts[m.ID],
			Liked:      likedMap[m.ID],
		})
	}

	writeJSON(w, http.StatusOK, momentFeedResponse{Moments: resp})
}

func (a *API) handleMomentLike(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req momentLikeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	momentID := strings.TrimSpace(req.MomentID)
	if momentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_moment", "invalid moment")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.moments.Like(ctx, momentID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	likeCounts, err := a.moments.CountLikes(ctx, []string{momentID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, momentLikeResponse{
		MomentID: momentID,
		Liked:    true,
		Likes:    likeCounts[momentID],
	})
}

func (a *API) handleMomentUnlike(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req momentLikeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	momentID := strings.TrimSpace(req.MomentID)
	if momentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_moment", "invalid moment")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.moments.Unlike(ctx, momentID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	likeCounts, err := a.moments.CountLikes(ctx, []string{momentID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, momentLikeResponse{
		MomentID: momentID,
		Liked:    false,
		Likes:    likeCounts[momentID],
	})
}

func (a *API) handleMomentDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req momentDeleteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	momentID := strings.TrimSpace(req.MomentID)
	if momentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_moment", "invalid moment")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	moment, err := a.moments.GetByID(ctx, momentID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "moment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if moment.UserID != claims.Subject {
		writeError(w, http.StatusForbidden, "forbidden", "cannot delete others moment")
		return
	}
	if err := a.moments.Delete(ctx, momentID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleMomentComment(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req momentCommentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	momentID := strings.TrimSpace(req.MomentID)
	body := strings.TrimSpace(req.Body)
	if momentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_moment", "invalid moment")
		return
	}
	if !isValidCommentBody(body) {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	id := nanoid.New()
	comment := &data.MomentComment{
		ID:       id,
		MomentID: momentID,
		UserID:   claims.Subject,
		Body:     body,
	}
	if err := a.moments.AddComment(ctx, comment); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, momentCommentResponse{
		ID:         id,
		MomentID:   momentID,
		FromUID:    user.UID,
		FromName:   user.DisplayName,
		FromTitle:  user.UserTitle,
		FromAvatar: user.AvatarURL,
		Body:       body,
		CreatedAt:  time.Now().Unix(),
	})
}

func (a *API) handleMomentComments(w http.ResponseWriter, r *http.Request) {
	_, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	momentID := strings.TrimSpace(r.URL.Query().Get("moment_id"))
	if momentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_moment", "invalid moment")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	before := parseBefore(r.URL.Query().Get("before"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	comments, err := a.moments.ListComments(ctx, momentID, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := make([]momentCommentResponse, 0, len(comments))
	for _, c := range comments {
		resp = append(resp, momentCommentResponse{
			ID:         c.ID,
			MomentID:   c.MomentID,
			FromUID:    c.FromUID,
			FromName:   c.FromName,
			FromTitle:  c.FromTitle,
			FromAvatar: c.AvatarURL,
			Body:       c.Body,
			CreatedAt:  c.Created.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, momentCommentListResponse{Comments: resp})
}

func (a *API) handleMomentCommentDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req momentCommentDeleteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	commentID := strings.TrimSpace(req.CommentID)
	if commentID == "" {
		writeError(w, http.StatusBadRequest, "invalid_comment", "invalid comment")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	comment, err := a.moments.GetCommentByID(ctx, commentID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "comment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	moment, err := a.moments.GetByID(ctx, comment.MomentID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "moment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if comment.UserID != claims.Subject && moment.UserID != claims.Subject {
		writeError(w, http.StatusForbidden, "forbidden", "cannot delete others comment")
		return
	}

	if err := a.moments.DeleteComment(ctx, commentID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "comment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func isValidMomentBody(body string) bool {
	if len(body) < 1 || len(body) > 1000 {
		return false
	}
	return true
}

const maxMomentImageCount = 9
const maxMomentImagePayloadLen = 4096

func normalizeMomentImageInput(raw string, arr []string) (string, bool) {
	urls, ok := collectMomentImageURLs(raw, arr)
	if !ok {
		return "", false
	}
	if len(urls) == 0 {
		return "", true
	}
	if len(urls) == 1 {
		return urls[0], true
	}
	encoded, err := json.Marshal(urls)
	if err != nil || len(encoded) > maxMomentImagePayloadLen {
		return "", false
	}
	return string(encoded), true
}

func collectMomentImageURLs(raw string, arr []string) ([]string, bool) {
	urls := make([]string, 0, maxMomentImageCount)
	seen := make(map[string]struct{})
	appendURL := func(input string) bool {
		trimmed := strings.TrimSpace(input)
		if trimmed == "" {
			return true
		}
		if !isValidImageURL(trimmed) {
			return false
		}
		if _, ok := seen[trimmed]; ok {
			return true
		}
		seen[trimmed] = struct{}{}
		urls = append(urls, trimmed)
		if len(urls) > maxMomentImageCount {
			return false
		}
		return true
	}

	if len(arr) > 0 {
		for _, item := range arr {
			if !appendURL(item) {
				return nil, false
			}
		}
		return urls, true
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return urls, true
	}

	if strings.HasPrefix(raw, "[") {
		var parsed []string
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, false
		}
		for _, item := range parsed {
			if !appendURL(item) {
				return nil, false
			}
		}
		return urls, true
	}

	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		for _, part := range parts {
			if !appendURL(part) {
				return nil, false
			}
		}
		return urls, true
	}

	if !appendURL(raw) {
		return nil, false
	}
	return urls, true
}

func isValidImageURL(url string) bool {
	if url == "" {
		return true
	}
	if len(url) > 1024 {
		return false
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return true
	}
	if strings.HasPrefix(url, "/v1/uploads/") || strings.HasPrefix(url, "/uploads/") {
		return true
	}
	return false
}

func isValidCommentBody(body string) bool {
	if len(body) < 1 || len(body) > 300 {
		return false
	}
	return true
}
