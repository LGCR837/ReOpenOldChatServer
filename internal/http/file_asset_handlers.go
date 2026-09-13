package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"
	"github.com/go-chi/chi/v5"

	"ReOpenOldChatServer/internal/data"
)

// 官方 v2 文件上限 500MiB（v1 资源上传为 100MiB，见 maxResourceUploadBytes）。
const maxFileUploadBytes = 500 << 20

type fileUploadResponse struct {
	FileID       string `json:"file_id"`
	Name         string `json:"name"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	URL          string `json:"url"`
	Deduplicated bool   `json:"deduplicated"`
}

type fileCheckRequest struct {
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// fileURL 是客户端取回该资产的地址。官方用独立域名 files.*，
// 本机走同源 /v2/files/download/{id}（免签名豁免路径，仅 Bearer）。
func fileURL(fileID string) string {
	return "/v2/files/download/" + fileID
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

// handleFileCheck 秒传检查：内容已在库则直接返回既有资产，客户端无需重传。
func (a *API) handleFileCheck(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req fileCheckRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	sha := strings.ToLower(strings.TrimSpace(req.SHA256))
	if !isHexSHA256(sha) {
		writeError(w, http.StatusBadRequest, "invalid_sha256", "sha256 must be 64 lowercase hex chars")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	asset, err := a.fileAssets.FindBySHA256(ctx, sha)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"exists": false})
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"exists":     true,
		"file_id":    asset.ID,
		"name":       asset.Name,
		"size_bytes": asset.SizeBytes,
		"sha256":     asset.SHA256,
		"url":        fileURL(asset.ID),
	})
}

// handleFileUpload 聊天文件上传。multipart 字段 file，上限 500MiB。
// 边写盘边算 SHA-256，落盘前先查重：内容相同则丢弃本次副本（秒传）。
func (a *API) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFileUploadBytes)
	limitUploadBody(r)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "missing file")
		return
	}
	defer file.Close()

	dir := filepath.Join(a.cfg.UploadDir, "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	// 先写临时名，确认内容后才改成正式名，避免半截文件被当成完整资产
	tmpPath := filepath.Join(dir, "tmp_"+nanoid.New())
	dst, err := os.Create(tmpPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(file, maxFileUploadBytes+1))
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	if written <= 0 || written > maxFileUploadBytes {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusBadRequest, "invalid_size", "invalid size")
		return
	}
	sha := hex.EncodeToString(hash.Sum(nil))

	name := sanitizeResourceName(header.Filename)
	if name == "" {
		name = "file"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if existing, err := a.fileAssets.FindBySHA256(ctx, sha); err == nil {
		_ = os.Remove(tmpPath)
		writeJSON(w, http.StatusCreated, fileUploadResponse{
			FileID:       existing.ID,
			Name:         existing.Name,
			SizeBytes:    existing.SizeBytes,
			SHA256:       existing.SHA256,
			URL:          fileURL(existing.ID),
			Deduplicated: true,
		})
		return
	}

	assetID := nanoid.New()
	storedName := assetID + sanitizeResourceExt(name)
	finalPath := filepath.Join(dir, storedName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	created, err := a.fileAssets.Create(ctx, &data.FileAsset{
		ID:         assetID,
		SHA256:     sha,
		Name:       name,
		StoredName: storedName,
		SizeBytes:  written,
		OwnerID:    claims.Subject,
	})
	if err != nil {
		_ = os.Remove(finalPath)
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if !created {
		// 并发同内容上传：唯一索引挡下，改用对方那条，删掉本副本
		_ = os.Remove(finalPath)
		existing, findErr := a.fileAssets.FindBySHA256(ctx, sha)
		if findErr != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		writeJSON(w, http.StatusCreated, fileUploadResponse{
			FileID:       existing.ID,
			Name:         existing.Name,
			SizeBytes:    existing.SizeBytes,
			SHA256:       existing.SHA256,
			URL:          fileURL(existing.ID),
			Deduplicated: true,
		})
		return
	}

	writeJSON(w, http.StatusCreated, fileUploadResponse{
		FileID:       assetID,
		Name:         name,
		SizeBytes:    written,
		SHA256:       sha,
		URL:          fileURL(assetID),
		Deduplicated: false,
	})
}

// handleFileDownload 文件资产下载。用 http.ServeContent 拿 HTTP Range 断点续传，
// 并把 SHA-256 作为 ETag 供客户端做条件请求。
func (a *API) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	fileID := strings.TrimSpace(chi.URLParam(r, "fileID"))
	if fileID == "" {
		fileID = strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v2/files/download/"))
	}
	if fileID == "" || strings.ContainsAny(fileID, "/\\") {
		writeError(w, http.StatusBadRequest, "invalid_file_id", "invalid file id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	asset, err := a.fileAssets.GetByID(ctx, fileID)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	path := filepath.Join(a.cfg.UploadDir, "files", asset.StoredName)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	defer f.Close()

	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(asset.Name))
	w.Header().Set("ETag", `"`+asset.SHA256+`"`)
	http.ServeContent(w, r, asset.Name, asset.Created, f)
}

// handleResourceDownload 资源广场条目下载：本地条目直接回源文件，
// 已在对象存储的条目（URL 非本地上传前缀）改走 302，避免服务端中转。
func (a *API) handleResourceDownload(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	itemID := strings.TrimSpace(chi.URLParam(r, "itemID"))
	if itemID == "" {
		itemID = strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v2/resources/download/"))
	}
	if itemID == "" || strings.ContainsAny(itemID, "/\\") {
		writeError(w, http.StatusBadRequest, "invalid_item_id", "invalid item id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	item, err := a.resources.GetItemByID(ctx, itemID)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	const localPrefix = "/v1/uploads/resources/"
	if !strings.HasPrefix(item.URL, localPrefix) {
		http.Redirect(w, r, item.URL, http.StatusFound)
		return
	}
	name := strings.TrimSpace(strings.TrimPrefix(item.URL, localPrefix))
	if name == "" || strings.ContainsAny(name, "/\\") {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}

	path := filepath.Join(a.cfg.UploadDir, "resources", name)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "read failed")
		return
	}

	display := item.Name
	if display == "" {
		display = name
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(display))
	http.ServeContent(w, r, display, info.ModTime(), f)
}

// contentDispositionAttachment 生成 attachment 头。
// 非 ASCII 文件名（中文很常见）必须补 RFC 5987 的 filename*，否则客户端拿到乱码。
func contentDispositionAttachment(name string) string {
	if strings.TrimSpace(name) == "" {
		name = "file"
	}
	var ascii strings.Builder
	for _, c := range name {
		if c < 128 && c != '"' && c != '\\' {
			ascii.WriteRune(c)
			continue
		}
		ascii.WriteByte('_')
	}
	return `attachment; filename="` + ascii.String() + `"; filename*=UTF-8''` + url.PathEscape(name)
}
