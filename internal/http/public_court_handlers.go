package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"metrochat/internal/data"
)

type publicCourtCaseItem struct {
	ID              string `json:"id"`
	ReporterUID     string `json:"reporter_uid"`
	ReporterName    string `json:"reporter_name"`
	ReporterAvatar  string `json:"reporter_avatar"`
	DefendantUID    string `json:"defendant_uid"`
	DefendantName   string `json:"defendant_name"`
	DefendantAvatar string `json:"defendant_avatar"`
	ReportReason    string `json:"report_reason"`
	ReportEvidence  string `json:"report_evidence"`
	DefenseReason   string `json:"defense_reason"`
	DefenseEvidence string `json:"defense_evidence"`
	Status          string `json:"status"`
	Verdict         string `json:"verdict"`
	AdminNote       string `json:"admin_note"`
	BanHours        int    `json:"ban_hours"`
	BanVoteCount    int    `json:"ban_vote_count"`
	KeepVoteCount   int    `json:"keep_vote_count"`
	TotalVoteCount  int    `json:"total_vote_count"`
	MyVote          string `json:"my_vote"`
	MyVoteReason    string `json:"my_vote_reason"`
	ClosedAt        int64  `json:"closed_at"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

type publicCourtVoteItem struct {
	VoterUID    string `json:"voter_uid"`
	VoterName   string `json:"voter_name"`
	VoterAvatar string `json:"voter_avatar"`
	Vote        string `json:"vote"`
	Reason      string `json:"reason"`
	Evidence    string `json:"evidence"`
	CreatedAt   int64  `json:"created_at"`
}

type publicCourtStatementItem struct {
	UserUID    string `json:"user_uid"`
	UserName   string `json:"user_name"`
	UserAvatar string `json:"user_avatar"`
	Role       string `json:"role"`
	Reason     string `json:"reason"`
	Evidence   string `json:"evidence"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

type publicCourtDiscussionItem struct {
	ID         string `json:"id"`
	UserUID    string `json:"user_uid"`
	UserName   string `json:"user_name"`
	UserAvatar string `json:"user_avatar"`
	Body       string `json:"body"`
	CreatedAt  int64  `json:"created_at"`
}

type publicCourtMergedReportItem struct {
	ReportID       string `json:"report_id"`
	ReporterUID    string `json:"reporter_uid"`
	ReporterName   string `json:"reporter_name"`
	ReporterAvatar string `json:"reporter_avatar"`
	Reason         string `json:"reason"`
	CreatedAt      int64  `json:"created_at"`
}

func parsePublicCourtReportPage(raw string) int {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func parsePublicCourtReportPageSize(raw string) int {
	size, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || size < 1 {
		return 5
	}
	if size > 20 {
		return 20
	}
	return size
}

func (a *API) handlePublicCourtCases(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := parseLimit(r.URL.Query().Get("limit"))
	before := parseBefore(r.URL.Query().Get("before"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	items, err := a.publicCourt.ListCases(ctx, status, limit, before, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	resp := make([]publicCourtCaseItem, 0, len(items))
	for _, item := range items {
		resp = append(resp, toPublicCourtCaseItem(item))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cases": resp,
	})
}

func (a *API) handlePublicCourtCaseDetail(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	caseID := strings.TrimSpace(chi.URLParam(r, "caseID"))
	if caseID == "" {
		writeError(w, http.StatusBadRequest, "missing_case", "case id is required")
		return
	}
	reportPage := parsePublicCourtReportPage(r.URL.Query().Get("report_page"))
	reportPageSize := parsePublicCourtReportPageSize(r.URL.Query().Get("report_page_size"))

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	item, err := a.publicCourt.GetCaseSummary(ctx, caseID, claims.Subject)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "case not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	statements, err := a.publicCourt.ListStatements(ctx, caseID, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	discussions, err := a.publicCourt.ListDiscussions(ctx, caseID, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	mergedReports, mergedTotal, err := a.publicCourt.ListMergedReports(ctx, caseID, reportPage, reportPageSize)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "case not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	mergedItems := make([]publicCourtMergedReportItem, 0, len(mergedReports))
	for _, item := range mergedReports {
		mergedItems = append(mergedItems, publicCourtMergedReportItem{
			ReportID:       item.ReportID,
			ReporterUID:    item.ReporterUID,
			ReporterName:   item.ReporterName,
			ReporterAvatar: item.ReporterAvatar,
			Reason:         item.Reason,
			CreatedAt:      item.CreatedAt.Unix(),
		})
	}
	effectivePage := reportPage
	if mergedTotal <= 0 {
		effectivePage = 1
	} else {
		totalPages := (mergedTotal + reportPageSize - 1) / reportPageSize
		if totalPages < 1 {
			totalPages = 1
		}
		if effectivePage > totalPages {
			effectivePage = totalPages
		}
		if effectivePage < 1 {
			effectivePage = 1
		}
	}
	statementItems := toPublicCourtStatements(*item, statements)
	discussionItems := toPublicCourtDiscussions(discussions)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"case":                    toPublicCourtCaseItem(*item),
		"statements":              statementItems,
		"discussions":             discussionItems,
		"merged_reports":          mergedItems,
		"merged_report_total":     mergedTotal,
		"merged_report_page":      effectivePage,
		"merged_report_page_size": reportPageSize,
	})
}

func (a *API) handlePublicCourtCaseVotes(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	caseID := strings.TrimSpace(chi.URLParam(r, "caseID"))
	if caseID == "" {
		writeError(w, http.StatusBadRequest, "missing_case", "case id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	votes, err := a.publicCourt.ListRecentVotes(ctx, caseID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	resp := make([]publicCourtVoteItem, 0, len(votes))
	for _, item := range votes {
		resp = append(resp, publicCourtVoteItem{
			VoterUID:    item.VoterUID,
			VoterName:   item.VoterName,
			VoterAvatar: item.VoterAvatar,
			Vote:        item.Vote,
			Reason:      item.Reason,
			Evidence:    item.Evidence,
			CreatedAt:   item.CreatedAt.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"votes": resp,
	})
}

func (a *API) handlePublicCourtCaseVote(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	caseID := strings.TrimSpace(chi.URLParam(r, "caseID"))
	if caseID == "" {
		writeError(w, http.StatusBadRequest, "missing_case", "case id is required")
		return
	}
	var body struct {
		Vote     string `json:"vote"`
		Reason   string `json:"reason"`
		Evidence string `json:"evidence"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, err := a.publicCourt.CastVote(ctx, caseID, claims.Subject, body.Vote, body.Reason, body.Evidence)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "case not found")
		case errors.Is(err, data.ErrPublicCourtClosed):
			writeError(w, http.StatusConflict, "case_closed", "case closed")
		case errors.Is(err, data.ErrPublicCourtForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "cannot vote this case")
		case errors.Is(err, data.ErrPublicCourtInvalidVote):
			writeError(w, http.StatusBadRequest, "invalid_vote", "invalid vote")
		case errors.Is(err, data.ErrPublicCourtVoteEvidenceRequired):
			writeError(w, http.StatusBadRequest, "invalid_evidence", "evidence is required")
		case errors.Is(err, data.ErrPublicCourtReputationTooLow):
			writeError(w, http.StatusForbidden, "reputation_too_low", "reputation score must be greater than 50")
		default:
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		}
		return
	}
	if result != nil && result.LockedForReview && result.JuryVerdict == "ban" && result.TemporaryBanHours > 0 && result.DefendantID != "" {
		_ = a.devices.BanUser(ctx, result.DefendantID, "公开法庭初审临时封禁", result.TemporaryBanHours)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":             "vote saved",
		"case_status":         result.CaseStatus,
		"ban_vote_count":      result.BanVoteCount,
		"keep_vote_count":     result.KeepVoteCount,
		"total_vote_count":    result.TotalVoteCount,
		"locked_for_review":   result.LockedForReview,
		"jury_verdict":        result.JuryVerdict,
		"temporary_ban_hours": result.TemporaryBanHours,
	})
}

func (a *API) handlePublicCourtCaseStatement(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	caseID := strings.TrimSpace(chi.URLParam(r, "caseID"))
	if caseID == "" {
		writeError(w, http.StatusBadRequest, "missing_case", "case id is required")
		return
	}
	var body struct {
		Reason   string `json:"reason"`
		Evidence string `json:"evidence"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	role, err := a.publicCourt.SaveStatement(ctx, caseID, claims.Subject, body.Reason, body.Evidence)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "case not found")
		case errors.Is(err, data.ErrPublicCourtClosed):
			writeError(w, http.StatusConflict, "case_closed", "case closed")
		case errors.Is(err, data.ErrPublicCourtForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "not participant")
		case errors.Is(err, data.ErrPublicCourtInvalidStatement):
			writeError(w, http.StatusBadRequest, "invalid_statement", "reason or evidence required")
		default:
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "statement saved",
		"role":    role,
	})
}

func (a *API) handlePublicCourtCaseDiscussions(w http.ResponseWriter, r *http.Request) {
	if _, ok := claimsFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	caseID := strings.TrimSpace(chi.URLParam(r, "caseID"))
	if caseID == "" {
		writeError(w, http.StatusBadRequest, "missing_case", "case id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	rows, err := a.publicCourt.ListDiscussions(ctx, caseID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"discussions": toPublicCourtDiscussions(rows),
	})
}

func (a *API) handlePublicCourtCaseDiscussion(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	caseID := strings.TrimSpace(chi.URLParam(r, "caseID"))
	if caseID == "" {
		writeError(w, http.StatusBadRequest, "missing_case", "case id is required")
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	row, err := a.publicCourt.AddDiscussion(ctx, caseID, claims.Subject, body.Body)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "case not found")
		case errors.Is(err, data.ErrPublicCourtClosed):
			writeError(w, http.StatusConflict, "case_closed", "case closed")
		case errors.Is(err, data.ErrPublicCourtInvalidDiscussion):
			writeError(w, http.StatusBadRequest, "invalid_discussion", "discussion body is required")
		default:
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		}
		return
	}
	if row == nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "discussion saved",
		"discussion": publicCourtDiscussionItem{
			ID:         row.ID,
			UserUID:    row.UserUID,
			UserName:   row.UserName,
			UserAvatar: row.UserAvatar,
			Body:       row.Body,
			CreatedAt:  row.CreatedAt.Unix(),
		},
	})
}

