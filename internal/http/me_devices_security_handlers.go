package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"metrochat/internal/data"
)

type cleanupOtherDevicesRequest struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	IMEI       string `json:"imei"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

type cleanupOtherDevicesResponse struct {
	Status       string `json:"status"`
	RemovedCount int64  `json:"removed_count"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (a *API) handleMeDevicesCleanupOthers(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req cleanupOtherDevicesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "missing device id")
		return
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	imei := strings.TrimSpace(req.IMEI)
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	appVersion := strings.TrimSpace(req.AppVersion)

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
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

	removedCount, err := a.devices.DeleteOtherUserDevices(ctx, user.ID, deviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	_ = a.refresh.RevokeAllByUser(ctx, user.ID)
	newVersion, err := a.users.IncrementTokenVersion(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed", "internal error")
		return
	}
	user.TokenVersion = newVersion
	a.setTokenVersionCache(user.ID, newVersion)

	tokens, err := a.issueTokens(ctx, user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed", "internal error")
		return
	}

	_ = a.devices.UpsertUserDevice(ctx, user.ID, deviceID, imei)
	_ = a.devices.UpsertLoginDevice(ctx, user.ID, deviceID, deviceName, platform, appVersion)

	writeJSON(w, http.StatusOK, cleanupOtherDevicesResponse{
		Status:       "ok",
		RemovedCount: removedCount,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
}
