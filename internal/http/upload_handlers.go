package httpapi

import (
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/data"
)

func (a *API) handleAvatarUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	limitUploadBody(r)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "missing file")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	ext, err := avatarExtFromType(contentType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_type", "invalid file type")
		return
	}

	dir := filepath.Join(a.cfg.UploadDir, "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	filename := claims.Subject + "-" + nanoid.New() + ext
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	avatarURL := "/v1/uploads/avatars/" + filename
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if err := a.users.UpdateProfile(ctx, user.ID, user.DisplayName, avatarURL, user.Signature, user.CoverURL); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	go a.maybePushUploadToDataServer(avatarURL)
	user.AvatarURL = avatarURL
	writeJSON(w, http.StatusOK, toSelfUserResponse(user))
}

func (a *API) handleCoverUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
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

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	ext, err := avatarExtFromType(contentType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_type", "invalid file type")
		return
	}

	dir := filepath.Join(a.cfg.UploadDir, "covers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	filename := claims.Subject + "-" + nanoid.New() + ext
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	coverURL := "/v1/uploads/covers/" + filename
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	if err := a.users.UpdateProfile(ctx, user.ID, user.DisplayName, user.AvatarURL, user.Signature, coverURL); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	go a.maybePushUploadToDataServer(coverURL)
	user.CoverURL = coverURL
	writeJSON(w, http.StatusOK, toSelfUserResponse(user))
}

type mediaUploadResponse struct {
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url,omitempty"`
}