func (a *API) handlePublicCourtCaseWithdraw(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	if !requireJSON(w, r) {
		return
	}
	caseID := strings.TrimSpace(chi.URLParam(r, "caseID"))
	if caseID == "" {
		writeError(w, http.StatusBadRequest, "missing_case", "case id is required")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	updated, err := a.publicCourt.WithdrawCase(ctx, caseID, claims.Subject, body.Reason)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "case not found")
		case errors.Is(err, data.ErrPublicCourtForbidden):
			writeError(w, http.StatusForbidden, "forbidden", "only reporter can withdraw")
		case errors.Is(err, data.ErrPublicCourtClosed):
			writeError(w, http.StatusConflict, "case_closed", "case closed")
		default:
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "withdrawn",
		"status":  updated.Status,
	})
}

func (a *API) handleAdminPublicCourtClose(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	caseID := strings.TrimSpace(r.FormValue("case_id"))
	if caseID == "" {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("缺少案件ID")+"#reports", http.StatusSeeOther)
		return
	}
	verdict := strings.TrimSpace(r.FormValue("verdict"))
	adminNote := strings.TrimSpace(r.FormValue("admin_note"))
	banHours, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("ban_hours")))
	if banHours < 0 {
		banHours = 0
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	beforeCase, beforeErr := a.publicCourt.GetByID(ctx, caseID)
	if beforeErr != nil || beforeCase == nil {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("案件不存在")+"#reports", http.StatusSeeOther)
		return
	}
	if beforeCase.Status != "pending_review" {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("案件尚未进入待二审状态")+"#reports", http.StatusSeeOther)
		return
	}
	closedCase, err := a.publicCourt.FinalizeCase(ctx, caseID, verdict, adminNote, banHours)
	if err != nil {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("二审失败: "+err.Error())+"#reports", http.StatusSeeOther)
		return
	}
	if closedCase != nil && closedCase.Verdict == "ban" {
		if closedCase.BanHours < 0 {
			closedCase.BanHours = 0
		}
		_ = a.devices.BanUser(ctx, closedCase.DefendantID, "公开法庭二审裁决", closedCase.BanHours)
	}
	if closedCase != nil && closedCase.Verdict == "keep" && beforeCase.Verdict == "ban" && beforeCase.BanHours > 0 {
		if a.shouldReleasePublicCourtTempBan(ctx, closedCase.DefendantID, closedCase.ID) {
			_ = a.devices.UnbanUser(ctx, closedCase.DefendantID)
		}
	}
	http.Redirect(w, r, "/admins?ok="+url.QueryEscape("公开法庭二审完成")+"#reports", http.StatusSeeOther)
}

