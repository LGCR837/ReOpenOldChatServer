package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"metrochat/internal/data"
)

type submitCrashReportRequest struct {
	CrashLog       string `json:"crash_log"`
	DeviceModel    string `json:"device_model"`
	AndroidVersion string `json:"android_version"`
}

func (a *API) handleSubmitCrashReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := claimsFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	var req submitCrashReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	if req.CrashLog == "" {
		writeError(w, http.StatusBadRequest, "missing_field", "crash_log is required")
		return
	}

	var userID *string
	ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
	user, err := a.users.GetByID(ctx2, claims.Subject)
	cancel()
	if err == nil && user != nil {
		userID = &user.ID
	}

	report := &data.CrashReport{
		ID:             uuid.New().String(),
		UserID:         userID,
		CrashLog:       req.CrashLog,
		DeviceModel:    req.DeviceModel,
		AndroidVersion: req.AndroidVersion,
	}

	ctx3, cancel := context.WithTimeout(ctx, 2*time.Second)
	if err := a.crashReportStore.Create(ctx3, report); err != nil {
		cancel()
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to save crash report")
		return
	}
	cancel()

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "crash report submitted successfully",
	})
}

func (a *API) handleListCrashReports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 检查管理员会话
	if !a.isAdmin(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		var parsedLimit int
		if err := json.Unmarshal([]byte(l), &parsedLimit); err == nil && parsedLimit > 0 && parsedLimit <= 1000 {
			limit = parsedLimit
		}
	}

	ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
	reports, err := a.crashReportStore.ListRecent(ctx2, limit)
	cancel()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list crash reports")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reports": reports,
	})
}

func (a *API) handleGetCrashReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 检查管理员会话
	if !a.isAdmin(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	reportID := chi.URLParam(r, "id")
	if reportID == "" {
		writeError(w, http.StatusBadRequest, "missing_field", "report id is required")
		return
	}

	ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
	report, err := a.crashReportStore.GetByID(ctx2, reportID)
	cancel()
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "crash report not found")
		return
	}

	writeJSON(w, http.StatusOK, report)
}
