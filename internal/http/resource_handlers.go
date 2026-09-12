package httpapi

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/data"
)

const (
	maxResourceSectionsPerUser = 5
	maxResourceUploadBytes     = 100 << 20
)

type resourceQuotaResponse struct {
	LimitBytes     int64 `json:"limit_bytes"`
	UsedBytes      int64 `json:"used_bytes"`
	RemainingBytes int64 `json:"remaining_bytes"`
}

type resourceSectionCreateRequest struct {
	Name string `json:"name"`
}

type resourceSectionDeleteRequest struct {
	SectionID string `json:"section_id"`
}

type resourceSectionResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OwnerUID      string `json:"owner_uid"`
	OwnerName     string `json:"owner_name,omitempty"`
	OwnerTitle    string `json:"owner_title,omitempty"`
	OwnerAvatar   string `json:"owner_avatar,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	ResourceCount int    `json:"resource_count"`
	IsOwner       bool   `json:"is_owner"`
}

type resourceSectionListResponse struct {
	Sections []resourceSectionResponse `json:"sections"`
}

type resourceItemDeleteRequest struct {
	ItemID string `json:"item_id"`
}

type resourceItemResponse struct {
	ID             string `json:"id"`
	SectionID      string `json:"section_id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	SizeBytes      int64  `json:"size_bytes"`
	UploaderUID    string `json:"uploader_uid"`
	UploaderName   string `json:"uploader_name,omitempty"`
	UploaderTitle  string `json:"uploader_title,omitempty"`
	UploaderAvatar string `json:"uploader_avatar,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	Likes          int    `json:"likes"`
	Comments       int    `json:"comments"`
	Liked          bool   `json:"liked"`
}

type resourceItemListResponse struct {
	Items []resourceItemResponse `json:"items"`
}

func (a *API) resourceQuotaBytes() int64 {
	if a != nil && a.cfg.ResourceQuota > 0 {
		return a.cfg.ResourceQuota
	}
	return 10 * 1024 * 1024 * 1024
}

type resourceLikeRequest struct {
	ItemID string `json:"item_id"`
}

type resourceLikeResponse struct {
	ItemID string `json:"item_id"`
	Liked  bool   `json:"liked"`
	Likes  int    `json:"likes"`
}

type resourceCommentRequest struct {
	ItemID string `json:"item_id"`
	Body   string `json:"body"`
}

type resourceCommentDeleteRequest struct {
	CommentID string `json:"comment_id"`
}

type resourceReportRequest struct {
	ItemID string `json:"item_id"`
	Reason string `json:"reason"`
}

type resourceCommentResponse struct {
	ID         string `json:"id"`
	ItemID     string `json:"item_id"`
	FromUID    string `json:"from_uid"`
	FromName   string `json:"from_name"`
	FromTitle  string `json:"from_title"`
	FromAvatar string `json:"from_avatar"`
	Body       string `json:"body"`
	CreatedAt  int64  `json:"created_at"`
}

type resourceCommentListResponse struct {
	Comments []resourceCommentResponse `json:"comments"`
}

func (a *API) handleResourceSectionCreate(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req resourceSectionCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	name := strings.TrimSpace(req.Name)
	if !isValidResourceSectionName(name) {
		writeError(w, http.StatusBadRequest, "invalid_name", "invalid name")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	count, err := a.resources.CountSectionsByOwner(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if count >= maxResourceSectionsPerUser {
		writeError(w, http.StatusBadRequest, "limit_reached", "section limit reached")
		return
	}
	section := &data.ResourceSection{
		ID:      nanoid.New(),
		Name:    name,
		OwnerID: claims.Subject,
	}
	if err := a.resources.CreateSection(ctx, section); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			writeError(w, http.StatusConflict, "duplicate_name", "duplicate name")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	user, _ := a.users.GetByID(ctx, claims.Subject)
	resp := resourceSectionResponse{
		ID:            section.ID,
		Name:          name,
		OwnerUID:      claims.UID,
		OwnerName:     "",
		OwnerTitle:    "",
		OwnerAvatar:   "",
		CreatedAt:     time.Now().Unix(),
		ResourceCount: 0,
		IsOwner:       true,
	}
	if user != nil {
		resp.OwnerName = user.DisplayName
		resp.OwnerTitle = user.UserTitle
		resp.OwnerAvatar = user.AvatarURL
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (a *API) handleResourceSectionList(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	sections, err := a.resources.ListSections(ctx, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	resp := make([]resourceSectionResponse, 0, len(sections))
	for _, s := range sections {
		resp = append(resp, resourceSectionResponse{
			ID:            s.ID,
			Name:          s.Name,
			OwnerUID:      s.OwnerUID,
			OwnerName:     s.OwnerName,
			OwnerTitle:    s.OwnerTitle,
			OwnerAvatar:   s.OwnerAvatar,
			CreatedAt:     s.Created.Unix(),
			ResourceCount: s.ResourceCount,
			IsOwner:       s.OwnerID == claims.Subject,
		})
	}
	writeJSON(w, http.StatusOK, resourceSectionListResponse{Sections: resp})
}

func (a *API) handleResourceSectionDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req resourceSectionDeleteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	sectionID := strings.TrimSpace(req.SectionID)
	if sectionID == "" {
		writeError(w, http.StatusBadRequest, "invalid_section", "invalid section")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	section, err := a.resources.GetSectionByID(ctx, sectionID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "section not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if section.OwnerID != claims.Subject {
		writeError(w, http.StatusForbidden, "forbidden", "cannot delete others section")
		return
	}
	if err := a.resources.DeleteSection(ctx, sectionID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleResourceUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxResourceUploadBytes)
	limitUploadBody(r)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}
	sectionID := strings.TrimSpace(r.FormValue("section_id"))
	if sectionID == "" {
		writeError(w, http.StatusBadRequest, "invalid_section", "invalid section")
		return
	}
	ctxCheck, cancelCheck := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancelCheck()
	if _, err := a.resources.GetSectionByID(ctxCheck, sectionID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "section not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "missing file")
		return
	}
	defer file.Close()

	usedBytes, _ := a.resources.SumUploaderSize(ctxCheck, claims.Subject)
	quota := a.resourceQuotaBytes()
	remaining := quota - usedBytes
	if remaining < 0 {
		remaining = 0
	}
	if header != nil && header.Size > 0 && header.Size > remaining {
		writeError(w, http.StatusBadRequest, "quota_exceeded", "storage quota exceeded")
		return
	}

	dir := filepath.Join(a.cfg.UploadDir, "resources")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	ext := sanitizeResourceExt(header.Filename)
	filename := nanoid.New() + ext
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	if written <= 0 || written > maxResourceUploadBytes {
		_ = os.Remove(path)
		writeError(w, http.StatusBadRequest, "invalid_size", "invalid size")
		return
	}
	if usedBytes+written > quota {
		_ = os.Remove(path)
		writeError(w, http.StatusBadRequest, "quota_exceeded", "storage quota exceeded")
		return
	}

	name := sanitizeResourceName(header.Filename)
	if !isValidResourceItemName(name) {
		name = "resource"
	}

	itemID := nanoid.New()
	item := &data.ResourceItem{
		ID:         itemID,
		SectionID:  sectionID,
		UploaderID: claims.Subject,
		Name:       name,
		URL:        "/v1/uploads/resources/" + filename,
		SizeBytes:  written,
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.resources.CreateItem(ctx, item); err != nil {
		_ = os.Remove(path)
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	user, _ := a.users.GetByID(ctx, claims.Subject)
	resp := resourceItemResponse{
		ID:             itemID,
		SectionID:      sectionID,
		Name:           name,
		URL:            item.URL,
		SizeBytes:      written,
		UploaderUID:    claims.UID,
		UploaderName:   "",
		UploaderTitle:  "",
		UploaderAvatar: "",
		CreatedAt:      time.Now().Unix(),
		Likes:          0,
		Comments:       0,
		Liked:          false,
	}
	if user != nil {
		resp.UploaderName = user.DisplayName
		resp.UploaderTitle = user.UserTitle
		resp.UploaderAvatar = user.AvatarURL
	}
	go a.maybePushUploadToDataServer(item.URL)
	writeJSON(w, http.StatusCreated, resp)
}

func (a *API) handleMeResourceQuota(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	used, err := a.resources.SumUploaderSize(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	quota := a.resourceQuotaBytes()
	remaining := quota - used
	if remaining < 0 {
		remaining = 0
	}
	writeJSON(w, http.StatusOK, resourceQuotaResponse{
		LimitBytes:     quota,
		UsedBytes:      used,
		RemainingBytes: remaining,
	})
}

func (a *API) handleResourceItems(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	sectionID := strings.TrimSpace(r.URL.Query().Get("section_id"))
	if sectionID == "" {
		writeError(w, http.StatusBadRequest, "invalid_section", "invalid section")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	items, err := a.resources.ListItemsBySection(ctx, sectionID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		itemIDs = append(itemIDs, item.ID)
	}
	likeCounts, _ := a.resources.CountLikes(ctx, itemIDs)
	commentCounts, _ := a.resources.CountComments(ctx, itemIDs)
	likedMap, _ := a.resources.LikedBy(ctx, itemIDs, claims.Subject)
	resp := make([]resourceItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, resourceItemResponse{
			ID:             item.ID,
			SectionID:      item.SectionID,
			Name:           item.Name,
			URL:            item.URL,
			SizeBytes:      item.SizeBytes,
			UploaderUID:    item.UploaderUID,
			UploaderName:   item.UploaderName,
			UploaderTitle:  item.UploaderTitle,
			UploaderAvatar: item.UploaderAvatar,
			CreatedAt:      item.Created.Unix(),
			Likes:          likeCounts[item.ID],
			Comments:       commentCounts[item.ID],
			Liked:          likedMap[item.ID],
		})
	}
	writeJSON(w, http.StatusOK, resourceItemListResponse{Items: resp})
}

func (a *API) handleResourceSearch(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "invalid_query", "empty query")
		return
	}
	sectionID := strings.TrimSpace(r.URL.Query().Get("section_id"))
	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	items, err := a.resources.SearchItems(ctx, q, sectionID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	itemIDs := make([]string, 0, len(items))
	for _, item := range items {
		itemIDs = append(itemIDs, item.ID)
	}
	likeCounts, _ := a.resources.CountLikes(ctx, itemIDs)
	commentCounts, _ := a.resources.CountComments(ctx, itemIDs)
	likedMap, _ := a.resources.LikedBy(ctx, itemIDs, claims.Subject)
	resp := make([]resourceItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, resourceItemResponse{
			ID:             item.ID,
			SectionID:      item.SectionID,
			Name:           item.Name,
			URL:            item.URL,
			SizeBytes:      item.SizeBytes,
			UploaderUID:    item.UploaderUID,
			UploaderName:   item.UploaderName,
			UploaderTitle:  item.UploaderTitle,
			UploaderAvatar: item.UploaderAvatar,
			CreatedAt:      item.Created.Unix(),
			Likes:          likeCounts[item.ID],
			Comments:       commentCounts[item.ID],
			Liked:          likedMap[item.ID],
		})
	}
	writeJSON(w, http.StatusOK, resourceItemListResponse{Items: resp})
}

func (a *API) handleResourceItemDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req resourceItemDeleteRequest
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
	item, err := a.resources.GetItemByID(ctx, itemID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	section, err := a.resources.GetSectionByID(ctx, item.SectionID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "section not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if item.UploaderID != claims.Subject && section.OwnerID != claims.Subject {
		writeError(w, http.StatusForbidden, "forbidden", "cannot delete resource")
		return
	}
	itemURL := item.URL
	if err := a.resources.DeleteItem(ctx, itemID); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	_ = a.resourceReports.DeleteByItemID(ctx, itemID)
	if path := resourceUploadPath(a.cfg.UploadDir, itemURL); path != "" {
		_ = os.Remove(path)
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleResourceLike(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req resourceLikeRequest
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
	if err := a.resources.Like(ctx, itemID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	likeCounts, err := a.resources.CountLikes(ctx, []string{itemID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, resourceLikeResponse{ItemID: itemID, Liked: true, Likes: likeCounts[itemID]})
}

func (a *API) handleResourceUnlike(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req resourceLikeRequest
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
	if err := a.resources.Unlike(ctx, itemID, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	likeCounts, err := a.resources.CountLikes(ctx, []string{itemID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, resourceLikeResponse{ItemID: itemID, Liked: false, Likes: likeCounts[itemID]})
}

func (a *API) handleResourceComment(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req resourceCommentRequest
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
	if !isValidResourceCommentBody(body) {
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
	comment := &data.ResourceComment{
		ID:     id,
		ItemID: itemID,
		UserID: claims.Subject,
		Body:   body,
	}
	if err := a.resources.AddComment(ctx, comment); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, resourceCommentResponse{
		ID:         id,
		ItemID:     itemID,
		FromUID:    user.UID,
		FromName:   user.DisplayName,
		FromTitle:  user.UserTitle,
		FromAvatar: user.AvatarURL,
		Body:       body,
		CreatedAt:  time.Now().Unix(),
	})
}

func (a *API) handleResourceComments(w http.ResponseWriter, r *http.Request) {
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
	comments, err := a.resources.ListComments(ctx, itemID, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	resp := make([]resourceCommentResponse, 0, len(comments))
	for _, c := range comments {
		resp = append(resp, resourceCommentResponse{
			ID:         c.ID,
			ItemID:     c.ItemID,
			FromUID:    c.FromUID,
			FromName:   c.FromName,
			FromTitle:  c.FromTitle,
			FromAvatar: c.AvatarURL,
			Body:       c.Body,
			CreatedAt:  c.Created.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, resourceCommentListResponse{Comments: resp})
}

func (a *API) handleResourceReport(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req resourceReportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	itemID := strings.TrimSpace(req.ItemID)
	reason := strings.TrimSpace(req.Reason)
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_item", "invalid item")
		return
	}
	if !isValidResourceReportReason(reason) {
		writeError(w, http.StatusBadRequest, "invalid_reason", "invalid reason")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := a.resources.GetItemByID(ctx, itemID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	reporter, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	report := &data.ResourceReport{
		ItemID:      itemID,
		ReporterID:  reporter.ID,
		ReporterUID: reporter.UID,
		Reason:      reason,
	}
	if err := a.resourceReports.Create(ctx, report); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleResourceCommentDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req resourceCommentDeleteRequest
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
	comment, err := a.resources.GetCommentByID(ctx, commentID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "comment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	item, err := a.resources.GetItemByID(ctx, comment.ItemID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	section, err := a.resources.GetSectionByID(ctx, item.SectionID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "section not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if comment.UserID != claims.Subject && section.OwnerID != claims.Subject {
		writeError(w, http.StatusForbidden, "forbidden", "cannot delete others comment")
		return
	}
	if err := a.resources.DeleteComment(ctx, commentID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "comment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func isValidResourceSectionName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	return true
}

func isValidResourceItemName(name string) bool {
	if len(name) < 1 || len(name) > 128 {
		return false
	}
	return true
}

func isValidResourceCommentBody(body string) bool {
	if len(body) < 1 || len(body) > 300 {
		return false
	}
	return true
}

func isValidResourceReportReason(reason string) bool {
	if len(reason) < 1 || len(reason) > 300 {
		return false
	}
	return true
}

func sanitizeResourceName(filename string) string {
	base := filepath.Base(filename)
	base = strings.TrimSpace(base)
	if base == "." || base == ".." {
		base = ""
	}
	if len(base) > 128 {
		base = base[:128]
	}
	return base
}

func sanitizeResourceExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" || len(ext) > 10 {
		return ".bin"
	}
	for i := 1; i < len(ext); i++ {
		c := ext[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		return ".bin"
	}
	return ext
}

func resourceUploadPath(uploadDir, url string) string {
	if url == "" {
		return ""
	}
	baseURL := strings.SplitN(url, "?", 2)[0]
	const prefix1 = "/v1/uploads/resources/"
	const prefix2 = "/uploads/resources/"
	name := ""
	switch {
	case strings.HasPrefix(baseURL, prefix1):
		name = strings.TrimPrefix(baseURL, prefix1)
	case strings.HasPrefix(baseURL, prefix2):
		name = strings.TrimPrefix(baseURL, prefix2)
	default:
		return ""
	}
	name = filepath.Base(name)
	if name == "" || name == "." || name == "/" {
		return ""
	}
	return filepath.Join(uploadDir, "resources", name)
}