func (a *API) shouldReleasePublicCourtTempBan(ctx context.Context, userID string, closedCaseID string) bool {
	if strings.TrimSpace(userID) == "" {
		return false
	}
	activeCases, err := a.publicCourt.ListActiveByDefendant(ctx, userID, 20)
	if err != nil {
		return false
	}
	for _, item := range activeCases {
		if item.ID == closedCaseID {
			continue
		}
		if item.Status == "pending_review" && item.Verdict == "ban" && item.BanHours > 0 {
			return false
		}
	}
	bannedRow, err := a.devices.GetBannedUserByUserID(ctx, userID)
	if err != nil || bannedRow == nil {
		return false
	}
	reason := strings.TrimSpace(bannedRow.Reason)
	return reason == "公开法庭初审临时封禁"
}

func (a *API) handleAdminPublicCourtClear(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	confirm := strings.TrimSpace(r.FormValue("confirm"))
	if confirm != "1" {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("请确认后再执行")+"#reports", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	count, err := a.publicCourt.ClearAllCases(ctx)
	if err != nil {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("清空公开法庭案件失败: "+err.Error())+"#reports", http.StatusSeeOther)
		return
	}
	msg := "已清空公开法庭案件，共 " + strconv.FormatInt(count, 10) + " 条"
	http.Redirect(w, r, "/admins?ok="+url.QueryEscape(msg)+"#reports", http.StatusSeeOther)
}

