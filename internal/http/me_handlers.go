package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"metrochat/internal/auth"
	"metrochat/internal/data"
)

type updateUIDRequest struct {
	UID string `json:"uid"`
}

type updateProfileRequest struct {
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Signature   *string `json:"signature"`
	CoverURL    *string `json:"cover_url"`
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, toSelfUserResponse(user))
}

func (a *API) handleUpdateUID(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req updateUIDRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	uid := strings.ToUpper(strings.TrimSpace(req.UID))
	if !isValidUID(uid) {
		writeError(w, http.StatusBadRequest, "invalid_uid", "invalid uid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if user.UID == uid {
		writeJSON(w, http.StatusOK, toSelfUserResponse(user))
		return
	}

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	allowFirstChange := user.UIDChangedAt.Equal(user.CreatedAt)
	if user.UIDChangedAt.After(cutoff) && !allowFirstChange {
		writeError(w, http.StatusConflict, "uid_change_wait", "uid change cooldown")
		return
	}

	if err := a.users.UpdateUID(ctx, user.ID, uid, cutoff); err != nil {
		if err == data.ErrUIDTooSoon {
			writeError(w, http.StatusConflict, "uid_change_wait", "uid change cooldown")
			return
		}
		if isUniqueViolation(err, "users_uid_key") {
			writeError(w, http.StatusConflict, "uid_taken", "uid already used")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	user.UID = uid
	writeJSON(w, http.StatusOK, toSelfUserResponse(user))
}

func (a *API) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req updateProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	displayName := user.DisplayName
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if !isValidDisplayName(displayName) {
			writeError(w, http.StatusBadRequest, "invalid_display_name", "invalid display name")
			return
		}
	}

	avatarURL := user.AvatarURL
	if req.AvatarURL != nil {
		avatarURL = strings.TrimSpace(*req.AvatarURL)
		if !isValidAvatarURL(avatarURL) {
			writeError(w, http.StatusBadRequest, "invalid_avatar_url", "invalid avatar url")
			return
		}
	}

	signature := user.Signature
	if req.Signature != nil {
		signature = strings.TrimSpace(*req.Signature)
		if !isValidSignature(signature) {
			writeError(w, http.StatusBadRequest, "invalid_signature", "invalid signature")
			return
		}
	}

	coverURL := user.CoverURL
	if req.CoverURL != nil {
		coverURL = strings.TrimSpace(*req.CoverURL)
		if !isValidCoverURL(coverURL) {
			writeError(w, http.StatusBadRequest, "invalid_cover_url", "invalid cover url")
			return
		}
	}

	if err := a.users.UpdateProfile(ctx, claims.Subject, displayName, avatarURL, signature, coverURL); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	user, err = a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toSelfUserResponse(user))
}

type updatePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type deleteAccountRequest struct {
	Password string `json:"password"`
}

func (a *API) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req updatePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	oldPass := strings.TrimSpace(req.OldPassword)
	newPass := strings.TrimSpace(req.NewPassword)

	if oldPass == "" || newPass == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "missing password")
		return
	}
	if len(newPass) < 8 {
		writeError(w, http.StatusBadRequest, "invalid_password", "password too short")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	ok, err = auth.VerifyPassword(oldPass, user.PasswordHash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid old password")
		return
	}

	newHash, err := auth.HashPassword(newPass)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash_failed", "internal error")
		return
	}

	if err := a.users.UpdatePassword(ctx, user.ID, newHash); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req deleteAccountRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	password := strings.TrimSpace(req.Password)
	if password == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "missing password")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if user.ID == data.SystemNotificationUID || strings.EqualFold(user.UID, data.SystemNotificationUID) {
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
		return
	}

	ok, err = auth.VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}

	_ = a.refresh.RevokeAllByUser(ctx, user.ID)
	a.clearTokenVersionCache(user.ID)
	if err := a.users.DeleteByID(ctx, user.ID); err != nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

type userDeviceItem struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
	LastSeen   int64  `json:"last_seen"`
	CreatedAt  int64  `json:"created_at"`
}

type userDevicesResponse struct {
	Devices []userDeviceItem `json:"devices"`
}

func (a *API) handleMeDevices(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	devices, err := a.devices.ListUserLoginDevices(ctx, claims.Subject, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := userDevicesResponse{Devices: make([]userDeviceItem, 0, len(devices))}
	for _, d := range devices {
		resp.Devices = append(resp.Devices, userDeviceItem{
			DeviceID:   d.DeviceID,
			DeviceName: d.DeviceName,
			Platform:   d.Platform,
			AppVersion: d.AppVersion,
			LastSeen:   d.LastSeen.Unix(),
			CreatedAt:  d.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func isValidUID(uid string) bool {
	if len(uid) < 4 || len(uid) > 20 {
		return false
	}
	for i := 0; i < len(uid); i++ {
		c := uid[i]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			continue
		}
		return false
	}
	return true
}

func isValidDisplayName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	return true
}

func isValidAvatarURL(url string) bool {
	if url == "" {
		return true
	}
	if len(url) > 512 {
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

func isValidSignature(signature string) bool {
	if len(signature) > 140 {
		return false
	}
	return true
}

func isValidCoverURL(url string) bool {
	if url == "" {
		return true
	}
	if len(url) > 512 {
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
