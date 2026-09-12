package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

func unixPtr(t sql.NullTime) *int64 {
	if !t.Valid {
		return nil
	}
	v := t.Time.Unix()
	return &v
}

func (a *API) handleMeBugReports(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reports, err := a.bugReportStore.ListByUser(ctx, claims.Subject, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	type item struct {
		ID             string `json:"id"`
		Content        string `json:"content"`
		DeviceModel    string `json:"device_model"`
		AndroidVersion string `json:"android_version"`
		AppVersion     string `json:"app_version"`
		Status         string `json:"status"`
		AdminNote      string `json:"admin_note"`
		ResolvedAt     *int64 `json:"resolved_at"`
		CreatedAt      int64  `json:"created_at"`
	}
	out := make([]item, 0, len(reports))
	for _, it := range reports {
		out = append(out, item{
			ID:             it.ID,
			Content:        it.Content,
			DeviceModel:    it.DeviceModel,
			AndroidVersion: it.AndroidVersion,
			AppVersion:     it.AppVersion,
			Status:         it.Status,
			AdminNote:      it.AdminNote,
			ResolvedAt:     unixPtr(it.ResolvedAt),
			CreatedAt:      it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (a *API) handleAllBugReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reports, err := a.bugReportStore.ListRecent(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	type item struct {
		ID             string `json:"id"`
		UserUID        string `json:"user_uid"`
		Content        string `json:"content"`
		DeviceModel    string `json:"device_model"`
		AndroidVersion string `json:"android_version"`
		AppVersion     string `json:"app_version"`
		Status         string `json:"status"`
		AdminNote      string `json:"admin_note"`
		ResolvedAt     *int64 `json:"resolved_at"`
		CreatedAt      int64  `json:"created_at"`
	}
	out := make([]item, 0, len(reports))
	for _, it := range reports {
		out = append(out, item{
			ID:             it.ID,
			UserUID:        it.UserUID,
			Content:        it.Content,
			DeviceModel:    it.DeviceModel,
			AndroidVersion: it.AndroidVersion,
			AppVersion:     it.AppVersion,
			Status:         it.Status,
			AdminNote:      it.AdminNote,
			ResolvedAt:     unixPtr(it.ResolvedAt),
			CreatedAt:      it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (a *API) handleMeUserReports(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reports, err := a.reportStore.ListUserReportsByReporter(ctx, claims.Subject, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	type item struct {
		ID        string `json:"id"`
		TargetUID string `json:"target_uid"`
		Reason    string `json:"reason"`
		Status    string `json:"status"`
		Result    string `json:"result"`
		HandledAt *int64 `json:"handled_at"`
		CreatedAt int64  `json:"created_at"`
	}
	out := make([]item, 0, len(reports))
	for _, it := range reports {
		out = append(out, item{
			ID:        it.ID,
			TargetUID: it.TargetUID,
			Reason:    it.Reason,
			Status:    it.Status,
			Result:    it.Result,
			HandledAt: unixPtr(it.HandledAt),
			CreatedAt: it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (a *API) handleMeGroupReports(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reports, err := a.groupReportStore.ListByReporter(ctx, claims.Subject, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	type item struct {
		ID        string `json:"id"`
		GroupID   string `json:"group_id"`
		GroupName string `json:"group_name"`
		Reason    string `json:"reason"`
		Status    string `json:"status"`
		Result    string `json:"result"`
		HandledAt *int64 `json:"handled_at"`
		CreatedAt int64  `json:"created_at"`
	}
	out := make([]item, 0, len(reports))
	for _, it := range reports {
		out = append(out, item{
			ID:        it.ID,
			GroupID:   it.GroupID,
			GroupName: it.GroupName,
			Reason:    it.Reason,
			Status:    it.Status,
			Result:    it.Result,
			HandledAt: unixPtr(it.HandledAt),
			CreatedAt: it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (a *API) handleAllUserReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reports, err := a.reportStore.ListRecentUserReports(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	type item struct {
		ID          string `json:"id"`
		ReporterUID string `json:"reporter_uid"`
		TargetUID   string `json:"target_uid"`
		Reason      string `json:"reason"`
		Status      string `json:"status"`
		Result      string `json:"result"`
		HandledAt   *int64 `json:"handled_at"`
		CreatedAt   int64  `json:"created_at"`
	}
	out := make([]item, 0, len(reports))
	for _, it := range reports {
		out = append(out, item{
			ID:          it.ID,
			ReporterUID: it.ReporterUID,
			TargetUID:   it.TargetUID,
			Reason:      it.Reason,
			Status:      it.Status,
			Result:      it.Result,
			HandledAt:   unixPtr(it.HandledAt),
			CreatedAt:   it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (a *API) handleAllGroupReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reports, err := a.groupReportStore.ListRecent(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	type item struct {
		ID          string `json:"id"`
		ReporterUID string `json:"reporter_uid"`
		GroupID     string `json:"group_id"`
		GroupName   string `json:"group_name"`
		Reason      string `json:"reason"`
		Status      string `json:"status"`
		Result      string `json:"result"`
		HandledAt   *int64 `json:"handled_at"`
		CreatedAt   int64  `json:"created_at"`
	}
	out := make([]item, 0, len(reports))
	for _, it := range reports {
		out = append(out, item{
			ID:          it.ID,
			ReporterUID: it.ReporterUID,
			GroupID:     it.GroupID,
			GroupName:   it.GroupName,
			Reason:      it.Reason,
			Status:      it.Status,
			Result:      it.Result,
			HandledAt:   unixPtr(it.HandledAt),
			CreatedAt:   it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (a *API) handleMeResourceReports(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reports, err := a.resourceReports.ListByReporter(ctx, claims.Subject, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	type item struct {
		ID          string `json:"id"`
		ItemID      string `json:"item_id"`
		ItemName    string `json:"item_name"`
		SectionName string `json:"section_name"`
		Reason      string `json:"reason"`
		Status      string `json:"status"`
		Result      string `json:"result"`
		HandledAt   *int64 `json:"handled_at"`
		CreatedAt   int64  `json:"created_at"`
	}
	out := make([]item, 0, len(reports))
	for _, it := range reports {
		out = append(out, item{
			ID:          it.ID,
			ItemID:      it.ItemID,
			ItemName:    it.ItemName,
			SectionName: it.SectionName,
			Reason:      it.Reason,
			Status:      it.Status,
			Result:      it.Result,
			HandledAt:   unixPtr(it.HandledAt),
			CreatedAt:   it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (a *API) handleAllResourceReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	reports, err := a.resourceReports.ListRecent(ctx, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	type item struct {
		ID          string `json:"id"`
		ItemID      string `json:"item_id"`
		ItemName    string `json:"item_name"`
		SectionName string `json:"section_name"`
		ReporterUID string `json:"reporter_uid"`
		Reason      string `json:"reason"`
		Status      string `json:"status"`
		Result      string `json:"result"`
		HandledAt   *int64 `json:"handled_at"`
		CreatedAt   int64  `json:"created_at"`
	}
	out := make([]item, 0, len(reports))
	for _, it := range reports {
		out = append(out, item{
			ID:          it.ID,
			ItemID:      it.ItemID,
			ItemName:    it.ItemName,
			SectionName: it.SectionName,
			ReporterUID: it.ReporterUID,
			Reason:      it.Reason,
			Status:      it.Status,
			Result:      it.Result,
			HandledAt:   unixPtr(it.HandledAt),
			CreatedAt:   it.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}