func toPublicCourtCaseItem(item data.PublicCourtCaseSummary) publicCourtCaseItem {
	closedAt := int64(0)
	if item.ClosedAt.Valid {
		closedAt = item.ClosedAt.Time.Unix()
	}
	return publicCourtCaseItem{
		ID:              item.ID,
		ReporterUID:     item.ReporterUID,
		ReporterName:    item.ReporterName,
		ReporterAvatar:  item.ReporterAvatar,
		DefendantUID:    item.DefendantUID,
		DefendantName:   item.DefendantName,
		DefendantAvatar: item.DefendantAvatar,
		ReportReason:    item.ReportReason,
		ReportEvidence:  item.ReportEvidence,
		DefenseReason:   item.DefenseReason,
		DefenseEvidence: item.DefenseEvidence,
		Status:          item.Status,
		Verdict:         item.Verdict,
		AdminNote:       item.AdminNote,
		BanHours:        item.BanHours,
		BanVoteCount:    item.BanVoteCount,
		KeepVoteCount:   item.KeepVoteCount,
		TotalVoteCount:  item.TotalVoteCount,
		MyVote:          item.MyVote,
		MyVoteReason:    item.MyVoteReason,
		ClosedAt:        closedAt,
		CreatedAt:       item.CreatedAt.Unix(),
		UpdatedAt:       item.UpdatedAt.Unix(),
	}
}

func toPublicCourtDiscussions(rows []data.PublicCourtDiscussion) []publicCourtDiscussionItem {
	out := make([]publicCourtDiscussionItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, publicCourtDiscussionItem{
			ID:         row.ID,
			UserUID:    row.UserUID,
			UserName:   row.UserName,
			UserAvatar: row.UserAvatar,
			Body:       row.Body,
			CreatedAt:  row.CreatedAt.Unix(),
		})
	}
	return out
}

func toPublicCourtStatements(item data.PublicCourtCaseSummary, rows []data.PublicCourtStatement) []publicCourtStatementItem {
	out := make([]publicCourtStatementItem, 0, len(rows)+2)
	seenUID := make(map[string]struct{})

	for _, row := range rows {
		uid := strings.TrimSpace(row.UserUID)
		if uid != "" {
			seenUID[uid] = struct{}{}
		}
		out = append(out, publicCourtStatementItem{
			UserUID:    row.UserUID,
			UserName:   row.UserName,
			UserAvatar: row.UserAvatar,
			Role:       row.Role,
			Reason:     row.Reason,
			Evidence:   row.Evidence,
			CreatedAt:  row.CreatedAt.Unix(),
			UpdatedAt:  row.UpdatedAt.Unix(),
		})
	}

	if strings.TrimSpace(item.ReportReason) != "" || strings.TrimSpace(item.ReportEvidence) != "" {
		if _, ok := seenUID[item.ReporterUID]; !ok {
			out = append(out, publicCourtStatementItem{
				UserUID:    item.ReporterUID,
				UserName:   item.ReporterName,
				UserAvatar: item.ReporterAvatar,
				Role:       "reporter",
				Reason:     item.ReportReason,
				Evidence:   item.ReportEvidence,
				CreatedAt:  item.CreatedAt.Unix(),
				UpdatedAt:  item.UpdatedAt.Unix(),
			})
		}
	}
	if strings.TrimSpace(item.DefenseReason) != "" || strings.TrimSpace(item.DefenseEvidence) != "" {
		if _, ok := seenUID[item.DefendantUID]; !ok {
			out = append(out, publicCourtStatementItem{
				UserUID:    item.DefendantUID,
				UserName:   item.DefendantName,
				UserAvatar: item.DefendantAvatar,
				Role:       "defendant",
				Reason:     item.DefenseReason,
				Evidence:   item.DefenseEvidence,
				CreatedAt:  item.CreatedAt.Unix(),
				UpdatedAt:  item.UpdatedAt.Unix(),
			})
		}
	}

	return out
}
