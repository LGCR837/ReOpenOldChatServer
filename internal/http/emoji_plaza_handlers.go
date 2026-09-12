package httpapi

import (
	"archive/zip"
	"bytes"
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
	emojiPlazaMaxNameLen  = 32
	emojiPlazaMaxZipBytes = 30 << 20
	emojiPlazaMaxZipItems = 500
	emojiPlazaMaxThumbMB  = 3 << 20
)

var errEmojiPlazaZipTooLarge = errors.New("zip too large")
var errEmojiPlazaZipInvalid = errors.New("invalid zip")
var errEmojiPlazaThumbTooLarge = errors.New("thumb too large")
var errEmojiPlazaThumbInvalid = errors.New("invalid thumb")

type emojiPlazaItemResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MediaURL    string `json:"media_url"`
	PackageURL  string `json:"package_url,omitempty"`
	CoverURL    string `json:"cover_url,omitempty"`
	ItemCount   int    `json:"item_count"`
	IsGIF       bool   `json:"is_gif"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedAt   int64  `json:"created_at"`
	OwnerUID    string `json:"owner_uid"`
	OwnerName   string `json:"owner_name"`
	OwnerTitle  string `json:"owner_title,omitempty"`
	OwnerAvatar string `json:"owner_avatar,omitempty"`
}

type emojiPlazaListResponse struct {
	Items   []emojiPlazaItemResponse `json:"items"`
	Total   int                      `json:"total"`
	HasMore bool                     `json:"has_more"`
	Limit   int                      `json:"limit"`
	Offset  int                      `json:"offset"`
}

type emojiPlazaSaveRequest struct {
	ItemID string `json:"item_id"`
}

type emojiPlazaDeleteRequest struct {
	ItemID string `json:"item_id"`
}

type emojiPlazaSaveResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MediaURL   string `json:"media_url"`
	PackageURL string `json:"package_url,omitempty"`
	CoverURL   string `json:"cover_url,omitempty"`
	ItemCount  int    `json:"item_count"`
	IsGIF      bool   `json:"is_gif"`
}

func buildEmojiPlazaItemResponse(item data.EmojiPlazaItem) emojiPlazaItemResponse {
	coverURL := item.CoverURL
	if strings.TrimSpace(coverURL) == "" {
		coverURL = item.MediaURL
	}
	itemCount := item.ItemCount
	if itemCount <= 0 {
		itemCount = 1
	}
	return emojiPlazaItemResponse{
		ID:          item.ID,
		Name:        item.Name,
		MediaURL:    item.MediaURL,
		PackageURL:  item.PackageURL,
		CoverURL:    coverURL,
		ItemCount:   itemCount,
		IsGIF:       item.IsGIF != 0,
		SizeBytes:   item.SizeBytes,
		CreatedAt:   item.CreatedAt.Unix(),
		OwnerUID:    item.OwnerUID,
		OwnerName:   item.OwnerName,
		OwnerTitle:  item.OwnerTitle,
		OwnerAvatar: item.OwnerAvatar,
	}
}

func (a *API) handleEmojiPlazaList(w http.ResponseWriter, r *http.Request) {
	_, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	items, err := a.emojis.List(ctx, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	total, err := a.emojis.Count(ctx, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := make([]emojiPlazaItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, buildEmojiPlazaItemResponse(item))
	}

	writeJSON(w, http.StatusOK, emojiPlazaListResponse{
		Items:   resp,
		Total:   total,
		HasMore: offset+len(resp) < total,
		Limit:   limit,
		Offset:  offset,
	})
}

func (a *API) handleEmojiPlazaMineList(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := parseLimit(r.URL.Query().Get("limit"))
	offset := parseOffset(r.URL.Query().Get("offset"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	items, err := a.emojis.ListByOwner(ctx, claims.Subject, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	total, err := a.emojis.CountByOwner(ctx, claims.Subject, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := make([]emojiPlazaItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, buildEmojiPlazaItemResponse(item))
	}

	writeJSON(w, http.StatusOK, emojiPlazaListResponse{
		Items:   resp,
		Total:   total,
		HasMore: offset+len(resp) < total,
		Limit:   limit,
		Offset:  offset,
	})
}

func (a *API) handleEmojiPlazaDelete(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req emojiPlazaDeleteRequest
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
	item, err := a.emojis.GetByID(ctx, itemID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if strings.TrimSpace(item.OwnerID) != strings.TrimSpace(claims.Subject) {
		writeError(w, http.StatusForbidden, "forbidden", "not owner")
		return
	}
	if err := a.emojis.DeleteByOwner(ctx, itemID, claims.Subject); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	paths := map[string]struct{}{}
	for _, rawURL := range []string{item.MediaURL, item.PackageURL, item.CoverURL} {
		if path := emojiPlazaUploadedFilePath(a.cfg.UploadDir, rawURL); path != "" {
			paths[path] = struct{}{}
		}
	}
	for path := range paths {
		_ = os.Remove(path)
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleEmojiPlazaUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 40<<20)
	limitUploadBody(r)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if !isValidEmojiPlazaName(name) {
		writeError(w, http.StatusBadRequest, "invalid_name", "invalid name")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "missing file")
		return
	}
	if file != nil {
		_ = file.Close()
	}
	if header == nil {
		writeError(w, http.StatusBadRequest, "missing_file", "missing file")
		return
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	isZipUpload := isZipContentType(contentType, header.Filename)
	if !isZipUpload && !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeError(w, http.StatusBadRequest, "invalid_type", "invalid file type")
		return
	}

	coverURL := strings.TrimSpace(r.FormValue("cover_url"))
	mediaURL := ""
	packageURL := ""
	itemCount := 1
	isGIF := 0
	sizeBytes := int64(0)

	if isZipUpload {
		zURL, zSize, zCount, zErr := saveEmojiPlazaZipUpload(a, r)
		if zErr != nil {
			switch {
			case errors.Is(zErr, errEmojiPlazaZipTooLarge):
				writeError(w, http.StatusRequestEntityTooLarge, "package_too_large", "emoji package too large")
			case errors.Is(zErr, errEmojiPlazaZipInvalid):
				writeError(w, http.StatusBadRequest, "invalid_package", "invalid emoji package")
			default:
				writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
			}
			return
		}

		thumbURL, thumbGIF, thumbErr := saveEmojiPlazaThumbUpload(a, r)
		if thumbErr != nil {
			switch {
			case errors.Is(thumbErr, errEmojiPlazaThumbTooLarge):
				writeError(w, http.StatusRequestEntityTooLarge, "image_too_large", "image too large")
			case errors.Is(thumbErr, errEmojiPlazaThumbInvalid):
				writeError(w, http.StatusBadRequest, "invalid_cover", "invalid cover")
			default:
				writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
			}
			return
		}

		if coverURL == "" {
			coverURL = thumbURL
		}
		if coverURL == "" {
			writeError(w, http.StatusBadRequest, "missing_cover", "missing cover")
			return
		}

		mediaURL = coverURL
		packageURL = zURL
		itemCount = zCount
		sizeBytes = zSize
		isGIF = thumbGIF
	} else {
		media, wroteResponse, uploadErr := uploadEmojiMedia(a, w, r)
		if wroteResponse {
			return
		}
		if uploadErr != nil || media == nil || strings.TrimSpace(media.URL) == "" {
			writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
			return
		}
		mediaURL = media.URL
		if strings.Contains(strings.ToLower(media.URL), ".gif") || strings.EqualFold(contentType, "image/gif") {
			isGIF = 1
		}
		if header.Size > 0 {
			sizeBytes = header.Size
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	dupKey := mediaURL
	if isZipUpload {
		dupKey = packageURL
	}
	exists, err := a.emojis.ExistsByOwnerAndMediaURL(ctx, claims.Subject, dupKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if isZipUpload && !exists {
		exists, err = a.emojis.ExistsByOwnerAndPackageURL(ctx, claims.Subject, packageURL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
	}
	if exists {
		writeError(w, http.StatusConflict, "duplicate_emoji", "duplicate emoji")
		return
	}

	item := &data.EmojiPlazaItem{
		ID:         data.NewID(),
		OwnerID:    claims.Subject,
		Name:       name,
		MediaURL:   mediaURL,
		PackageURL: packageURL,
		CoverURL:   coverURL,
		ItemCount:  itemCount,
		IsGIF:      isGIF,
		SizeBytes:  sizeBytes,
	}
	if err := a.emojis.Create(ctx, item); err != nil {
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

	writeJSON(w, http.StatusCreated, emojiPlazaItemResponse{
		ID:          item.ID,
		Name:        item.Name,
		MediaURL:    item.MediaURL,
		PackageURL:  item.PackageURL,
		CoverURL:    item.CoverURL,
		ItemCount:   item.ItemCount,
		IsGIF:       item.IsGIF != 0,
		SizeBytes:   item.SizeBytes,
		CreatedAt:   time.Now().Unix(),
		OwnerUID:    claims.UID,
		OwnerName:   ownerName,
		OwnerTitle:  ownerTitle,
		OwnerAvatar: ownerAvatar,
	})
}

func isZipContentType(contentType, filename string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "application/zip" || ct == "application/x-zip-compressed" || ct == "multipart/x-zip" {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".zip")
}

func (a *API) handleEmojiPlazaSave(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	_, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var req emojiPlazaSaveRequest
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
	item, err := a.emojis.GetByID(ctx, itemID)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, emojiPlazaSaveResponse{
		ID:         item.ID,
		Name:       item.Name,
		MediaURL:   item.MediaURL,
		PackageURL: item.PackageURL,
		CoverURL:   item.CoverURL,
		ItemCount:  item.ItemCount,
		IsGIF:      item.IsGIF != 0,
	})
}

func isValidEmojiPlazaName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if utf8Len(name) > emojiPlazaMaxNameLen {
		return false
	}
	return true
}

func utf8Len(s string) int {
	count := 0
	for range s {
		count++
	}
	return count
}

type capturedMediaUpload struct {
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url"`
}

