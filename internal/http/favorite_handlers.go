package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"metrochat/internal/data"
)

type favoriteUpsertRequest struct {
	Type     string `json:"type"`
	TargetID string `json:"target_id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	MediaURL string `json:"media_url"`
	Extra    string `json:"extra"`
}

type favoriteRemoveRequest struct {
	Type     string `json:"type"`
	TargetID string `json:"target_id"`
}

type favoriteItemResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	TargetID  string `json:"target_id"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	MediaURL  string `json:"media_url"`
	Extra     string `json:"extra"`
	CreatedAt int64  `json:"created_at"`
}

type favoriteListResponse struct {
	Items   []favoriteItemResponse `json:"items"`
	Total   int                    `json:"total"`
	HasMore bool                   `json:"has_more"`
	Limit   int                    `json:"limit"`
	Offset  int                    `json:"offset"`
}

func (a *API) handleFavoriteAdd(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req favoriteUpsertRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	favType := normalizeFavoriteType(req.Type)
	targetID := strings.TrimSpace(req.TargetID)
	if favType == "" {
		writeError(w, http.StatusBadRequest, "invalid_type", "invalid type")
		return
	}
	if targetID == "" || len(targetID) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_target", "invalid target")
		return
	}
	title := strings.TrimSpace(req.Title)
	subtitle := strings.TrimSpace(req.Subtitle)
	mediaURL := strings.TrimSpace(req.MediaURL)
	extra := strings.TrimSpace(req.Extra)
	if len(title) > 200 || len(subtitle) > 300 || len(mediaURL) > 1024 || len(extra) > 4000 {
		writeError(w, http.StatusBadRequest, "invalid_payload", "invalid payload")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	err := a.favorites.Upsert(ctx, &data.FavoriteItem{
		ID:        data.NewID(),
		UserID:    claims.Subject,
		FavType:   favType,
		TargetID:  targetID,
		Title:     title,
		Subtitle:  subtitle,
		MediaURL:  mediaURL,
		ExtraJSON: extra,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleFavoriteRemove(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req favoriteRemoveRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	favType := normalizeFavoriteType(req.Type)
	targetID := strings.TrimSpace(req.TargetID)
	if favType == "" {
		writeError(w, http.StatusBadRequest, "invalid_type", "invalid type")
		return
	}
	if targetID == "" || len(targetID) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_target", "invalid target")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.favorites.Delete(ctx, claims.Subject, favType, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleFavoriteList(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	favType := normalizeFavoriteTypeAllowEmpty(strings.TrimSpace(r.URL.Query().Get("type")))
	if favType == "_invalid" {
		writeError(w, http.StatusBadRequest, "invalid_type", "invalid type")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := a.favorites.List(ctx, claims.Subject, favType, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	total, err := a.favorites.Count(ctx, claims.Subject, favType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	resp := make([]favoriteItemResponse, 0, len(rows))
	for _, it := range rows {
		resp = append(resp, favoriteItemResponse{
			ID:        it.ID,
			Type:      it.FavType,
			TargetID:  it.TargetID,
			Title:     it.Title,
			Subtitle:  it.Subtitle,
			MediaURL:  it.MediaURL,
			Extra:     it.ExtraJSON,
			CreatedAt: it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, favoriteListResponse{
		Items:   resp,
		Total:   total,
		HasMore: offset+len(resp) < total,
		Limit:   limit,
		Offset:  offset,
	})
}

func normalizeFavoriteTypeAllowEmpty(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	v := normalizeFavoriteType(raw)
	if v == "" {
		return "_invalid"
	}
	return v
}

func normalizeFavoriteType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "chat_image", "image":
		return "chat_image"
	case "chat_voice", "voice":
		return "chat_voice"
	case "chat_video", "video":
		return "chat_video"
	case "resource_file", "resource":
		return "resource_file"
	case "emoji_pack", "emoji":
		return "emoji_pack"
	case "music_song", "music":
		return "music_song"
	default:
		return ""
	}
}
