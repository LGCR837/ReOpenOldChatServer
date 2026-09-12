package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"metrochat/internal/data"
)

func (a *API) handleUserReport(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var body struct {
		TargetUID string `json:"target_uid"`
		Reason    string `json:"reason"`
	}
	if !requireJSON(w, r) {
		return
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	targetUID := strings.TrimSpace(body.TargetUID)
	if targetUID == "" {
		writeError(w, http.StatusBadRequest, "missing_field", "target_uid is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	reporter, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	targetUser, err := a.users.GetByUID(ctx, targetUID)
	if err != nil || targetUser == nil {
		writeError(w, http.StatusNotFound, "not_found", "target user not found")
		return
	}
	deviceID, _, _ := a.devices.LatestDeviceForUser(ctx, targetUser.ID)
	report := &data.UserReport{
		ReporterID:   reporter.ID,
		ReporterUID:  reporter.UID,
		TargetUserID: targetUser.ID,
		TargetUID:    targetUser.UID,
		TargetDevice: deviceID,
		Reason:       strings.TrimSpace(body.Reason),
	}
	if err := a.reportStore.CreateUserReport(ctx, report); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to save report")
		return
	}
	_, _, _ = a.publicCourt.OpenOrCreate(ctx, &data.PublicCourtCase{
		ReportID:       report.ID,
		ReporterID:     reporter.ID,
		ReporterUID:    reporter.UID,
		DefendantID:    targetUser.ID,
		DefendantUID:   targetUser.UID,
		ReportReason:   report.Reason,
		ReportEvidence: report.Reason,
		Status:         "open",
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "report submitted",
	})
}

func (a *API) handleGroupReport(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	var body struct {
		GroupID string `json:"group_id"`
		Reason  string `json:"reason"`
	}
	if !requireJSON(w, r) {
		return
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	groupID := strings.ToUpper(strings.TrimSpace(body.GroupID))
	if !isValidGroupID(groupID) {
		writeError(w, http.StatusBadRequest, "invalid_group_id", "group_id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	reporter, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	group, err := a.groups.GetByID(ctx, groupID)
	if err != nil || group == nil {
		if err == data.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	report := &data.GroupReport{
		ReporterID:  reporter.ID,
		ReporterUID: reporter.UID,
		GroupID:     group.ID,
		GroupName:   group.Name,
		Reason:      strings.TrimSpace(body.Reason),
	}
	if err := a.groupReportStore.Create(ctx, report); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to save report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "report submitted",
	})
}