func parseMediaUploadResponse(body []byte) (*capturedMediaUpload, error) {
	var out capturedMediaUpload
	if len(body) == 0 {
		return &out, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func uploadEmojiMedia(a *API, w http.ResponseWriter, r *http.Request) (*capturedMediaUpload, bool, error) {
	if a == nil || r == nil {
		return nil, false, io.ErrUnexpectedEOF
	}
	originalPath := r.URL.Path
	originalURI := r.RequestURI
	r.URL.Path = "/v1/media"
	r.RequestURI = "/v1/media"

	capture := newResponseCapture()
	a.handleMediaUpload(capture, r)

	r.URL.Path = originalPath
	r.RequestURI = originalURI

	if capture.status < 200 || capture.status >= 300 {
		capture.flushTo(w)
		return nil, true, nil
	}
	resp, err := parseMediaUploadResponse(capture.body)
	if err != nil {
		return nil, false, err
	}
	return resp, false, nil
}

func saveEmojiPlazaZipUpload(a *API, r *http.Request) (string, int64, int, error) {
	if a == nil || r == nil {
		return "", 0, 0, errEmojiPlazaZipInvalid
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", 0, 0, errEmojiPlazaZipInvalid
	}
	defer file.Close()
	if header == nil {
		return "", 0, 0, errEmojiPlazaZipInvalid
	}
	if header.Size > int64(emojiPlazaMaxZipBytes) {
		return "", 0, 0, errEmojiPlazaZipTooLarge
	}

	dir := filepath.Join(a.cfg.UploadDir, "emoji-packs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, 0, err
	}
	filename := "epk-" + data.NewID() + ".zip"
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		return "", 0, 0, err
	}
	release := acquireUploadSlot(false)
	defer release()
	src := io.Reader(file)
	rate := mediaTransferRate()
	if rate > 0 {
		src = newThrottledReader(file, rate)
	}
	written, cpErr := io.Copy(dst, io.LimitReader(src, int64(emojiPlazaMaxZipBytes)+1))
	_ = dst.Close()
	if cpErr != nil {
		_ = os.Remove(path)
		return "", 0, 0, cpErr
	}
	if written <= 0 {
		_ = os.Remove(path)
		return "", 0, 0, errEmojiPlazaZipInvalid
	}
	if written > int64(emojiPlazaMaxZipBytes) {
		_ = os.Remove(path)
		return "", 0, 0, errEmojiPlazaZipTooLarge
	}

	itemCount, err := countEmojiFilesInZip(path)
	if err != nil {
		_ = os.Remove(path)
		return "", 0, 0, err
	}
	if itemCount <= 0 {
		_ = os.Remove(path)
		return "", 0, 0, errEmojiPlazaZipInvalid
	}

	uploadURL := "/v1/uploads/emoji-packs/" + filename
	go a.maybePushUploadToDataServer(uploadURL)
	return uploadURL, written, itemCount, nil
}

func saveEmojiPlazaThumbUpload(a *API, r *http.Request) (string, int, error) {
	if a == nil || r == nil {
		return "", 0, errEmojiPlazaThumbInvalid
	}
	file, header, err := r.FormFile("thumb")
	if err != nil {
		return "", 0, nil
	}
	defer file.Close()
	if header == nil {
		return "", 0, errEmojiPlazaThumbInvalid
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	ext, extErr := avatarExtFromType(contentType)
	if extErr != nil {
		return "", 0, errEmojiPlazaThumbInvalid
	}
	if header.Size > int64(emojiPlazaMaxThumbMB) {
		return "", 0, errEmojiPlazaThumbTooLarge
	}

	dir := filepath.Join(a.cfg.UploadDir, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	filename := "ep-cover-" + data.NewID() + ext
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		return "", 0, err
	}
	release := acquireUploadSlot(false)
	defer release()
	src := io.Reader(file)
	rate := mediaTransferRate()
	if rate > 0 {
		src = newThrottledReader(file, rate)
	}
	written, cpErr := io.Copy(dst, io.LimitReader(src, int64(emojiPlazaMaxThumbMB)+1))
	_ = dst.Close()
	if cpErr != nil {
		_ = os.Remove(path)
		return "", 0, cpErr
	}
	if written <= 0 {
		_ = os.Remove(path)
		return "", 0, errEmojiPlazaThumbInvalid
	}
	if written > int64(emojiPlazaMaxThumbMB) {
		_ = os.Remove(path)
		return "", 0, errEmojiPlazaThumbTooLarge
	}

	isGIF := 0
	if ext == ".gif" {
		isGIF = 1
	}
	uploadURL := "/v1/uploads/media/" + filename
	go a.maybePushUploadToDataServer(uploadURL)
	return uploadURL, isGIF, nil
}

func countEmojiFilesInZip(path string) (int, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return 0, errEmojiPlazaZipInvalid
	}
	defer reader.Close()

	count := 0
	for _, f := range reader.File {
		if f == nil || f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif":
			count++
			if count > emojiPlazaMaxZipItems {
				return 0, errEmojiPlazaZipInvalid
			}
		}
	}
	if count <= 0 {
		return 0, errEmojiPlazaZipInvalid
	}
	return count, nil
}

