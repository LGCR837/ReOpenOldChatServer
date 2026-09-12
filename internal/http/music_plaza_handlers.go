package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"metrochat/internal/data"
)

const (
	musicPlazaMaxNameLen   = 64
	musicPlazaMaxSongBytes = 20 << 20
	musicPlazaMaxCoverMB   = 1 << 20
	musicPlazaMaxLrcBytes  = 1 << 20
	musicPlazaUploadBody   = 26 << 20
	musicPlazaUploadForm   = 24 << 20
)

var errMusicPlazaSongTooLarge = errors.New("song too large")
var errMusicPlazaSongInvalid = errors.New("invalid song")
var errMusicPlazaCoverTooLarge = errors.New("cover too large")
var errMusicPlazaCoverInvalid = errors.New("invalid cover")
var errMusicPlazaLyricsTooLarge = errors.New("lyrics too large")
var errMusicPlazaLyricsInvalid = errors.New("invalid lyrics")

type musicPlazaItemResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SongURL     string `json:"song_url"`
	CoverURL    string `json:"cover_url,omitempty"`
	LyricsURL   string `json:"lyrics_url,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	DurationMS  int    `json:"duration_ms"`
	PlayCount   int    `json:"play_count"`
	CreatedAt   int64  `json:"created_at"`
	OwnerUID    string `json:"owner_uid"`
	OwnerName   string `json:"owner_name"`
	OwnerTitle  string `json:"owner_title,omitempty"`
	OwnerAvatar string `json:"owner_avatar,omitempty"`
	Likes       int    `json:"likes"`
	Comments    int    `json:"comments"`
	Liked       bool   `json:"liked"`
	CanDelete   bool   `json:"can_delete"`
}

type musicPlazaListResponse struct {
	Items   []musicPlazaItemResponse `json:"items"`
	Total   int                      `json:"total"`
	HasMore bool                     `json:"has_more"`
	Limit   int                      `json:"limit"`
	Offset  int                      `json:"offset"`
}

type musicLyricsUploadResponse struct {
	LyricsURL string `json:"lyrics_url"`
}

type musicPlazaDeleteRequest struct {
	ItemID string `json:"item_id"`
}

type musicPlazaBatchDeleteRequest struct {
	ItemIDs []string `json:"item_ids"`
}

type musicPlazaBatchDeleteResponse struct {
	Deleted int `json:"deleted"`
}

type musicPlazaLikeRequest struct {
	ItemID string `json:"item_id"`
}

type musicPlazaLikeResponse struct {
	ItemID string `json:"item_id"`
	Liked  bool   `json:"liked"`
	Likes  int    `json:"likes"`
}

type musicPlazaPlayRequest struct {
	ItemID string `json:"item_id"`
}

type musicPlazaPlayResponse struct {
	ItemID    string `json:"item_id"`
	PlayCount int    `json:"play_count"`
}

type musicPlazaCommentRequest struct {
	ItemID string `json:"item_id"`
	Body   string `json:"body"`
}

type musicPlazaCommentDeleteRequest struct {
	CommentID string `json:"comment_id"`
}

type musicPlazaCommentResponse struct {
	ID         string `json:"id"`
	ItemID     string `json:"item_id"`
	FromUID    string `json:"from_uid"`
	FromName   string `json:"from_name"`
	FromTitle  string `json:"from_title,omitempty"`
	FromAvatar string `json:"from_avatar,omitempty"`
	Body       string `json:"body"`
	CreatedAt  int64  `json:"created_at"`
}

type musicPlazaCommentListResponse struct {
	Comments []musicPlazaCommentResponse `json:"comments"`
}

func (a *API) handleMusicPlazaList(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))
	mineRaw := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("mine")))
	mineOnly := mineRaw == "1" || mineRaw == "true" || mineRaw == "yes"

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var items []data.MusicPlazaItem
	var total int
	var err error
	if mineOnly {
		items, err = a.musicPlaza.ListByOwner(ctx, claims.Subject, query, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		total, err = a.musicPlaza.CountByOwner(ctx, claims.Subject, query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
	} else {
		items, err = a.musicPlaza.List(ctx, query, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		total, err = a.musicPlaza.Count(ctx, query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
	}

	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			itemIDs = append(itemIDs, item.ID)
		}
	}
	likeCounts, err := a.musicPlaza.CountLikes(ctx, itemIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	commentCounts, err := a.musicPlaza.CountComments(ctx, itemIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	likedBy, err := a.musicPlaza.LikedBy(ctx, itemIDs, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := make([]musicPlazaItemResponse, 0, len(items))
	for _, item := range items {
		coverURL := a.musicCoverProxyURL(item.CoverURL)
		resp = append(resp, musicPlazaItemResponse{
			ID:          item.ID,
			Name:        item.Name,
			SongURL:     item.SongURL,
			CoverURL:    coverURL,
			LyricsURL:   item.LyricsURL,
			SizeBytes:   item.SizeBytes,
			DurationMS:  item.DurationMS,
			PlayCount:   item.PlayCount,
			CreatedAt:   item.CreatedAt.Unix(),
			OwnerUID:    item.OwnerUID,
			OwnerName:   item.OwnerName,
			OwnerTitle:  item.OwnerTitle,
			OwnerAvatar: item.OwnerAvatar,
			Likes:       likeCounts[item.ID],
			Comments:    commentCounts[item.ID],
			Liked:       likedBy[item.ID],
			CanDelete:   item.OwnerID == claims.Subject,
		})
	}

	writeJSON(w, http.StatusOK, musicPlazaListResponse{
		Items:   resp,
		Total:   total,
		HasMore: offset+len(resp) < total,
		Limit:   limit,
		Offset:  offset,
	})
}

func (a *API) handleMusicPlazaMineList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	query.Set("mine", "1")
	r.URL.RawQuery = query.Encode()
	a.handleMusicPlazaList(w, r)
}

func (a *API) handleMusicPlazaPlay(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req musicPlazaPlayRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	itemID := strings.TrimSpace(req.ItemID)
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	count, err := a.musicPlaza.IncreasePlayCount(ctx, itemID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, musicPlazaPlayResponse{ItemID: itemID, PlayCount: count})
}

func (a *API) handleMusicPlazaRanking(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	items, err := a.musicPlaza.ListByPlayCount(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" {
			itemIDs = append(itemIDs, item.ID)
		}
	}
	likeCounts, err := a.musicPlaza.CountLikes(ctx, itemIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	commentCounts, err := a.musicPlaza.CountComments(ctx, itemIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	likedBy, err := a.musicPlaza.LikedBy(ctx, itemIDs, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := make([]musicPlazaItemResponse, 0, len(items))
	for _, item := range items {
		coverURL := a.musicCoverProxyURL(item.CoverURL)
		resp = append(resp, musicPlazaItemResponse{
			ID:          item.ID,
			Name:        item.Name,
			SongURL:     item.SongURL,
			CoverURL:    coverURL,
			LyricsURL:   item.LyricsURL,
			SizeBytes:   item.SizeBytes,
			DurationMS:  item.DurationMS,
			PlayCount:   item.PlayCount,
			CreatedAt:   item.CreatedAt.Unix(),
			OwnerUID:    item.OwnerUID,
			OwnerName:   item.OwnerName,
			OwnerTitle:  item.OwnerTitle,
			OwnerAvatar: item.OwnerAvatar,
			Likes:       likeCounts[item.ID],
			Comments:    commentCounts[item.ID],
			Liked:       likedBy[item.ID],
			CanDelete:   item.OwnerID == claims.Subject,
		})
	}

	writeJSON(w, http.StatusOK, musicPlazaListResponse{
		Items:   resp,
		Total:   len(resp),
		HasMore: false,
		Limit:   limit,
		Offset:  0,
	})
}

func (a *API) handleMusicPlazaUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, musicPlazaUploadBody)
	limitUploadBody(r)
	if err := r.ParseMultipartForm(musicPlazaUploadForm); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if !isValidMusicPlazaName(name) {
		writeError(w, http.StatusBadRequest, "invalid_name", "invalid name")
		return
	}

	songURL, sizeBytes, songErr := saveMusicPlazaSongUpload(a, r)
	if songErr != nil {
		switch {
		case errors.Is(songErr, errMusicPlazaSongTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "song_too_large", "song too large")
		case errors.Is(songErr, errMusicPlazaSongInvalid):
			writeError(w, http.StatusBadRequest, "invalid_song", "invalid song")
		default:
			writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		}
		return
	}

	coverURL, coverErr := saveMusicPlazaCoverUpload(a, r)
	if coverErr != nil {
		switch {
		case errors.Is(coverErr, errMusicPlazaCoverTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "cover_too_large", "cover too large")
		case errors.Is(coverErr, errMusicPlazaCoverInvalid):
			writeError(w, http.StatusBadRequest, "invalid_cover", "invalid cover")
		default:
			writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		}
		return
	}

	lyricsURL, lyricsErr := saveMusicPlazaLyricsUpload(a, r)
	if lyricsErr != nil {
		switch {
		case errors.Is(lyricsErr, errMusicPlazaLyricsTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "lyrics_too_large", "lyrics too large")
		case errors.Is(lyricsErr, errMusicPlazaLyricsInvalid):
			writeError(w, http.StatusBadRequest, "invalid_lyrics", "invalid lyrics")
		default:
			writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		}
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	exists, err := a.musicPlaza.ExistsByOwnerAndSongURL(ctx, claims.Subject, songURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "duplicate_song", "duplicate song")
		return
	}

	item := &data.MusicPlazaItem{
		ID:         data.NewID(),
		OwnerID:    claims.Subject,
		Name:       name,
		SongURL:    songURL,
		CoverURL:   coverURL,
		LyricsURL:  lyricsURL,
		SizeBytes:  sizeBytes,
		DurationMS: 0,
	}
	if err := a.musicPlaza.Create(ctx, item); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	ownerName := claims.UID
	ownerTitle := ""
	ownerAvatar := ""
	user, _ := a.users.GetByID(ctx, claims.Subject)
	if user != nil {
		if strings.TrimSpace(user.DisplayName) != "" {
			ownerName = strings.TrimSpace(user.DisplayName)
		}
		ownerTitle = strings.TrimSpace(user.UserTitle)
		ownerAvatar = strings.TrimSpace(user.AvatarURL)
	}

	writeJSON(w, http.StatusCreated, musicPlazaItemResponse{
		ID:          item.ID,
		Name:        item.Name,
		SongURL:     item.SongURL,
		CoverURL:    a.musicCoverProxyURL(item.CoverURL),
		LyricsURL:   item.LyricsURL,
		SizeBytes:   item.SizeBytes,
		DurationMS:  item.DurationMS,
		PlayCount:   0,
		CreatedAt:   time.Now().Unix(),
		OwnerUID:    claims.UID,
		OwnerName:   ownerName,
		OwnerTitle:  ownerTitle,
		OwnerAvatar: ownerAvatar,
		Likes:       0,
		Comments:    0,
		Liked:       false,
		CanDelete:   true,
	})
}

func (a *API) handleMusicPlazaLyricsUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	limitUploadBody(r)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}

	itemID := strings.TrimSpace(r.URL.Query().Get("item_id"))
	if itemID == "" {
		itemID = strings.TrimSpace(r.FormValue("item_id"))
	}
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}

	lyricsURL, err := saveMusicPlazaLyricsUpload(a, r)
	if err != nil {
		switch {
		case errors.Is(err, errMusicPlazaLyricsTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "lyrics_too_large", "lyrics too large")
		case errors.Is(err, errMusicPlazaLyricsInvalid):
			writeError(w, http.StatusBadRequest, "invalid_lyrics", "invalid lyrics")
		default:
			writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		}
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	item, getErr := a.musicPlaza.GetByID(ctx, itemID)
	if getErr != nil {
		if getErr == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if item.OwnerID == "" || item.OwnerID != claims.Subject {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}

	if err := a.musicPlaza.UpdateLyricsURL(ctx, itemID, lyricsURL); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, musicLyricsUploadResponse{LyricsURL: lyricsURL})
}

func (a *API) handleMusicPlazaDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req musicPlazaDeleteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	itemID := strings.TrimSpace(req.ItemID)
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	item, err := a.musicPlaza.GetByID(ctx, itemID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if item.OwnerID != claims.Subject {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}
	if err := a.musicPlaza.DeleteByOwner(ctx, itemID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if path := musicUploadedFilePath(a.cfg.UploadDir, item.SongURL); path != "" {
		_ = os.Remove(path)
	}
	if path := musicUploadedFilePath(a.cfg.UploadDir, item.CoverURL); path != "" {
		_ = os.Remove(path)
	}
	if path := musicUploadedFilePath(a.cfg.UploadDir, item.LyricsURL); path != "" {
		_ = os.Remove(path)
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleMusicPlazaMineBatchDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req musicPlazaBatchDeleteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	seen := make(map[string]struct{})
	itemIDs := make([]string, 0, len(req.ItemIDs))
	for _, raw := range req.ItemIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		itemIDs = append(itemIDs, id)
		if len(itemIDs) >= 200 {
			break
		}
	}
	if len(itemIDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	ownedIDs := make([]string, 0, len(itemIDs))
	removePaths := make([]string, 0, len(itemIDs)*3)
	for _, id := range itemIDs {
		item, err := a.musicPlaza.GetByID(ctx, id)
		if err != nil {
			if err == data.ErrNotFound {
				continue
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		if item.OwnerID != claims.Subject {
			continue
		}
		ownedIDs = append(ownedIDs, id)
		if path := musicUploadedFilePath(a.cfg.UploadDir, item.SongURL); path != "" {
			removePaths = append(removePaths, path)
		}
		if path := musicUploadedFilePath(a.cfg.UploadDir, item.CoverURL); path != "" {
			removePaths = append(removePaths, path)
		}
		if path := musicUploadedFilePath(a.cfg.UploadDir, item.LyricsURL); path != "" {
			removePaths = append(removePaths, path)
		}
	}
	if len(ownedIDs) == 0 {
		writeJSON(w, http.StatusOK, musicPlazaBatchDeleteResponse{Deleted: 0})
		return
	}
	deleted, err := a.musicPlaza.DeleteBatchByOwner(ctx, ownedIDs, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	for _, path := range removePaths {
		_ = os.Remove(path)
	}
	writeJSON(w, http.StatusOK, musicPlazaBatchDeleteResponse{Deleted: int(deleted)})
}

func (a *API) handleMusicPlazaLike(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req musicPlazaLikeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	itemID := strings.TrimSpace(req.ItemID)
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := a.musicPlaza.GetByID(ctx, itemID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if err := a.musicPlaza.Like(ctx, itemID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	counts, err := a.musicPlaza.CountLikes(ctx, []string{itemID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, musicPlazaLikeResponse{ItemID: itemID, Liked: true, Likes: counts[itemID]})
}

func (a *API) handleMusicPlazaUnlike(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req musicPlazaLikeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	itemID := strings.TrimSpace(req.ItemID)
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.musicPlaza.Unlike(ctx, itemID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	counts, err := a.musicPlaza.CountLikes(ctx, []string{itemID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, musicPlazaLikeResponse{ItemID: itemID, Liked: false, Likes: counts[itemID]})
}

func (a *API) handleMusicPlazaComment(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req musicPlazaCommentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	itemID := strings.TrimSpace(req.ItemID)
	body := strings.TrimSpace(req.Body)
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}
	if !isValidMusicCommentBody(body) {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := a.musicPlaza.GetByID(ctx, itemID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	commentID := data.NewID()
	comment := &data.MusicPlazaComment{
		ID:     commentID,
		ItemID: itemID,
		UserID: claims.Subject,
		Body:   body,
	}
	if err := a.musicPlaza.AddComment(ctx, comment); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, musicPlazaCommentResponse{
		ID:         commentID,
		ItemID:     itemID,
		FromUID:    user.UID,
		FromName:   user.DisplayName,
		FromTitle:  user.UserTitle,
		FromAvatar: user.AvatarURL,
		Body:       body,
		CreatedAt:  time.Now().Unix(),
	})
}

func (a *API) handleMusicPlazaComments(w http.ResponseWriter, r *http.Request) {
	_, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	itemID := strings.TrimSpace(r.URL.Query().Get("item_id"))
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	before := parseBefore(r.URL.Query().Get("before"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	comments, err := a.musicPlaza.ListComments(ctx, itemID, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	resp := make([]musicPlazaCommentResponse, 0, len(comments))
	for _, c := range comments {
		resp = append(resp, musicPlazaCommentResponse{
			ID:         c.ID,
			ItemID:     c.ItemID,
			FromUID:    c.FromUID,
			FromName:   c.FromName,
			FromTitle:  c.FromTitle,
			FromAvatar: c.AvatarURL,
			Body:       c.Body,
			CreatedAt:  c.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, musicPlazaCommentListResponse{Comments: resp})
}

func (a *API) handleMusicPlazaCommentDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req musicPlazaCommentDeleteRequest
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
	comment, err := a.musicPlaza.GetCommentByID(ctx, commentID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "comment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	item, err := a.musicPlaza.GetByID(ctx, comment.ItemID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if comment.UserID != claims.Subject && item.OwnerID != claims.Subject {
		writeError(w, http.StatusForbidden, "forbidden", "cannot delete")
		return
	}
	if err := a.musicPlaza.DeleteComment(ctx, commentID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "comment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func isValidMusicPlazaName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if utf8Len(name) > musicPlazaMaxNameLen {
		return false
	}
	return true
}

func isValidMusicCommentBody(body string) bool {
	if len(body) < 1 || len(body) > 300 {
		return false
	}
	return true
}

func saveMusicPlazaSongUpload(a *API, r *http.Request) (string, int64, error) {
	if a == nil || r == nil {
		return "", 0, errMusicPlazaSongInvalid
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", 0, errMusicPlazaSongInvalid
	}
	defer file.Close()
	if header == nil {
		return "", 0, errMusicPlazaSongInvalid
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	ext, extErr := musicAudioExtFromType(contentType, header.Filename)
	if extErr != nil {
		return "", 0, errMusicPlazaSongInvalid
	}
	if header.Size > int64(musicPlazaMaxSongBytes) {
		return "", 0, errMusicPlazaSongTooLarge
	}

	dir := filepath.Join(a.cfg.UploadDir, "music")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	filename := "msc-" + data.NewID() + ext
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		return "", 0, err
	}
	written, cpErr := io.Copy(dst, io.LimitReader(file, int64(musicPlazaMaxSongBytes)+1))
	_ = dst.Close()
	if cpErr != nil {
		_ = os.Remove(path)
		return "", 0, cpErr
	}
	if written <= 0 {
		_ = os.Remove(path)
		return "", 0, errMusicPlazaSongInvalid
	}
	if written > int64(musicPlazaMaxSongBytes) {
		_ = os.Remove(path)
		return "", 0, errMusicPlazaSongTooLarge
	}
	uploadURL := "/v1/uploads/music/" + filename
	go a.maybePushUploadToDataServer(uploadURL)
	return uploadURL, written, nil
}

func saveMusicPlazaCoverUpload(a *API, r *http.Request) (string, error) {
	if a == nil || r == nil {
		return "", errMusicPlazaCoverInvalid
	}
	file, header, err := r.FormFile("cover")
	if err != nil {
		file, header, err = r.FormFile("thumb")
		if err != nil {
			return "", nil
		}
	}
	defer file.Close()
	if header == nil {
		return "", errMusicPlazaCoverInvalid
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	ext, extErr := avatarExtFromType(contentType)
	if extErr != nil {
		return "", errMusicPlazaCoverInvalid
	}
	if header.Size > int64(musicPlazaMaxCoverMB) {
		return "", errMusicPlazaCoverTooLarge
	}

	dir := filepath.Join(a.cfg.UploadDir, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	filename := "music-cover-" + data.NewID() + ext
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		return "", err
	}
	written, cpErr := io.Copy(dst, io.LimitReader(file, int64(musicPlazaMaxCoverMB)+1))
	_ = dst.Close()
	if cpErr != nil {
		_ = os.Remove(path)
		return "", cpErr
	}
	if written <= 0 {
		_ = os.Remove(path)
		return "", errMusicPlazaCoverInvalid
	}
	if written > int64(musicPlazaMaxCoverMB) {
		_ = os.Remove(path)
		return "", errMusicPlazaCoverTooLarge
	}
	uploadURL := "/v1/uploads/media/" + filename
	go a.maybePushUploadToDataServer(uploadURL)
	return uploadURL, nil
}

func saveMusicPlazaLyricsUpload(a *API, r *http.Request) (string, error) {
	if a == nil || r == nil {
		return "", errMusicPlazaLyricsInvalid
	}
	file, header, err := r.FormFile("lyrics")
	if err != nil {
		return "", nil
	}
	defer file.Close()
	if header == nil {
		return "", errMusicPlazaLyricsInvalid
	}

	filenameLower := strings.ToLower(strings.TrimSpace(header.Filename))
	if filenameLower == "" || !strings.HasSuffix(filenameLower, ".lrc") {
		return "", errMusicPlazaLyricsInvalid
	}
	if header.Size > int64(musicPlazaMaxLrcBytes) {
		return "", errMusicPlazaLyricsTooLarge
	}

	dir := filepath.Join(a.cfg.UploadDir, "lyrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	filename := "lyr-" + data.NewID() + ".lrc"
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		return "", err
	}
	written, cpErr := io.Copy(dst, io.LimitReader(file, int64(musicPlazaMaxLrcBytes)+1))
	_ = dst.Close()
	if cpErr != nil {
		_ = os.Remove(path)
		return "", cpErr
	}
	if written <= 0 {
		_ = os.Remove(path)
		return "", errMusicPlazaLyricsInvalid
	}
	if written > int64(musicPlazaMaxLrcBytes) {
		_ = os.Remove(path)
		return "", errMusicPlazaLyricsTooLarge
	}
	uploadURL := "/v1/uploads/lyrics/" + filename
	go a.maybePushUploadToDataServer(uploadURL)
	return uploadURL, nil
}

func musicAudioExtFromType(ct, filename string) (string, error) {
	ct = strings.ToLower(strings.TrimSpace(ct))
	switch ct {
	case "audio/mpeg", "audio/mp3":
		return ".mp3", nil
	case "audio/mp4", "audio/x-m4a":
		return ".m4a", nil
	case "audio/aac":
		return ".aac", nil
	case "audio/3gpp", "audio/amr", "audio/amr-wb":
		return ".amr", nil
	case "audio/flac", "audio/x-flac":
		return ".flac", nil
	case "audio/ogg", "audio/x-ogg", "application/ogg", "audio/vorbis":
		return ".ogg", nil
	case "audio/wav", "audio/x-wav", "audio/wave", "audio/vnd.wave":
		return ".wav", nil
	}
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(filename)))
	switch ext {
	case ".mp3":
		return ".mp3", nil
	case ".m4a", ".mp4":
		return ".m4a", nil
	case ".aac":
		return ".aac", nil
	case ".amr", ".3gp", ".3gpp":
		return ".amr", nil
	case ".flac":
		return ".flac", nil
	case ".ogg", ".oga":
		return ".ogg", nil
	case ".wav", ".wave":
		return ".wav", nil
	default:
		return "", errMusicPlazaSongInvalid
	}
}

type musicLyricsCapture struct {
	LyricsURL string `json:"lyrics_url"`
}

func parseMusicLyricsUploadResponse(body []byte) (*musicLyricsCapture, error) {
	var out musicLyricsCapture
	if len(body) == 0 {
		return &out, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func musicUploadedFilePath(uploadDir, url string) string {
	if url == "" {
		return ""
	}
	baseURL := strings.SplitN(url, "?", 2)[0]
	name := ""
	subdir := ""
	switch {
	case strings.HasPrefix(baseURL, "/v1/uploads/music/"):
		name = strings.TrimPrefix(baseURL, "/v1/uploads/music/")
		subdir = "music"
	case strings.HasPrefix(baseURL, "/uploads/music/"):
		name = strings.TrimPrefix(baseURL, "/uploads/music/")
		subdir = "music"
	case strings.HasPrefix(baseURL, "/v1/uploads/media/"):
		name = strings.TrimPrefix(baseURL, "/v1/uploads/media/")
		subdir = "media"
	case strings.HasPrefix(baseURL, "/uploads/media/"):
		name = strings.TrimPrefix(baseURL, "/uploads/media/")
		subdir = "media"
	case strings.HasPrefix(baseURL, "/v1/uploads/lyrics/"):
		name = strings.TrimPrefix(baseURL, "/v1/uploads/lyrics/")
		subdir = "lyrics"
	case strings.HasPrefix(baseURL, "/uploads/lyrics/"):
		name = strings.TrimPrefix(baseURL, "/uploads/lyrics/")
		subdir = "lyrics"
	default:
		return ""
	}
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return filepath.Join(uploadDir, subdir, name)
}
