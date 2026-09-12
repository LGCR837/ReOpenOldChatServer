package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"metrochat/internal/data"
)

func (a *API) handleSubmitBugReport(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var body struct {
		Content        string `json:"content"`
		DeviceModel    string `json:"device_model"`
		AndroidVersion string `json:"android_version"`
		AppVersion     string `json:"app_version"`
	}
	if !requireJSON(w, r) {
		return
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	content := strings.TrimSpace(body.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "missing_field", "content is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	report := &data.BugReport{
		UserID:         user.ID,
		UserUID:        user.UID,
		Content:        content,
		DeviceModel:    strings.TrimSpace(body.DeviceModel),
		AndroidVersion: strings.TrimSpace(body.AndroidVersion),
		AppVersion:     strings.TrimSpace(body.AppVersion),
	}
	if err := a.bugReportStore.Create(ctx, report); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to save bug report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "bug report submitted",
	})
}