func emojiPlazaUploadedFilePath(uploadDir, fileURL string) string {
	if fileURL == "" {
		return ""
	}
	baseURL := strings.SplitN(fileURL, "?", 2)[0]
	name := ""
	subdir := ""
	switch {
	case strings.HasPrefix(baseURL, "/v1/uploads/emoji-packs/"):
		name = strings.TrimPrefix(baseURL, "/v1/uploads/emoji-packs/")
		subdir = "emoji-packs"
	case strings.HasPrefix(baseURL, "/uploads/emoji-packs/"):
		name = strings.TrimPrefix(baseURL, "/uploads/emoji-packs/")
		subdir = "emoji-packs"
	case strings.HasPrefix(baseURL, "/v1/uploads/media/"):
		name = strings.TrimPrefix(baseURL, "/v1/uploads/media/")
		subdir = "media"
	case strings.HasPrefix(baseURL, "/uploads/media/"):
		name = strings.TrimPrefix(baseURL, "/uploads/media/")
		subdir = "media"
	default:
		return ""
	}
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return filepath.Join(uploadDir, subdir, name)
}

type responseCapture struct {
	headers http.Header
	status  int
	body    []byte
}

func newResponseCapture() *responseCapture {
	return &responseCapture{headers: make(http.Header), status: http.StatusOK}
}

func (c *responseCapture) Header() http.Header {
	return c.headers
}

func (c *responseCapture) WriteHeader(statusCode int) {
	c.status = statusCode
}

func (c *responseCapture) Write(b []byte) (int, error) {
	if b == nil {
		return 0, nil
	}
	c.body = append(c.body, b...)
	return len(b), nil
}

func (c *responseCapture) flushTo(w http.ResponseWriter) {
	if w == nil {
		return
	}
	for key, values := range c.headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if c.status <= 0 {
		c.status = http.StatusInternalServerError
	}
	w.WriteHeader(c.status)
	if len(c.body) > 0 {
		_, _ = io.Copy(w, bytes.NewReader(c.body))
	}
}