func (a *API) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	_, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	const maxMediaBytes = 50 << 20
	const maxImageMediaBytes = 3 << 20
	const maxMediaRequestBytes = maxMediaBytes + (2 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaRequestBytes)
	limitUploadBody(r)
	if err := r.ParseMultipartForm(maxMediaRequestBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "missing file")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = inferMediaMimeByExt(header.Filename)
	}
	ext, err := mediaExtFromType(contentType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_type", "invalid file type")
		return
	}
	normalizedType := strings.ToLower(strings.TrimSpace(contentType))
	if normalizedType == "video/3gpp" && looksLikeVoiceUploadFileName(header.Filename) {
		normalizedType = "audio/3gpp"
	}
	isVideo := strings.HasPrefix(normalizedType, "video/")
	isImage := strings.HasPrefix(normalizedType, "image/")
	isAudio := strings.HasPrefix(normalizedType, "audio/")
	if !isVideo && !isImage && !isAudio {
		switch strings.ToLower(filepath.Ext(header.Filename)) {
		case ".mp4":
			isVideo = true
		case ".3gp":
			if looksLikeVoiceUploadFileName(header.Filename) {
				isAudio = true
			} else {
				isVideo = true
			}
		case ".jpg", ".jpeg", ".png", ".gif", ".webp":
			isImage = true
		case ".amr", ".aac", ".mp3", ".m4a", ".wav", ".wave":
			isAudio = true
		}
	}
	if isVideo && !a.cfg.VideoEnabled {
		writeError(w, http.StatusForbidden, "video_disabled", "video upload disabled")
		return
	}
	if isImage && header.Size > int64(maxImageMediaBytes) {
		writeError(w, http.StatusRequestEntityTooLarge, "image_too_large", "image too large")
		return
	}

	dir := filepath.Join(a.cfg.UploadDir, "media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}

	filename := nanoid.New() + ext
	path := filepath.Join(dir, filename)
	dst, err := os.Create(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	defer dst.Close()

	release := acquireUploadSlot(isVideo)
	defer release()
	rate := mediaTransferRate()
	if isVideo {
		rate = videoTransferRate()
	}
	log.Printf("media upload classify: filename=%q content_type=%q normalized=%q is_image=%t is_audio=%t is_video=%t rate=%d", header.Filename, contentType, normalizedType, isImage, isAudio, isVideo, rate)
	maxFileBytes := int64(maxMediaBytes)
	if isImage {
		maxFileBytes = int64(maxImageMediaBytes)
	}
	src := io.Reader(file)
	if rate > 0 {
		src = newThrottledReader(file, rate)
	}
	written, err := io.Copy(dst, io.LimitReader(src, maxFileBytes+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload_failed", "upload failed")
		return
	}
	if written > maxFileBytes {
		_ = dst.Close()
		_ = os.Remove(path)
		writeError(w, http.StatusRequestEntityTooLarge, "image_too_large", "image too large")
		return
	}

	resp := mediaUploadResponse{
		URL: "/v1/uploads/media/" + filename,
	}

	thumb, thHeader, err := r.FormFile("thumb")
	if err == nil && thumb != nil {
		defer thumb.Close()
		thumbType := thHeader.Header.Get("Content-Type")
		if thumbType == "" {
			thumbType = mime.TypeByExtension(filepath.Ext(thHeader.Filename))
		}
		thumbExt, thumbErr := avatarExtFromType(thumbType)
		if thumbErr == nil {
			if thHeader.Size > int64(maxImageMediaBytes) {
				_ = os.Remove(path)
				writeError(w, http.StatusRequestEntityTooLarge, "image_too_large", "image too large")
				return
			}
			prefix := "thumb-"
			if isVideo {
				prefix = "vthumb-"
			}
			thumbName := prefix + nanoid.New() + thumbExt
			thumbPath := filepath.Join(dir, thumbName)
			thumbDst, err := os.Create(thumbPath)
			if err == nil {
				defer thumbDst.Close()
				thumbWritten, cpErr := io.Copy(thumbDst, io.LimitReader(thumb, int64(maxImageMediaBytes)+1))
				if cpErr == nil && thumbWritten <= int64(maxImageMediaBytes) {
					resp.ThumbURL = "/v1/uploads/media/" + thumbName
				} else {
					_ = thumbDst.Close()
					_ = os.Remove(thumbPath)
					if thumbWritten > int64(maxImageMediaBytes) {
						_ = os.Remove(path)
						writeError(w, http.StatusRequestEntityTooLarge, "image_too_large", "image too large")
						return
					}
				}
			}
		}
	}

	if err := a.pushUploadToDataServer(resp.URL); err != nil {
		writeError(w, http.StatusBadGateway, "data_sync_failed", "sync to data server failed")
		return
	}
	if strings.TrimSpace(resp.ThumbURL) != "" {
		if err := a.pushUploadToDataServer(resp.ThumbURL); err != nil {
			writeError(w, http.StatusBadGateway, "data_sync_failed", "sync to data server failed")
			return
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func inferMediaMimeByExt(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch filepath.Ext(lower) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".3gp":
		return "video/3gpp"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".amr":
		return "audio/amr"
	case ".wav", ".wave":
		return "audio/wav"
	}
	return ""
}

func looksLikeVoiceUploadFileName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "voice") || strings.Contains(lower, "audio") || strings.HasPrefix(lower, "record")
}

func avatarExtFromType(ct string) (string, error) {
	ct = strings.ToLower(ct)
	switch ct {
	case "image/png":
		return ".png", nil
	case "image/jpeg", "image/jpg":
		return ".jpg", nil
	case "image/gif":
		return ".gif", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", errors.New("unsupported")
	}
}

func mediaExtFromType(ct string) (string, error) {
	ct = strings.ToLower(ct)
	switch ct {
	case "image/png":
		return ".png", nil
	case "image/jpeg", "image/jpg":
		return ".jpg", nil
	case "image/gif":
		return ".gif", nil
	case "audio/3gpp":
		return ".3gp", nil
	case "audio/amr":
		return ".amr", nil
	case "audio/aac":
		return ".aac", nil
	case "audio/mpeg", "audio/mp3":
		return ".mp3", nil
	case "audio/mp4":
		return ".m4a", nil
	case "audio/wav", "audio/x-wav", "audio/wave", "audio/vnd.wave":
		return ".wav", nil
	case "video/mp4":
		return ".mp4", nil
	case "video/3gpp", "video/3gp", "video/3gpp2":
		return ".3gp", nil
	default:
		return "", errors.New("unsupported")
	}
}
