package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"metrochat/internal/config"
	"metrochat/internal/data"
)

type adminStats struct {
	TotalUsers           int
	BannedUserCount      int
	BannedDeviceCount    int
	TotalReports         int
	PendingBanAppeals    int
	TotalBugReports      int
	TotalResourceReports int
	ActiveMessageUsers   int
}

type adminPageData struct {
	Error                     string
	Success                   string
	Stats                     adminStats
	Users                     []data.UserAdminRow
	ActiveUsers               []data.UserActiveRow
	ActiveRange               string
	ActiveStart               string
	ActiveEnd                 string
	ActiveLimit               int
	RegistrationLimit         int
	Devices                   []data.DeviceRow
	BannedDevices             []data.BannedDeviceRow
	BannedUsers               []data.BannedUserRow
	Reports                   []data.UserReport
	BanAppeals                []data.BanAppeal
	PublicCourtCases          []data.PublicCourtCaseSummary
	PublicCourtPendingCases   []data.PublicCourtCaseSummary
	GroupReports              []data.GroupReport
	BugReports                []data.BugReport
	ResourceReports           []data.ResourceReportRow
	EmojiPlazaItems           []data.EmojiPlazaItem
	Notifications             []data.SystemNotification
	Groups                    []data.GroupAdminRow
	TitleCatalog              []data.TitleCatalogRow
	ServerLogs                []string
	ServerLogLimit            int
	ServerLogMax              int
	ServerStats               []string
	ResourceQuotaGB           string
	ResourceQuotaBytes        int64
	VideoEnabled              bool
	MediaRateMB               string
	UpdateRateMB              string
	VideoRateMB               string
	MusicRateMB               string
	MediaDownloadConcurrency  int
	UpdateDownloadConcurrency int
	VideoDownloadConcurrency  int
	MusicDownloadConcurrency  int
	PublicBaseURL             string
	DataServerBaseURL         string
	DataServerSyncToken       string
}

const adminCookie = "admin_session"
const adminSessionTTL = 12 * time.Hour

var adminLoginTpl = template.Must(template.New("admin_login").Parse(adminLoginHTML))
var adminDashboardTpl = template.Must(template.New("admin_dashboard").Parse(adminDashboardHTML))

func (a *API) handleAdminIndex(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		renderAdminLogin(w, "")
		return
	}
	successMsg := strings.TrimSpace(r.URL.Query().Get("ok"))
	errMsg := strings.TrimSpace(r.URL.Query().Get("err"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	userLimit := 1000
	if v := strings.TrimSpace(r.URL.Query().Get("users_limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			userLimit = n
		}
	}

	logLimit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("logs_limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= logLineLimit {
			logLimit = n
		}
	}
	serverLogs := tailLines(defaultMonitor.copyLogLines(), logLimit)
	serverStats := defaultMonitor.formatStats()

	activeLimit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("active_limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			activeLimit = n
		}
	}
	activeRange := strings.TrimSpace(r.URL.Query().Get("active_range"))
	activeStartParam := strings.TrimSpace(r.URL.Query().Get("active_start"))
	activeEndParam := strings.TrimSpace(r.URL.Query().Get("active_end"))
	now := time.Now()
	activeStart := time.Time{}
	activeEnd := time.Time{}
	if activeStartParam != "" || activeEndParam != "" {
		activeStart = parseAdminTime(activeStartParam, now.Add(-24*time.Hour))
		activeEnd = parseAdminTime(activeEndParam, now)
		activeRange = "custom"
	} else {
		switch activeRange {
		case "7d":
			activeStart = now.Add(-7 * 24 * time.Hour)
		case "30d":
			activeStart = now.Add(-30 * 24 * time.Hour)
		default:
			activeRange = "24h"
			activeStart = now.Add(-24 * time.Hour)
		}
		activeEnd = now
	}
	activeUsers, _ := a.users.ListActiveUsers(ctx, activeStart, activeEnd, activeLimit)

	users, _ := a.users.ListRecent(ctx, userLimit)
	devices, _ := a.devices.ListRecentDevices(ctx, 50)
	bannedDevices, _ := a.devices.ListBannedDevices(ctx, 50)
	bannedUsers, _ := a.devices.ListBannedUsers(ctx, 50)
	reports, _ := a.reportStore.ListRecentUserReports(ctx, 300)
	banAppeals, _ := a.banAppeals.ListRecent(ctx, "all", 300)
	pendingBanAppeals, pendingErr := a.banAppeals.CountPending(ctx)
	if pendingErr != nil {
		pendingBanAppeals = 0
		for i := 0; i < len(banAppeals); i++ {
			if strings.EqualFold(strings.TrimSpace(banAppeals[i].Status), "pending") {
				pendingBanAppeals++
			}
		}
	}
	publicCourtCases, _ := a.publicCourt.ListCases(ctx, "all", 500, time.Time{}, "")
	publicCourtPendingCases, _ := a.publicCourt.ListCases(ctx, "pending_review", 500, time.Time{}, "")
	groupReports, _ := a.groupReportStore.ListRecent(ctx, 50)
	bugReports, _ := a.bugReportStore.ListRecent(ctx, 50)
	resourceReports, _ := a.resourceReports.ListRecent(ctx, 50)
	emojiPlazaItems, _ := a.emojis.List(ctx, "", 300, 0)
	notifications, _ := a.notifications.List(ctx, 20, time.Time{})
	groups, _ := a.groups.ListAdmin(ctx, 1000)
	_ = a.titles.EnsureTable(ctx)
	titles, _ := a.titles.List(ctx, true)
	quotaBytes := a.resourceQuotaBytes()
	quotaGB := float64(quotaBytes) / float64(1024*1024*1024)
	mediaRate, updateRate, videoRate, musicRate := getTransferRateLimits()
	mediaDlConcurrency, updateDlConcurrency, videoDlConcurrency, musicDlConcurrency := getTransferConcurrencyLimits()

	totalUsers, err := a.users.Count(ctx)
	if err != nil {
		totalUsers = len(users)
	}
	stats := adminStats{
		TotalUsers:           totalUsers,
		BannedUserCount:      len(bannedUsers),
		BannedDeviceCount:    len(bannedDevices),
		TotalReports:         len(reports) + len(groupReports),
		PendingBanAppeals:    pendingBanAppeals,
		TotalBugReports:      len(bugReports),
		TotalResourceReports: len(resourceReports),
		ActiveMessageUsers:   len(activeUsers),
	}

	renderAdminDashboard(w, adminPageData{
		Error:                     errMsg,
		Success:                   successMsg,
		Stats:                     stats,
		Users:                     users,
		ActiveUsers:               activeUsers,
		ActiveRange:               activeRange,
		ActiveStart:               activeStart.Format("2006-01-02T15:04"),
		ActiveEnd:                 activeEnd.Format("2006-01-02T15:04"),
		ActiveLimit:               activeLimit,
		RegistrationLimit:         a.cfg.RegistrationLimit,
		Devices:                   devices,
		BannedDevices:             bannedDevices,
		BannedUsers:               bannedUsers,
		Reports:                   reports,
		BanAppeals:                banAppeals,
		PublicCourtCases:          publicCourtCases,
		PublicCourtPendingCases:   publicCourtPendingCases,
		GroupReports:              groupReports,
		BugReports:                bugReports,
		ResourceReports:           resourceReports,
		EmojiPlazaItems:           emojiPlazaItems,
		Notifications:             notifications,
		Groups:                    groups,
		TitleCatalog:              titles,
		ServerLogs:                serverLogs,
		ServerLogLimit:            logLimit,
		ServerLogMax:              logLineLimit,
		ServerStats:               serverStats,
		ResourceQuotaGB:           fmt.Sprintf("%.2f", quotaGB),
		ResourceQuotaBytes:        quotaBytes,
		VideoEnabled:              a.cfg.VideoEnabled,
		MediaRateMB:               rateBytesToMBpsString(mediaRate),
		UpdateRateMB:              rateBytesToMBpsString(updateRate),
		VideoRateMB:               rateBytesToMBpsString(videoRate),
		MusicRateMB:               rateBytesToMBpsString(musicRate),
		MediaDownloadConcurrency:  mediaDlConcurrency,
		UpdateDownloadConcurrency: updateDlConcurrency,
		VideoDownloadConcurrency:  videoDlConcurrency,
		MusicDownloadConcurrency:  musicDlConcurrency,
		PublicBaseURL:             a.cfg.PublicBaseURL,
		DataServerBaseURL:         a.cfg.DataServerBaseURL,
		DataServerSyncToken:       a.cfg.DataServerSyncToken,
	})
}

func (a *API) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !a.ipLimiter.Allow("admin_login:" + clientIP(r)) {
		renderAdminLogin(w, "请求过于频繁，请稍后再试")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		renderAdminLogin(w, "登录失败")
		return
	}
	user := strings.TrimSpace(r.FormValue("username"))
	pass := strings.TrimSpace(r.FormValue("password"))
	if !secureEqual(user, a.cfg.AdminUser) || !secureEqual(pass, a.cfg.AdminPassword) {
		renderAdminLogin(w, "账号或密码错误")
		return
	}
	token := a.adminSessions.New(adminSessionTTL)
	secure := isHTTPS(r)
	exp := time.Now().Add(adminSessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    token,
		Path:     "/admins",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(adminSessionTTL.Seconds()),
		Expires:  exp,
	})
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	if cookie, err := r.Cookie(adminCookie); err == nil {
		a.adminSessions.Delete(cookie.Value)
	}
	secure := isHTTPS(r)
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    "",
		Path:     "/admins",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
	})
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminBanDevice(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	deviceID := strings.TrimSpace(r.FormValue("device_id"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	duration := parseDurationHours(r.FormValue("duration_hours"))
	_ = a.devices.BanDevice(r.Context(), deviceID, reason, duration)
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminUnbanDevice(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	deviceID := strings.TrimSpace(r.FormValue("device_id"))
	_ = a.devices.UnbanDevice(r.Context(), deviceID)
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminBanUser(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	userID := strings.TrimSpace(r.FormValue("user_id"))
	userUID := strings.TrimSpace(r.FormValue("user_uid"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	duration := parseDurationHours(r.FormValue("duration_hours"))
	if userID == "" && userUID != "" {
		if u, err := a.users.GetByUID(r.Context(), userUID); err == nil {
			userID = u.ID
		}
	}
	_ = a.devices.BanUser(r.Context(), userID, reason, duration)
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminUnbanUser(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	userID := strings.TrimSpace(r.FormValue("user_id"))
	userUID := strings.TrimSpace(r.FormValue("user_uid"))
	if userID == "" && userUID != "" {
		if u, err := a.users.GetByUID(r.Context(), userUID); err == nil {
			userID = u.ID
		}
	}
	_ = a.devices.UnbanUser(r.Context(), userID)
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminDeactivateUser(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	userID := strings.TrimSpace(r.FormValue("user_id"))
	userUID := strings.ToUpper(strings.TrimSpace(r.FormValue("user_uid")))
	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" {
		reason = "管理员停用"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if userID == "" && userUID != "" {
		if u, err := a.users.GetByUID(ctx, userUID); err == nil {
			userID = u.ID
			userUID = u.UID
		}
	}
	if userID == "" || strings.EqualFold(userID, data.SystemNotificationUID) || strings.EqualFold(userUID, data.SystemNotificationUID) {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	_ = a.refresh.RevokeAllByUser(ctx, userID)
	_, _ = a.users.IncrementTokenVersion(ctx, userID)
	a.clearTokenVersionCache(userID)
	_ = a.devices.BanUser(ctx, userID, reason, 0)
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	userID := strings.TrimSpace(r.FormValue("user_id"))
	userUID := strings.ToUpper(strings.TrimSpace(r.FormValue("user_uid")))
	confirm := strings.TrimSpace(r.FormValue("confirm"))
	if confirm != "1" {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if userID == "" && userUID != "" {
		if u, err := a.users.GetByUID(ctx, userUID); err == nil {
			userID = u.ID
			userUID = u.UID
		}
	}
	if userID == "" || strings.EqualFold(userID, data.SystemNotificationUID) || strings.EqualFold(userUID, data.SystemNotificationUID) {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	_ = a.refresh.RevokeAllByUser(ctx, userID)
	a.clearTokenVersionCache(userID)
	_ = a.users.DeleteByID(ctx, userID)
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) handleAdminBanGroup(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	groupID := strings.ToUpper(strings.TrimSpace(r.FormValue("group_id")))
	reason := strings.TrimSpace(r.FormValue("reason"))
	duration := parseDurationHours(r.FormValue("duration_hours"))
	_ = a.groupBans.Ban(r.Context(), groupID, reason, duration)
	http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
}

func (a *API) handleAdminUnbanGroup(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	groupID := strings.ToUpper(strings.TrimSpace(r.FormValue("group_id")))
	_ = a.groupBans.Unban(r.Context(), groupID)
	http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
}

func (a *API) handleAdminDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	groupID := strings.ToUpper(strings.TrimSpace(r.FormValue("group_id")))
	if groupID != "" {
		_ = a.groups.Delete(r.Context(), groupID)
		_ = a.groupBans.Unban(r.Context(), groupID)
	}
	http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
}

func (a *API) handleAdminSetGroupOwner(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	groupID := strings.ToUpper(strings.TrimSpace(r.FormValue("group_id")))
	ownerUID := strings.ToUpper(strings.TrimSpace(r.FormValue("owner_uid")))
	if !isValidGroupID(groupID) || !isValidUID(ownerUID) {
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	ownerUser, err := a.users.GetByUID(ctx, ownerUID)
	if err != nil || ownerUser == nil {
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}

	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}

	var currentOwnerID string
	if err = tx.GetContext(ctx, &currentOwnerID, `SELECT owner_id FROM groups WHERE id = $1 LIMIT 1`, groupID); err != nil {
		_ = tx.Rollback()
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}
	if currentOwnerID == ownerUser.ID {
		_ = tx.Rollback()
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO group_members (group_id, user_id, role, joined_at, last_read_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING
`, groupID, ownerUser.ID, data.GroupRoleMember)
	if err != nil {
		_ = tx.Rollback()
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}

	_, err = tx.ExecContext(ctx, `
UPDATE group_members
SET role = $1
WHERE group_id = $2 AND user_id = $3
`, data.GroupRoleOwner, groupID, ownerUser.ID)
	if err != nil {
		_ = tx.Rollback()
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}

	if currentOwnerID != "" {
		_, _ = tx.ExecContext(ctx, `
UPDATE group_members
SET role = $1
WHERE group_id = $2 AND user_id = $3 AND user_id <> $4
`, data.GroupRoleAdmin, groupID, currentOwnerID, ownerUser.ID)
	}

	_, err = tx.ExecContext(ctx, `
UPDATE groups
SET owner_id = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, ownerUser.ID, groupID)
	if err != nil {
		_ = tx.Rollback()
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}

	if err = tx.Commit(); err != nil {
		http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admins#groups", http.StatusSeeOther)
}

func (a *API) handleAdminUserTitle(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	userUID := strings.ToUpper(strings.TrimSpace(r.FormValue("user_uid")))
	title := sanitizeUserTitle(r.FormValue("user_title"))
	if userUID == "" || !isValidUID(userUID) {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByUID(ctx, userUID)
	if err != nil || user == nil {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	price := 0
	var newTitleRow *data.TitleCatalogRow
	if title != "" {
		price = 100
		if row, err := a.titles.GetByTitle(ctx, title); err == nil && row != nil {
			newTitleRow = row
			if row.Price > 0 {
				price = row.Price
			}
		}
	}
	oldTitle := strings.TrimSpace(user.UserTitle)
	if oldTitle != "" {
		var oldRow struct {
			ID       string `db:"id"`
			IsCustom int    `db:"is_custom"`
		}
		err = a.db.GetContext(ctx, &oldRow, `SELECT id, is_custom FROM title_catalog WHERE title = $1`, oldTitle)
		if err == nil && oldRow.ID != "" {
			if oldRow.IsCustom != 0 {
				_, _ = a.db.ExecContext(ctx, `
UPDATE title_catalog
SET owner_id = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND (owner_id = $2 OR owner_id IS NULL)
`, oldRow.ID, user.ID)
			} else {
				_, _ = a.db.ExecContext(ctx, `
UPDATE title_catalog
SET owner_id = NULL, active = 1, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND (owner_id = $2 OR owner_id IS NULL)
`, oldRow.ID, user.ID)
			}
		}
	}
	if newTitleRow != nil {
		if newTitleRow.OwnerID.Valid && strings.TrimSpace(newTitleRow.OwnerID.String) != "" && newTitleRow.OwnerID.String != user.ID {
			_, _ = a.db.ExecContext(ctx, `
UPDATE users
SET user_title = '', user_title_price = 0, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND user_title = $2
`, newTitleRow.OwnerID.String, newTitleRow.Title)
		}
		_, _ = a.db.ExecContext(ctx, `
UPDATE title_catalog
SET owner_id = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, user.ID, newTitleRow.ID)
	} else if title != "" {
		_, _ = a.db.ExecContext(ctx, `
INSERT INTO title_catalog (id, title, price, active, is_custom, owner_id, created_at, updated_at)
VALUES ($1, $2, $3, 1, 1, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
`, data.NewID(), title, price, user.ID)
	}
	_ = a.users.UpdateTitleWithPriceByUID(ctx, userUID, title, price)
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminUserCoin(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	userUID := strings.ToUpper(strings.TrimSpace(r.FormValue("user_uid")))
	if userUID == "" || !isValidUID(userUID) {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	balanceStr := strings.TrimSpace(r.FormValue("coin_balance"))
	if balanceStr == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	balance, err := strconv.Atoi(balanceStr)
	if err != nil {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	if balance < 0 {
		balance = 0
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_ = a.users.UpdateCoinByUID(ctx, userUID, balance)
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminUserUID(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	userUID := strings.ToUpper(strings.TrimSpace(r.FormValue("user_uid")))
	newUID := strings.ToUpper(strings.TrimSpace(r.FormValue("new_uid")))
	if userUID == "" || newUID == "" || !isValidUID(newUID) {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByUID(ctx, userUID)
	if err != nil || user == nil {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	if user.UID == newUID {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	if err := a.users.UpdateUIDForce(ctx, user.ID, newUID); err != nil {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	_, _ = a.users.IncrementTokenVersion(ctx, user.ID)
	a.tokenVersionMu.Lock()
	delete(a.tokenVersionCache, user.ID)
	a.tokenVersionMu.Unlock()
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) handleAdminCreateTestUser(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	req := directCreateUserRequest{
		Username:    strings.TrimSpace(r.FormValue("username")),
		Password:    strings.TrimSpace(r.FormValue("password")),
		UID:         strings.ToUpper(strings.TrimSpace(r.FormValue("uid"))),
		Email:       strings.TrimSpace(r.FormValue("email")),
		DisplayName: strings.TrimSpace(r.FormValue("display_name")),
	}
	if coinRaw := strings.TrimSpace(r.FormValue("coin_balance")); coinRaw != "" {
		if n, err := strconv.Atoi(coinRaw); err == nil {
			req.CoinBalance = n
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, status, code, message := a.createDirectUserForTest(ctx, req)
	if status != 0 {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape(code+": "+message)+"#users", http.StatusSeeOther)
		return
	}
	info := "创建成功 UID=" + result.User.UID + " 用户名=" + result.User.Username + " 密码=" + result.Password
	http.Redirect(w, r, "/admins?ok="+url.QueryEscape(info)+"#users", http.StatusSeeOther)
}

func (a *API) handleAdminTitleCreate(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	title := sanitizeUserTitle(r.FormValue("title"))
	price := parsePositiveInt(r.FormValue("price"), 100)
	if title == "" {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_ = a.titles.EnsureTable(ctx)
	if err := a.titles.Create(ctx, title, price); err != nil {
		_ = a.titles.UpdateByTitle(ctx, title, price)
	}
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) handleAdminTitleUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	title := sanitizeUserTitle(r.FormValue("title"))
	price := parsePositiveInt(r.FormValue("price"), 100)
	if id == "" || title == "" {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_ = a.titles.Update(ctx, id, title, price)
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) handleAdminTitleToggle(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	active := strings.TrimSpace(r.FormValue("active"))
	if id == "" {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	enabled := active == "1" || strings.EqualFold(active, "true")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_ = a.titles.SetActive(ctx, id, enabled)
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) handleAdminTitleDelete(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_ = a.titles.Delete(ctx, id)
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) ensureAdminPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	if !a.isAdmin(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	return true
}

func parsePositiveInt(value string, fallback int) int {
	v := strings.TrimSpace(value)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parseDurationHours(value string) int {
	if value == "" {
		return 0
	}
	v := strings.TrimSpace(value)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func normalizeAdminReportStatus(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "pending", "open", "todo":
		return "pending"
	case "accepted", "accept", "approved", "approve", "pass", "resolved", "done":
		return "accepted"
	case "rejected", "reject", "deny", "denied", "declined":
		return "rejected"
	default:
		return "pending"
	}
}

func parseAdminTime(value string, fallback time.Time) time.Time {
	v := strings.TrimSpace(value)
	if v == "" {
		return fallback
	}
	layouts := []string{
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t
		}
	}
	return fallback
}

func sanitizeUserTitle(value string) string {
	title := strings.TrimSpace(value)
	if title == "" {
		return ""
	}
	title = strings.ReplaceAll(title, "\r", " ")
	title = strings.ReplaceAll(title, "\n", " ")
	if utf8.RuneCountInString(title) > 20 {
		runes := []rune(title)
		title = string(runes[:20])
	}
	return title
}

func (a *API) isAdmin(r *http.Request) bool {
	if r == nil {
		return false
	}
	cookie, err := r.Cookie(adminCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	a.adminSessions.PruneExpired()
	return a.adminSessions.Valid(cookie.Value)
}

func (a *API) handleAdminBugReports(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	reports, err := a.bugReportStore.ListRecent(ctx, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reports)
}

func (a *API) handleAdminBugReportDelete(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	_ = a.bugReportStore.Delete(r.Context(), id)
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminBugReportStatus(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	status := strings.TrimSpace(r.FormValue("status"))
	note := strings.TrimSpace(r.FormValue("note"))
	if id == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	_ = a.bugReportStore.SetStatus(r.Context(), id, status, note)
	http.Redirect(w, r, "/admins#bugs", http.StatusSeeOther)
}

func (a *API) handleAdminServerLog(w http.ResponseWriter, r *http.Request) {
	if !a.isAdmin(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	limit := 200
	if v := strings.TrimSpace(r.URL.Query().Get("logs_limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= logLineLimit {
			limit = n
		}
	}
	logs := tailLines(defaultMonitor.copyLogLines(), limit)
	stats := defaultMonitor.formatStats()
	writeJSON(w, http.StatusOK, map[string]any{
		"logs":       logs,
		"stats":      stats,
		"limit":      limit,
		"updated_at": time.Now().Format("15:04:05"),
	})
}

func (a *API) handleAdminUserReportStatus(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	status := normalizeAdminReportStatus(r.FormValue("status"))
	result := strings.TrimSpace(r.FormValue("result"))
	if id == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	_ = a.reportStore.SetUserReportStatus(r.Context(), id, status, result)
	http.Redirect(w, r, "/admins#reports", http.StatusSeeOther)
}

func (a *API) handleAdminGroupReportStatus(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	status := normalizeAdminReportStatus(r.FormValue("status"))
	result := strings.TrimSpace(r.FormValue("result"))
	if id == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	_ = a.groupReportStore.SetStatus(r.Context(), id, status, result)
	http.Redirect(w, r, "/admins#reports", http.StatusSeeOther)
}

func (a *API) handleAdminResourceReportStatus(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	status := normalizeAdminReportStatus(r.FormValue("status"))
	result := strings.TrimSpace(r.FormValue("result"))
	if id == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	_ = a.resourceReports.SetStatus(r.Context(), id, status, result)
	http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
}

func (a *API) handleAdminResourceDelete(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	itemID := strings.TrimSpace(r.FormValue("item_id"))
	if itemID == "" {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	item, err := a.resources.GetItemByID(ctx, itemID)
	if err != nil {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	itemURL := item.URL
	if err := a.resources.DeleteItem(ctx, itemID); err != nil {
		http.Redirect(w, r, "/admins", http.StatusSeeOther)
		return
	}
	_ = a.resourceReports.DeleteByItemID(ctx, itemID)
	if path := resourceUploadPath(a.cfg.UploadDir, itemURL); path != "" {
		_ = os.Remove(path)
	}
	http.Redirect(w, r, "/admins", http.StatusSeeOther)
}

func (a *API) handleAdminEmojiPlazaDelete(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	itemID := strings.TrimSpace(r.FormValue("item_id"))
	if itemID == "" {
		http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	item, err := a.emojis.GetByID(ctx, itemID)
	if err != nil {
		http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
		return
	}
	if err := a.emojis.DeleteByID(ctx, itemID); err != nil {
		http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
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
	http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
}

func (a *API) handleAdminResourceQuota(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	value := strings.TrimSpace(r.FormValue("resource_quota_gb"))
	if value == "" {
		http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
		return
	}
	gb, err := strconv.ParseFloat(value, 64)
	if err != nil || gb <= 0 {
		http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
		return
	}
	bytes := int64(gb * float64(1024*1024*1024))
	if bytes <= 0 {
		http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
		return
	}
	a.cfg.ResourceQuota = bytes
	_ = persistAdminSettings(a.cfg)
	http.Redirect(w, r, "/admins#resources", http.StatusSeeOther)
}

func (a *API) handleAdminRegistrationLimit(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	value := strings.TrimSpace(r.FormValue("registration_limit"))
	if value == "" {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 {
		http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
		return
	}
	a.cfg.RegistrationLimit = limit
	_ = persistAdminSettings(a.cfg)
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) handleAdminVideoToggle(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	value := strings.TrimSpace(r.FormValue("video_enabled"))
	enabled := value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "on")
	a.cfg.VideoEnabled = enabled
	_ = persistAdminSettings(a.cfg)
	http.Redirect(w, r, "/admins#users", http.StatusSeeOther)
}

func (a *API) handleAdminBandwidthLimit(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	parse := func(raw string, current int64) (int64, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return current, true
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 || v > 200 {
			return current, false
		}
		return int64(v * float64(1024*1024)), true
	}

	parseConcurrency := func(raw string, current int) (int, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return current, true
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 200 {
			return current, false
		}
		return v, true
	}

	mediaRate, ok := parse(r.FormValue("media_rate_mb"), a.cfg.MediaRateBytes)
	if !ok {
		http.Redirect(w, r, "/admins?err=媒体带宽配置非法#server", http.StatusSeeOther)
		return
	}
	updateRate, ok := parse(r.FormValue("update_rate_mb"), a.cfg.UpdateRateBytes)
	if !ok {
		http.Redirect(w, r, "/admins?err=更新包带宽配置非法#server", http.StatusSeeOther)
		return
	}
	videoRate, ok := parse(r.FormValue("video_rate_mb"), a.cfg.VideoRateBytes)
	if !ok {
		http.Redirect(w, r, "/admins?err=视频带宽配置非法#server", http.StatusSeeOther)
		return
	}
	musicRate, ok := parse(r.FormValue("music_rate_mb"), a.cfg.MusicRateBytes)
	if !ok {
		http.Redirect(w, r, "/admins?err=音乐带宽配置非法#server", http.StatusSeeOther)
		return
	}

	mediaDlConcurrency, ok := parseConcurrency(r.FormValue("media_download_concurrency"), a.cfg.MediaDownloadConcurrency)
	if !ok {
		http.Redirect(w, r, "/admins?err=媒体下载并发配置非法#server", http.StatusSeeOther)
		return
	}
	updateDlConcurrency, ok := parseConcurrency(r.FormValue("update_download_concurrency"), a.cfg.UpdateDownloadConcurrency)
	if !ok {
		http.Redirect(w, r, "/admins?err=更新下载并发配置非法#server", http.StatusSeeOther)
		return
	}
	videoDlConcurrency, ok := parseConcurrency(r.FormValue("video_download_concurrency"), a.cfg.VideoDownloadConcurrency)
	if !ok {
		http.Redirect(w, r, "/admins?err=视频下载并发配置非法#server", http.StatusSeeOther)
		return
	}
	musicDlConcurrency, ok := parseConcurrency(r.FormValue("music_download_concurrency"), a.cfg.MusicDownloadConcurrency)
	if !ok {
		http.Redirect(w, r, "/admins?err=音乐下载并发配置非法#server", http.StatusSeeOther)
		return
	}

	a.cfg.MediaRateBytes = mediaRate
	a.cfg.UpdateRateBytes = updateRate
	a.cfg.VideoRateBytes = videoRate
	a.cfg.MusicRateBytes = musicRate
	a.cfg.MediaDownloadConcurrency = mediaDlConcurrency
	a.cfg.UpdateDownloadConcurrency = updateDlConcurrency
	a.cfg.VideoDownloadConcurrency = videoDlConcurrency
	a.cfg.MusicDownloadConcurrency = musicDlConcurrency
	setTransferRateLimits(mediaRate, updateRate, videoRate, musicRate)
	setTransferConcurrencyLimits(mediaDlConcurrency, updateDlConcurrency, videoDlConcurrency, musicDlConcurrency)
	_ = persistAdminSettings(a.cfg)
	http.Redirect(w, r, "/admins?ok=带宽/并发配置已保存#server", http.StatusSeeOther)
}

func (a *API) handleAdminDataSyncConfig(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	publicBase := strings.TrimSpace(r.FormValue("public_base_url"))
	dataBase := strings.TrimRight(strings.TrimSpace(r.FormValue("data_server_base_url")), "/")
	token := strings.TrimSpace(r.FormValue("data_server_sync_token"))
	a.cfg.PublicBaseURL = publicBase
	a.cfg.DataServerBaseURL = dataBase
	a.cfg.DataServerSyncToken = token
	if err := persistAdminSettings(a.cfg); err != nil {
		http.Redirect(w, r, "/admins?err=数据服配置保存失败#server", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admins?ok=数据服同步配置已保存#server", http.StatusSeeOther)
}

func persistAdminSettings(cfg config.Config) error {
	path := strings.TrimSpace(os.Getenv("SETTINGS_JSON"))
	if path == "" {
		path = "settings.json"
	}
	payload := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &payload)
	}
	if cfg.ResourceQuota > 0 {
		payload["resource_quota_bytes"] = cfg.ResourceQuota
	}
	payload["registration_limit"] = cfg.RegistrationLimit
	payload["video_enabled"] = cfg.VideoEnabled
	payload["media_transfer_rate_bytes"] = cfg.MediaRateBytes
	payload["update_transfer_rate_bytes"] = cfg.UpdateRateBytes
	payload["video_transfer_rate_bytes"] = cfg.VideoRateBytes
	payload["music_transfer_rate_bytes"] = cfg.MusicRateBytes
	payload["public_base_url"] = cfg.PublicBaseURL
	payload["data_server_base_url"] = cfg.DataServerBaseURL
	payload["data_server_sync_token"] = cfg.DataServerSyncToken
	payload["media_download_concurrency"] = cfg.MediaDownloadConcurrency
	payload["update_download_concurrency"] = cfg.UpdateDownloadConcurrency
	payload["video_download_concurrency"] = cfg.VideoDownloadConcurrency
	payload["music_download_concurrency"] = cfg.MusicDownloadConcurrency
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func rateBytesToMBpsString(rate int64) string {
	if rate <= 0 {
		return "0"
	}
	mb := float64(rate) / float64(1024*1024)
	return strconv.FormatFloat(mb, 'f', 2, 64)
}

func renderAdminLogin(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminLoginTpl.Execute(w, adminPageData{Error: errMsg})
}

func renderAdminDashboard(w http.ResponseWriter, data adminPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminDashboardTpl.Execute(w, data)
}

func secureEqual(a, b string) bool {
	// Constant-time compare; ok for empty strings too.
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func isHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	return strings.EqualFold(proto, "https")
}

const adminLoginHTML = `
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>旧聊管理后台</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=Noto+Sans+SC:wght@400;500;700&display=swap" rel="stylesheet">
  <style>
    :root{--primary:#4f46e5;--primary-dark:#4338ca;--bg:#0f172a;--surface:#1e293b;--text:#f1f5f9;--muted:#94a3b8;--border:#334155;--error:#ef4444;--success:#22c55e;}
    *{box-sizing:border-box;margin:0;padding:0;}
    body{font-family:"Inter","Noto Sans SC",sans-serif;min-height:100vh;display:flex;align-items:center;justify-content:center;background:var(--bg);color:var(--text);position:relative;overflow:hidden;}
    .bg-pattern{position:fixed;inset:0;background-image:radial-gradient(circle at 20% 50%,rgba(79,70,229,0.15) 0%,transparent 50%),radial-gradient(circle at 80% 80%,rgba(99,102,241,0.1) 0%,transparent 50%),radial-gradient(circle at 40% 20%,rgba(139,92,246,0.08) 0%,transparent 40%);pointer-events:none;}
    .login-container{width:100%;max-width:420px;padding:20px;position:relative;z-index:1;}
    .login-card{background:var(--surface);border:1px solid var(--border);border-radius:16px;padding:40px;box-shadow:0 25px 50px -12px rgba(0,0,0,0.5);backdrop-filter:blur(10px);animation:slideUp 0.5s ease-out;}
    @keyframes slideUp{from{opacity:0;transform:translateY(20px);}to{opacity:1;transform:translateY(0);}}
    .brand{display:flex;align-items:center;gap:16px;margin-bottom:32px;}
    .brand-icon{width:56px;height:56px;border-radius:14px;background:linear-gradient(135deg,var(--primary),var(--primary-dark));display:flex;align-items:center;justify-content:center;font-size:24px;font-weight:700;color:white;box-shadow:0 10px 25px -5px rgba(79,70,229,0.4);}
    .brand-text h1{font-size:24px;font-weight:700;margin-bottom:4px;}
    .brand-text p{font-size:14px;color:var(--muted);}
    .form-group{margin-bottom:20px;}
    label{display:block;font-size:13px;font-weight:500;color:var(--muted);margin-bottom:6px;text-transform:uppercase;letter-spacing:0.5px;}
    input{width:100%;padding:14px 16px;background:#0f172a;border:1px solid var(--border);border-radius:10px;color:var(--text);font-size:15px;transition:all 0.2s;}
    input:focus{outline:none;border-color:var(--primary);box-shadow:0 0 0 3px rgba(79,70,229,0.15);}
    input::placeholder{color:#64748b;}
    .btn{width:100%;padding:14px 24px;background:linear-gradient(135deg,var(--primary),var(--primary-dark));border:none;border-radius:10px;color:white;font-size:15px;font-weight:600;cursor:pointer;transition:all 0.2s;box-shadow:0 4px 14px rgba(79,70,229,0.35);}
    .btn:hover{transform:translateY(-2px);box-shadow:0 8px 20px rgba(79,70,229,0.45);}
    .btn:active{transform:translateY(0);}
    .alert{margin-top:16px;padding:12px 16px;border-radius:8px;font-size:14px;display:flex;align-items:center;gap:10px;animation:shake 0.5s ease;}
    @keyframes shake{0%,100%{transform:translateX(0);}25%{transform:translateX(-5px);}75%{transform:translateX(5px);}}
    .alert-error{background:rgba(239,68,68,0.1);border:1px solid rgba(239,68,68,0.2);color:var(--error);}
    .alert::before{content:"⚠️";font-size:16px;}
    .footer{margin-top:24px;text-align:center;font-size:12px;color:var(--muted);line-height:1.6;}
    .footer a{color:var(--primary);text-decoration:none;}
    .footer a:hover{text-decoration:underline;}
    .input-wrapper{position:relative;}
    .input-icon{position:absolute;right:14px;top:50%;transform:translateY(-50%);color:var(--muted);font-size:16px;}
  </style>
</head>
<body>
  <div class="bg-pattern"></div>
  <div class="login-container">
    <div class="login-card">
      <div class="brand">
        <div class="brand-icon">旧</div>
        <div class="brand-text">
          <h1>旧聊管理后台</h1>
          <p>Authorized Personnel Only</p>
        </div>
      </div>
      <form method="post" action="/admins/login">
        <div class="form-group">
          <label>账号</label>
          <div class="input-wrapper">
            <input name="username" autocomplete="username" placeholder="输入管理员账号" required autofocus>
          </div>
        </div>
        <div class="form-group">
          <label>密码</label>
          <div class="input-wrapper">
            <input name="password" type="password" autocomplete="current-password" placeholder="输入密码" required>
          </div>
        </div>
        <button type="submit" class="btn">安全登录</button>
        {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
      </form>
      <div class="footer">
        <p>🔒 安全提示：请使用独立管理员账号</p>
        <p>请勿与普通用户共享登录凭证</p>
      </div>
    </div>
  </div>
</body>
</html>`

const adminDashboardHTML = `
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>旧聊管理后台</title>
  
  <style>
    :root{--bg:#edf2f8;--card:#ffffff;--line:#d6dfec;--line-strong:#bcc9db;--text:#17212f;--muted:#5c6f89;--blue:#2a66e6;--blue-hover:#1f4fb8;--blue-soft:#ebf2ff;--ok-bg:#ecf8f0;--ok-bd:#84cba0;--ok-tx:#17613a;--err-bg:#fef1f1;--err-bd:#f4aaaa;--err-tx:#b42318;--sidebar:#10192b;--sidebar-text:#d7e2f3;--sidebar-title:#f4f8ff;--shadow:0 8px 26px rgba(15,23,42,.08);}
    *{box-sizing:border-box;}
    html,body{height:100%;}
    body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Microsoft YaHei",Arial,sans-serif;margin:0;background:var(--bg);color:var(--text);line-height:1.5;}
    a{color:#245bd1;text-decoration:none;}
    a:hover{text-decoration:underline;}
    .layout{min-height:100%;}

    .sidebar{position:fixed;left:0;top:0;bottom:0;width:240px;background:linear-gradient(180deg,#10192b 0,#16243d 100%);color:var(--sidebar-text);padding:14px 12px;overflow:auto;z-index:30;border-right:1px solid #233251;}
    .side-title{font-size:16px;font-weight:700;color:var(--sidebar-title);margin:4px 8px 2px;letter-spacing:.2px;}
    .side-sub{font-size:12px;color:#93a6c2;margin:0 8px 12px;}
    .side-nav{display:flex;flex-direction:column;gap:4px;}
    .side-nav a{display:block;padding:8px 10px;border-radius:9px;color:var(--sidebar-text);font-size:13px;border:1px solid transparent;transition:all .15s ease;}
    .side-nav a.active,.side-nav a:hover{background:rgba(42,102,230,.26);color:#fff;border-color:rgba(124,160,236,.6);text-decoration:none;}
    .side-box{margin:14px 6px 0;padding:10px;border:1px solid rgba(148,163,184,.35);border-radius:10px;background:rgba(255,255,255,.04);}
    .side-box h4{margin:0 0 8px;font-size:12px;color:#e9f0fb;}
    .side-box .btn-line,.side-box a{display:block;width:100%;margin-bottom:6px;font-size:12px;padding:7px 8px;border-radius:8px;border:1px solid rgba(148,163,184,.35);background:rgba(255,255,255,.05);color:#e8f0fc;text-align:left;}
    .side-box .btn-line:hover,.side-box a:hover{text-decoration:none;background:rgba(42,102,230,.24);border-color:rgba(124,160,236,.62);}
    .side-box button{width:100%;font-size:12px;padding:7px 8px;border:1px solid #3f79e6;background:#2b67e8;color:#fff;border-radius:8px;cursor:pointer;}
    .side-box button:hover{background:#2355bd;}

    .wrap{max-width:1580px;margin-left:240px;padding:16px;}
    .card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:14px;margin-bottom:12px;box-shadow:var(--shadow);}
    .card:target{border-color:#8dadf0;box-shadow:0 0 0 2px #dbe7ff, var(--shadow);}
    h1,h2,h3{margin:8px 0;line-height:1.3;}
    h1{font-size:22px;font-weight:700;}
    h2{font-size:17px;display:flex;align-items:center;justify-content:space-between;gap:8px;border-bottom:1px solid #e8eef8;padding:0 0 10px;}
    h3{font-size:14px;color:#2b3c55;}

    .top{display:flex;gap:12px;align-items:center;justify-content:space-between;flex-wrap:wrap;}
    .toolbar{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-top:10px;padding:10px;border:1px solid #dbe6f5;border-radius:10px;background:#f8fbff;position:sticky;top:10px;z-index:8;}
    .toolbar .grow{flex:1;min-width:240px;}
    .tabbar{display:none;gap:6px;flex-wrap:wrap;margin-top:10px;}
    .tabbar a{font-size:12px;padding:7px 11px;border:1px solid #cad6ea;border-radius:999px;background:#fff;color:#245bd1;transition:all .15s ease;}
    .tabbar a.active,.tabbar a:hover{background:var(--blue-soft);border-color:#95b1e8;text-decoration:none;}

    .ok{background:var(--ok-bg);border:1px solid var(--ok-bd);color:var(--ok-tx);padding:9px 10px;border-radius:8px;margin-top:10px;}
    .err{background:var(--err-bg);border:1px solid var(--err-bd);color:var(--err-tx);padding:9px 10px;border-radius:8px;margin-top:10px;}
    .stats{display:grid;grid-template-columns:repeat(auto-fill,minmax(170px,1fr));gap:8px;margin-top:10px;}
    .stat{background:#f8fbff;border:1px solid #d9e5f5;border-radius:9px;padding:8px 10px;font-size:13px;font-weight:500;}

    .actions{display:flex;flex-wrap:wrap;gap:7px;align-items:center;margin:7px 0;}
    .actions form{display:inline-flex;gap:6px;align-items:center;flex-wrap:wrap;padding:6px;border-radius:9px;background:#f9fbff;border:1px solid #e1ebfa;}
    input,select,textarea{font-size:12px;padding:7px 9px;border:1px solid var(--line-strong);border-radius:8px;background:#fff;color:var(--text);}
    input:focus,select:focus,textarea:focus{outline:none;border-color:#7096da;box-shadow:0 0 0 3px rgba(42,102,230,.15);}
    textarea{min-height:58px;min-width:180px;resize:vertical;}
    button{font-size:12px;font-weight:600;padding:7px 11px;border:1px solid #2d62d6;background:var(--blue);color:#fff;border-radius:8px;cursor:pointer;transition:all .15s ease;}
    button:hover{background:var(--blue-hover);}
    button:disabled{opacity:.65;cursor:not-allowed;}
    .btn-muted{background:#eef3fb;border-color:#c9d7ec;color:#314257;}
    .btn-muted:hover{background:#e5edf9;}
    .btn-danger{background:#cd3131;border-color:#bb2727;}
    .btn-danger:hover{background:#b82727;}
    .mono{font-family:Menlo,Consolas,monospace;font-size:12px;}
    .muted{color:var(--muted);font-size:12px;}

    .card-body.collapsed{display:none;}
    .collapse-btn{font-size:11px;background:#edf2fb;border:1px solid #cad6ea;color:#334155;padding:4px 9px;border-radius:999px;}

    .table-wrap{overflow:auto;border:1px solid #dbe6f6;border-radius:10px;background:#fff;}
    table{width:100%;border-collapse:collapse;font-size:12px;background:#fff;min-width:780px;}
    th,td{border:1px solid #dee7f5;padding:7px;vertical-align:top;text-align:left;word-break:break-word;}
    th{background:#f4f8ff;font-weight:600;position:sticky;top:0;z-index:1;}
    tbody tr:nth-child(odd){background:#fbfdff;}
    tbody tr:hover{background:#f1f6ff;}
    .table-tools{display:flex;gap:8px;align-items:center;justify-content:space-between;flex-wrap:wrap;margin:8px 0 10px;}
    .table-tools .left,.table-tools .right{display:flex;gap:6px;align-items:center;flex-wrap:wrap;}

    pre{background:#0f172a;color:#dbe6ff;padding:12px;overflow:auto;max-height:300px;font-size:12px;border-radius:8px;line-height:1.45;}

    .batch-panel{position:fixed;right:16px;bottom:16px;background:#0f172a;color:#e2e8f0;border:1px solid #334155;border-radius:12px;padding:10px;z-index:40;max-width:450px;box-shadow:0 14px 34px rgba(15,23,42,.32);}
    .batch-panel .line{display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin-top:6px;}
    .batch-panel input,.batch-panel select{background:#1e293b;color:#e2e8f0;border:1px solid #475569;}
    .batch-panel button{padding:6px 10px;}

    .modal-mask{position:fixed;inset:0;background:rgba(2,6,23,.5);display:none;align-items:center;justify-content:center;z-index:50;padding:16px;}
    .modal{width:100%;max-width:600px;background:#fff;border:1px solid #d8e1ef;border-radius:12px;padding:13px;box-shadow:0 20px 60px rgba(2,6,23,.36);}
    .modal-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:10px;}
    .modal-head h3{margin:0;}

    .hidden{display:none !important;}

    @media (max-width:1024px){
      .sidebar{position:static;width:auto;max-height:none;border-right:none;border-bottom:1px solid #223555;}
      .wrap{margin-left:0;padding:10px;}
      .toolbar{position:static;}
      .tabbar{display:flex;}
      .batch-panel{left:10px;right:10px;max-width:none;}
      .actions form{display:flex;width:100%;}
    }
    @media (max-width:640px){
      .top,.toolbar,.actions,.table-tools{align-items:flex-start;}
      .tabbar{overflow:auto;white-space:nowrap;display:block;}
      .tabbar a{display:inline-block;margin:0 4px 6px 0;}
      input,select,textarea,button{font-size:13px;}
      .card{padding:10px;}
      .stats{grid-template-columns:repeat(2,minmax(0,1fr));}
    }
  </style>

</head>
<body>
<div class="layout">
  <aside class="sidebar">
    <div class="side-title">旧聊 /admins</div>
    <div class="side-sub">左侧固定导航 · 快速入口</div>
    <nav class="side-nav" id="sideNav">
      <a href="#dashboard" data-tab="dashboard">概览</a>
      <a href="#quick" data-tab="quick">快捷入口</a>
      <a href="#users" data-tab="users">用户</a>
      <a href="#titles" data-tab="titles">称号</a>
      <a href="#groups" data-tab="groups">群组</a>
      <a href="#devices" data-tab="devices">设备</a>
      <a href="#reports" data-tab="reports">举报/法庭</a>
      <a href="#banappeals" data-tab="banappeals">封禁申诉</a>
      <a href="#resources" data-tab="resources">资源</a>
      <a href="#notifications" data-tab="notifications">通知</a>
      <a href="#bugs" data-tab="bugs">Bug</a>
      <a href="#crash" data-tab="crash">崩溃日志</a>
      <a href="#server" data-tab="server">服务状态</a>
    </nav>
    <div class="side-box">
      <h4>快速操作面板</h4>
      <button type="button" class="btn-line" data-open-modal="modalCreateUser">创建测试用户</button>
      <button type="button" class="btn-line" data-open-modal="modalBanUser">封禁用户</button>
      <button type="button" class="btn-line" data-open-modal="modalSendNotice">发送系统通知</button>
      <a href="/admins/crash-reports" target="_blank" rel="noopener">打开崩溃日志 JSON</a>
      <a href="/admins/server-log?logs_limit={{.ServerLogLimit}}" target="_blank" rel="noopener">打开服务日志 JSON</a>
      <a href="/igotbanned" target="_blank" rel="noopener">打开用户申诉页</a>
    </div>
  </aside>
  <div class="wrap">
  <div id="dashboard" class="card">
    <div class="top">
      <h1>旧聊管理后台（简版）</h1>
      <div class="actions">
        <a href="/igotbanned" target="_blank" rel="noopener">打开用户申诉页 /igotbanned</a>
        <form method="post" action="/admins/logout" style="display:inline;"><button type="submit">退出登录</button></form>
      </div>
    </div>
    {{if .Success}}<div class="ok">{{.Success}}</div>{{end}}
    {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
    <div class="stats">
      <div class="stat">用户总数：{{.Stats.TotalUsers}}</div>
      <div class="stat">封禁用户：{{.Stats.BannedUserCount}}</div>
      <div class="stat">封禁设备：{{.Stats.BannedDeviceCount}}</div>
      <div class="stat">举报总数：{{.Stats.TotalReports}}</div>
      <div class="stat">待处理申诉：{{.Stats.PendingBanAppeals}}</div>
      <div class="stat">Bug反馈：{{.Stats.TotalBugReports}}</div>
      <div class="stat">资源举报：{{.Stats.TotalResourceReports}}</div>
      <div class="stat">活跃发言人数：{{.Stats.ActiveMessageUsers}}</div>
    </div>
    <div class="toolbar">
      <input id="globalSearch" class="grow" placeholder="全局搜索：UID / 用户名 / 案件 / 举报 / 群号 ...">
      <button type="button" class="btn-muted" id="expandAllBtn">全部展开</button>
      <button type="button" class="btn-muted" id="collapseAllBtn">全部折叠</button>
      <button type="button" class="btn-muted" id="showAllTabsBtn">显示全部标签</button>
    </div>
    <div class="tabbar" id="topTabbar">
      <a href="#dashboard" data-tab="dashboard">概览</a>
      <a href="#quick" data-tab="quick">快捷入口</a>
      <a href="#users" data-tab="users">用户</a>
      <a href="#titles" data-tab="titles">称号</a>
      <a href="#groups" data-tab="groups">群组</a>
      <a href="#devices" data-tab="devices">设备</a>
      <a href="#reports" data-tab="reports">举报/法庭</a>
      <a href="#banappeals" data-tab="banappeals">封禁申诉</a>
      <a href="#resources" data-tab="resources">资源</a>
      <a href="#notifications" data-tab="notifications">通知</a>
      <a href="#bugs" data-tab="bugs">Bug</a>
      <a href="#crash" data-tab="crash">崩溃日志</a>
      <a href="#server" data-tab="server">服务状态</a>
    </div>
  </div>

  <div id="quick" class="card">
    <h2>快捷入口（补齐所有旧入口）</h2>
    <div class="actions">
      <a href="/admins">/admins</a>
      <a href="/admins/bug-reports" target="_blank" rel="noopener">/admins/bug-reports</a>
      <a href="/admins/crash-reports" target="_blank" rel="noopener">/admins/crash-reports</a>
      <a href="/admins/server-log?logs_limit={{.ServerLogLimit}}" target="_blank" rel="noopener">/admins/server-log</a>
      <a href="/igotbanned" target="_blank" rel="noopener">/igotbanned</a>
    </div>
    <div class="actions">
      <form method="get" action="/admins" class="actions">
        <span>用户列表条数</span>
        <input name="users_limit" placeholder="1-1000" style="width:90px;">
        <button type="submit">应用</button>
      </form>
      <form method="get" action="/admins" class="actions">
        <span>日志条数</span>
        <input name="logs_limit" value="{{.ServerLogLimit}}" style="width:90px;">
        <button type="submit">应用</button>
      </form>
    </div>
  </div>

  <div id="users" class="card">
    <h2>用户管理</h2>
    <div class="actions">
      <form method="post" action="/admins/registration-limit" class="actions">
        <span>注册限制</span>
        <input name="registration_limit" value="{{.RegistrationLimit}}" style="width:90px;">
        <button type="submit">保存</button>
      </form>
      <form method="post" action="/admins/video-toggle" class="actions">
        <span>视频功能</span>
        <select name="video_enabled">
          <option value="1" {{if .VideoEnabled}}selected{{end}}>开启</option>
          <option value="0" {{if .VideoEnabled}}{{else}}selected{{end}}>关闭</option>
        </select>
        <button type="submit">保存</button>
      </form>
    </div>

    <h3>创建测试用户</h3>
    <form method="post" action="/admins/user/create" class="actions">
      <input name="username" placeholder="用户名" required>
      <input name="password" placeholder="密码" required>
      <input name="uid" placeholder="UID(可选)">
      <input name="email" placeholder="邮箱(可选)">
      <input name="display_name" placeholder="显示名(可选)">
      <input name="coin_balance" placeholder="金币(默认10)" style="width:120px;">
      <button type="submit">创建</button>
    </form>

    <h3>常用操作</h3>
    <form method="post" action="/admins/ban/user" class="actions">
      <strong>封禁用户</strong>
      <input name="user_uid" placeholder="UID">
      <input name="user_id" placeholder="UserID">
      <input name="reason" placeholder="原因">
      <input name="duration_hours" placeholder="时长h，0=永久" style="width:120px;">
      <button type="submit">提交</button>
    </form>
    <form method="post" action="/admins/unban/user" class="actions">
      <strong>解封用户</strong>
      <input name="user_uid" placeholder="UID">
      <input name="user_id" placeholder="UserID">
      <button type="submit">提交</button>
    </form>
    <form method="post" action="/admins/user-title" class="actions">
      <strong>设置称号</strong>
      <input name="user_uid" placeholder="UID" required>
      <input name="user_title" placeholder="称号内容，留空清除" style="min-width:260px;">
      <button type="submit">提交</button>
    </form>
    <form method="post" action="/admins/user-coin" class="actions">
      <strong>设置金币</strong>
      <input name="user_uid" placeholder="UID" required>
      <input name="coin_balance" placeholder="金币余额" required>
      <button type="submit">提交</button>
    </form>
    <form method="post" action="/admins/user-uid" class="actions">
      <strong>修改UID</strong>
      <input name="user_uid" placeholder="当前UID" required>
      <input name="new_uid" placeholder="新UID" required>
      <button type="submit">提交</button>
    </form>

    <h3>最近用户</h3>
    <table id="usersTable" data-batch="user">
      <thead>
        <tr>
          <th>UID</th><th>用户名</th><th>邮箱</th><th>称号</th><th>金币</th><th>注册时间</th><th>状态</th><th>操作</th>
        </tr>
      </thead>
      <tbody>
      {{range .Users}}
        <tr>
          <td class="mono">{{.UID}}</td>
          <td>{{.Username}}</td>
          <td>{{.Email}}</td>
          <td>{{.UserTitle}}</td>
          <td>{{.CoinBalance}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>{{if eq .Banned 1}}已封禁{{else}}正常{{end}}</td>
          <td>
            <form method="post" action="/admins/user-coin" class="actions">
              <input type="hidden" name="user_uid" value="{{.UID}}">
              <input name="coin_balance" placeholder="金币" style="width:70px;">
              <button type="submit">改金币</button>
            </form>
            {{if eq .Banned 1}}
            <form method="post" action="/admins/unban/user" class="actions">
              <input type="hidden" name="user_uid" value="{{.UID}}">
              <button type="submit">解封</button>
            </form>
            {{else}}
            <form method="post" action="/admins/ban/user" class="actions">
              <input type="hidden" name="user_uid" value="{{.UID}}">
              <input name="reason" placeholder="原因" style="width:90px;">
              <input name="duration_hours" placeholder="小时" style="width:70px;">
              <button type="submit">封禁</button>
            </form>
            {{end}}
            <form method="post" action="/admins/user/deactivate" class="actions">
              <input type="hidden" name="user_uid" value="{{.UID}}">
              <input name="reason" placeholder="停用原因" style="width:110px;">
              <button type="submit">停用</button>
            </form>
            <form method="post" action="/admins/user/delete" class="actions" onsubmit="return confirm('确定删除该用户？');">
              <input type="hidden" name="user_uid" value="{{.UID}}">
              <input type="hidden" name="confirm" value="1">
              <button type="submit">删除</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>

    <h3>消息活跃用户（按发消息统计）</h3>
    <form method="get" action="/admins" class="actions">
      <label>范围</label>
      <select name="active_range">
        <option value="24h" {{if eq .ActiveRange "24h"}}selected{{end}}>24h</option>
        <option value="7d" {{if eq .ActiveRange "7d"}}selected{{end}}>7d</option>
        <option value="30d" {{if eq .ActiveRange "30d"}}selected{{end}}>30d</option>
        <option value="custom" {{if eq .ActiveRange "custom"}}selected{{end}}>自定义</option>
      </select>
      <input type="datetime-local" name="active_start" value="{{.ActiveStart}}">
      <input type="datetime-local" name="active_end" value="{{.ActiveEnd}}">
      <input name="active_limit" value="{{.ActiveLimit}}" style="width:80px;">
      <button type="submit">刷新</button>
    </form>
    <table>
      <thead><tr><th>UID</th><th>用户名</th><th>邮箱</th><th>最近发言</th><th>私聊</th><th>群聊</th><th>总消息</th></tr></thead>
      <tbody>
      {{range .ActiveUsers}}
        <tr>
          <td class="mono">{{.UID}}</td>
          <td>{{.Username}}</td>
          <td>{{.Email}}</td>
          <td>{{.LastActivity.Format "2006-01-02 15:04"}}</td>
          <td>{{.DirectCount}}</td>
          <td>{{.GroupCount}}</td>
          <td>{{.MessageCount}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="titles" class="card">
    <h2>称号目录</h2>
    <form method="post" action="/admins/title/create" class="actions">
      <input name="title" placeholder="称号内容" required>
      <input name="price" placeholder="价格" value="100" style="width:90px;">
      <button type="submit">新增称号</button>
    </form>
    <table>
      <thead><tr><th>ID</th><th>称号</th><th>价格</th><th>状态</th><th>自定义</th><th>操作</th></tr></thead>
      <tbody>
      {{range .TitleCatalog}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td>{{.Title}}</td>
          <td>{{.Price}}</td>
          <td>{{if eq .Active 1}}启用{{else}}停用{{end}}</td>
          <td>{{if eq .IsCustom 1}}是{{else}}否{{end}}</td>
          <td>
            <form method="post" action="/admins/title/update" class="actions">
              <input type="hidden" name="id" value="{{.ID}}">
              <input name="title" value="{{.Title}}" style="width:150px;">
              <input name="price" value="{{.Price}}" style="width:70px;">
              <button type="submit">更新</button>
            </form>
            <form method="post" action="/admins/title/toggle" class="actions">
              <input type="hidden" name="id" value="{{.ID}}">
              <input type="hidden" name="active" value="{{if eq .Active 1}}0{{else}}1{{end}}">
              <button type="submit">{{if eq .Active 1}}停用{{else}}启用{{end}}</button>
            </form>
            <form method="post" action="/admins/title/delete" class="actions" onsubmit="return confirm('确定删除该称号？');">
              <input type="hidden" name="id" value="{{.ID}}">
              <button type="submit">删除</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="groups" class="card">
    <h2>群组管理</h2>
    <form method="post" action="/admins/group/ban" class="actions">
      <strong>封禁群</strong>
      <input name="group_id" placeholder="群ID" required>
      <input name="reason" placeholder="原因">
      <input name="duration_hours" placeholder="时长h，0永久" style="width:120px;">
      <button type="submit">提交</button>
    </form>
    <form method="post" action="/admins/group/unban" class="actions">
      <strong>解封群</strong>
      <input name="group_id" placeholder="群ID" required>
      <button type="submit">提交</button>
    </form>
    <form method="post" action="/admins/group/delete" class="actions" onsubmit="return confirm('确定删除该群？');">
      <strong>删除群</strong>
      <input name="group_id" placeholder="群ID" required>
      <button type="submit">提交</button>
    </form>
    <form method="post" action="/admins/group/owner" class="actions">
      <strong>设置群主</strong>
      <input name="group_id" placeholder="群ID" required>
      <input name="owner_uid" placeholder="新群主UID" required>
      <button type="submit">提交</button>
    </form>

    <table id="groupsTable" data-batch="group">
      <thead><tr><th>群ID</th><th>群名</th><th>群主UID</th><th>群主</th><th>人数</th><th>创建时间</th><th>封禁</th><th>封禁原因</th><th>操作</th></tr></thead>
      <tbody>
      {{range .Groups}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td>{{.Name}}</td>
          <td class="mono">{{.OwnerUID}}</td>
          <td>{{.OwnerName}}</td>
          <td>{{.MemberCount}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>{{if eq .Banned 1}}已封禁{{if .BannedUntil.Valid}} (至 {{.BannedUntil.Time.Format "2006-01-02 15:04"}}){{end}}{{else}}正常{{end}}</td>
          <td>{{.BannedReason}}</td>
          <td>
            {{if eq .Banned 1}}
            <form method="post" action="/admins/group/unban" class="actions">
              <input type="hidden" name="group_id" value="{{.ID}}">
              <button type="submit">解封</button>
            </form>
            {{else}}
            <form method="post" action="/admins/group/ban" class="actions">
              <input type="hidden" name="group_id" value="{{.ID}}">
              <input name="reason" placeholder="原因" style="width:90px;">
              <input name="duration_hours" placeholder="小时" style="width:70px;">
              <button type="submit">封禁</button>
            </form>
            {{end}}
            <form method="post" action="/admins/group/owner" class="actions">
              <input type="hidden" name="group_id" value="{{.ID}}">
              <input name="owner_uid" placeholder="新群主UID" style="width:100px;">
              <button type="submit">改群主</button>
            </form>
            <form method="post" action="/admins/group/delete" class="actions" onsubmit="return confirm('确定删除该群？');">
              <input type="hidden" name="group_id" value="{{.ID}}">
              <button type="submit">删除</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="devices" class="card">
    <h2>设备管理</h2>
    <form method="post" action="/admins/ban/device" class="actions">
      <strong>封禁设备</strong>
      <input name="device_id" placeholder="设备ID" required>
      <input name="reason" placeholder="原因">
      <input name="duration_hours" placeholder="时长h，0永久" style="width:120px;">
      <button type="submit">提交</button>
    </form>
    <form method="post" action="/admins/unban/device" class="actions">
      <strong>解封设备</strong>
      <input name="device_id" placeholder="设备ID" required>
      <button type="submit">提交</button>
    </form>

    <h3>最近登录设备</h3>
    <table id="devicesTable" data-batch="device">
      <thead><tr><th>设备ID</th><th>IMEI</th><th>UserID</th><th>UID</th><th>用户名</th><th>最近登录</th><th>封禁</th><th>操作</th></tr></thead>
      <tbody>
      {{range .Devices}}
        <tr>
          <td class="mono">{{.DeviceID}}</td>
          <td class="mono">{{.IMEI}}</td>
          <td class="mono">{{.UserID}}</td>
          <td class="mono">{{.UID}}</td>
          <td>{{.Username}}</td>
          <td>{{.LastSeen.Format "2006-01-02 15:04"}}</td>
          <td>{{if eq .Banned 1}}已封禁{{else}}正常{{end}}</td>
          <td>
            {{if eq .Banned 1}}
            <form method="post" action="/admins/unban/device" class="actions">
              <input type="hidden" name="device_id" value="{{.DeviceID}}">
              <button type="submit">解封</button>
            </form>
            {{else}}
            <form method="post" action="/admins/ban/device" class="actions">
              <input type="hidden" name="device_id" value="{{.DeviceID}}">
              <input name="reason" placeholder="原因" style="width:90px;">
              <input name="duration_hours" placeholder="小时" style="width:70px;">
              <button type="submit">封禁</button>
            </form>
            {{end}}
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>

    <h3>封禁设备列表</h3>
    <table>
      <thead><tr><th>设备ID</th><th>原因</th><th>封禁时间</th><th>到期</th></tr></thead>
      <tbody>
      {{range .BannedDevices}}
        <tr>
          <td class="mono">{{.DeviceID}}</td>
          <td>{{.Reason}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>{{if .BannedUntil.Valid}}{{.BannedUntil.Time.Format "2006-01-02 15:04"}}{{else}}永久{{end}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>

    <h3>封禁用户列表</h3>
    <table>
      <thead><tr><th>UserID</th><th>UID</th><th>用户名</th><th>原因</th><th>封禁时间</th><th>到期</th><th>操作</th></tr></thead>
      <tbody>
      {{range .BannedUsers}}
        <tr>
          <td class="mono">{{.UserID}}</td>
          <td class="mono">{{.UID}}</td>
          <td>{{.Username}}</td>
          <td>{{.Reason}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>{{if .BannedUntil.Valid}}{{.BannedUntil.Time.Format "2006-01-02 15:04"}}{{else}}永久{{end}}</td>
          <td>
            <form method="post" action="/admins/unban/user" class="actions">
              <input type="hidden" name="user_id" value="{{.UserID}}">
              <button type="submit">解封</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="reports" class="card">
    <h2>举报与公开法庭</h2>
    <form method="post" action="/admins/public-court/clear" class="actions" onsubmit="return confirm('确定清空所有公开法庭案件吗？此操作不可恢复。');">
      <input type="hidden" name="confirm" value="1">
      <button type="submit">一键清空全部公开法庭案件</button>
      <span class="muted">会同时删除案件、投票、观点证据记录。</span>
    </form>

    <h3>用户举报</h3>
    <table>
      <thead><tr><th>ID</th><th>举报人</th><th>目标UID</th><th>目标设备</th><th>理由</th><th>状态</th><th>结果</th><th>时间</th><th>操作</th></tr></thead>
      <tbody>
      {{range .Reports}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td class="mono">{{.ReporterUID}}</td>
          <td class="mono">{{.TargetUID}}</td>
          <td class="mono">{{.TargetDevice}}</td>
          <td>{{.Reason}}</td>
          <td>{{.Status}}</td>
          <td>{{.Result}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>
            <form method="post" action="/admins/user-reports/status" class="actions">
              <input type="hidden" name="id" value="{{.ID}}">
              <select name="status">
                <option value="pending">pending</option>
                <option value="accepted">accepted</option>
                <option value="rejected">rejected</option>
              </select>
              <input name="result" placeholder="处理说明" style="width:100px;">
              <button type="submit">更新</button>
            </form>
            <form method="post" action="/admins/ban/user" class="actions">
              <input type="hidden" name="user_uid" value="{{.TargetUID}}">
              <input name="reason" placeholder="封禁原因" style="width:100px;">
              <input name="duration_hours" placeholder="小时" style="width:60px;">
              <button type="submit">封用户</button>
            </form>
            {{if .TargetDevice}}
            <form method="post" action="/admins/ban/device" class="actions">
              <input type="hidden" name="device_id" value="{{.TargetDevice}}">
              <input name="reason" placeholder="封禁原因" style="width:100px;">
              <input name="duration_hours" placeholder="小时" style="width:60px;">
              <button type="submit">封设备</button>
            </form>
            {{end}}
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>

    <h3>群举报</h3>
    <table>
      <thead><tr><th>ID</th><th>举报人</th><th>群ID</th><th>群名</th><th>理由</th><th>状态</th><th>结果</th><th>时间</th><th>操作</th></tr></thead>
      <tbody>
      {{range .GroupReports}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td class="mono">{{.ReporterUID}}</td>
          <td class="mono">{{.GroupID}}</td>
          <td>{{.GroupName}}</td>
          <td>{{.Reason}}</td>
          <td>{{.Status}}</td>
          <td>{{.Result}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>
            <form method="post" action="/admins/group-reports/status" class="actions">
              <input type="hidden" name="id" value="{{.ID}}">
              <select name="status">
                <option value="pending">pending</option>
                <option value="accepted">accepted</option>
                <option value="rejected">rejected</option>
              </select>
              <input name="result" placeholder="处理说明" style="width:100px;">
              <button type="submit">更新</button>
            </form>
            <form method="post" action="/admins/group/ban" class="actions">
              <input type="hidden" name="group_id" value="{{.GroupID}}">
              <input name="reason" placeholder="封禁原因" style="width:100px;">
              <input name="duration_hours" placeholder="小时" style="width:60px;">
              <button type="submit">封群</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>

    <h3>公开法庭（待二审）</h3>
    <p class="muted">达到投票锁定后的案件。管理员在此二审并决定最终封禁时长。</p>
    <table>
      <thead><tr><th>案件ID</th><th>举报方</th><th>被举报方</th><th>票数(封/不封/总)</th><th>状态</th><th>初审结果</th><th>创建时间</th><th>二审</th></tr></thead>
      <tbody>
      {{range .PublicCourtPendingCases}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td>{{if .ReporterName}}{{.ReporterName}}{{else}}{{.ReporterUID}}{{end}} ({{.ReporterUID}})</td>
          <td>{{if .DefendantName}}{{.DefendantName}}{{else}}{{.DefendantUID}}{{end}} ({{.DefendantUID}})</td>
          <td>{{.BanVoteCount}} / {{.KeepVoteCount}} / {{.TotalVoteCount}}</td>
          <td>{{.Status}}</td>
          <td>{{.Verdict}}{{if eq .Verdict "ban"}} {{.BanHours}}h{{end}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>
            <form method="post" action="/admins/public-court/close" class="actions">
              <input type="hidden" name="case_id" value="{{.ID}}">
              <select name="verdict">
                <option value="ban">ban</option>
                <option value="keep">keep</option>
              </select>
              <input name="ban_hours" value="24" style="width:60px;">
              <input name="admin_note" placeholder="二审说明" style="width:120px;">
              <button type="submit">提交二审</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>

    <h3>公开法庭（全部案件）</h3>
    <table>
      <thead><tr><th>案件ID</th><th>举报方</th><th>被举报方</th><th>状态</th><th>裁决</th><th>票数</th><th>开庭时间</th></tr></thead>
      <tbody>
      {{range .PublicCourtCases}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td>{{if .ReporterName}}{{.ReporterName}}{{else}}{{.ReporterUID}}{{end}}</td>
          <td>{{if .DefendantName}}{{.DefendantName}}{{else}}{{.DefendantUID}}{{end}}</td>
          <td>{{.Status}}</td>
          <td>{{.Verdict}}{{if eq .Verdict "ban"}} {{.BanHours}}h{{end}}</td>
          <td>{{.BanVoteCount}} / {{.KeepVoteCount}} / {{.TotalVoteCount}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="banappeals" class="card">
    <h2>封禁申诉</h2>
    <p class="muted">用户入口：<a href="/igotbanned" target="_blank" rel="noopener">/igotbanned</a></p>
    <table>
      <thead><tr><th>ID</th><th>UID/用户名</th><th>封禁原因</th><th>申诉内容</th><th>联系方式</th><th>状态</th><th>管理员备注</th><th>提交时间</th><th>操作</th></tr></thead>
      <tbody>
      {{range .BanAppeals}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td class="mono">{{.UID}} / {{.Username}}</td>
          <td>
            <div>{{.BanReason}}</div>
            <div class="muted">{{if .BannedUntil.Valid}}至 {{.BannedUntil.Time.Format "2006-01-02 15:04"}}{{else}}永久{{end}}</div>
          </td>
          <td style="white-space:pre-wrap;">{{.AppealText}}</td>
          <td>{{.Contact}}</td>
          <td>{{.Status}}</td>
          <td>{{.AdminNote}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>
            <form method="post" action="/admins/ban-appeals/status" class="actions">
              <input type="hidden" name="id" value="{{.ID}}">
              <select name="status">
                <option value="pending" {{if eq .Status "pending"}}selected{{end}}>pending</option>
                <option value="approved" {{if eq .Status "approved"}}selected{{end}}>approved</option>
                <option value="rejected" {{if eq .Status "rejected"}}selected{{end}}>rejected</option>
              </select>
              <input name="admin_note" placeholder="备注" value="{{.AdminNote}}" style="width:110px;">
              <label><input type="checkbox" name="unban_on_approve" value="1">审批通过时解封</label>
              <button type="submit">提交</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="resources" class="card">
    <h2>资源管理与举报</h2>
    <form method="post" action="/admins/resource-quota" class="actions">
      <span>资源总额(GB)</span>
      <input name="resource_quota_gb" value="{{.ResourceQuotaGB}}" style="width:100px;">
      <button type="submit">保存</button>
      <span class="muted">当前字节：{{.ResourceQuotaBytes}}</span>
    </form>

    <table>
      <thead><tr><th>ID</th><th>资源</th><th>版块</th><th>上传者</th><th>举报人</th><th>理由</th><th>状态</th><th>结果</th><th>时间</th><th>操作</th></tr></thead>
      <tbody>
      {{range .ResourceReports}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td>
            <div>{{.ItemName}}</div>
            <div class="mono">{{.ItemID}}</div>
            <div class="muted">{{.ItemURL}}</div>
          </td>
          <td>{{.SectionName}}</td>
          <td>{{.UploaderUID}} {{.UploaderName}}</td>
          <td>{{.ReporterUID}} {{.ReporterName}}</td>
          <td>{{.Reason}}</td>
          <td>{{.Status}}</td>
          <td>{{.Result}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>
            <form method="post" action="/admins/resource-reports/status" class="actions">
              <input type="hidden" name="id" value="{{.ID}}">
              <select name="status">
                <option value="pending">pending</option>
                <option value="accepted">accepted</option>
                <option value="rejected">rejected</option>
              </select>
              <input name="result" placeholder="处理说明" style="width:100px;">
              <button type="submit">更新</button>
            </form>
            <form method="post" action="/admins/resources/delete" class="actions" onsubmit="return confirm('确定删除该资源？');">
              <input type="hidden" name="item_id" value="{{.ItemID}}">
              <button type="submit">删资源</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>

    <h3>表情广场（最近300条）</h3>
    <table>
      <thead><tr><th>ID</th><th>名称</th><th>上传者</th><th>资源</th><th>数量/大小</th><th>时间</th><th>操作</th></tr></thead>
      <tbody>
      {{range .EmojiPlazaItems}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td>{{.Name}}</td>
          <td>{{if .OwnerName}}{{.OwnerName}}{{else}}{{.OwnerUID}}{{end}}</td>
          <td>
            <div class="mono">{{.MediaURL}}</div>
            {{if .PackageURL}}<div class="mono">{{.PackageURL}}</div>{{end}}
            {{if .CoverURL}}<div class="mono">{{.CoverURL}}</div>{{end}}
          </td>
          <td>{{.ItemCount}} / {{.SizeBytes}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>
            <form method="post" action="/admins/emoji-plaza/delete" class="actions" onsubmit="return confirm('确定删除该表情包？');">
              <input type="hidden" name="item_id" value="{{.ID}}">
              <button type="submit">删除表情包</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="notifications" class="card">
    <h2>系统通知</h2>
    <form method="post" action="/admins/notification/send" class="actions">
      <input name="title" placeholder="标题(可选)" style="width:180px;">
      <textarea name="body" placeholder="通知内容" required></textarea>
      <label><input type="checkbox" name="important" value="1">重要通知</label>
      <button type="submit">发送</button>
    </form>

    <table>
      <thead><tr><th>ID</th><th>标题</th><th>内容</th><th>重要</th><th>时间</th><th>操作</th></tr></thead>
      <tbody>
      {{range .Notifications}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td>{{.Title}}</td>
          <td style="white-space:pre-wrap;">{{.Body}}</td>
          <td>{{if .Important}}是{{else}}否{{end}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>
            <form method="post" action="/admins/notification/delete" onsubmit="return confirm('确定删除该通知？');">
              <input type="hidden" name="id" value="{{.ID}}">
              <button type="submit">删除</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="bugs" class="card">
    <h2>Bug反馈</h2>
    <table>
      <thead><tr><th>ID</th><th>用户</th><th>内容</th><th>设备</th><th>系统/版本</th><th>状态</th><th>备注</th><th>时间</th><th>操作</th></tr></thead>
      <tbody>
      {{range .BugReports}}
        <tr>
          <td class="mono">{{.ID}}</td>
          <td class="mono">{{.UserUID}}</td>
          <td style="white-space:pre-wrap;">{{.Content}}</td>
          <td>{{.DeviceModel}}</td>
          <td>{{.AndroidVersion}} / {{.AppVersion}}</td>
          <td>{{.Status}}</td>
          <td>{{.AdminNote}}</td>
          <td>{{.CreatedAt.Format "2006-01-02 15:04"}}</td>
          <td>
            <form method="post" action="/admins/bug-reports/status" class="actions">
              <input type="hidden" name="id" value="{{.ID}}">
              <select name="status">
                <option value="open">open</option>
                <option value="resolved">resolved</option>
                <option value="closed">closed</option>
              </select>
              <input name="note" placeholder="管理员备注" style="width:100px;">
              <button type="submit">更新</button>
            </form>
            <form method="post" action="/admins/bug-reports/delete" class="actions" onsubmit="return confirm('确定删除该条反馈？');">
              <input type="hidden" name="id" value="{{.ID}}">
              <button type="submit">删除</button>
            </form>
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  <div id="crash" class="card">
    <h2>崩溃日志（Crash Reports）</h2>
    <p class="muted">完整数据通过 JSON 接口查看；保留旧入口不删。</p>
    <div class="actions">
      <a href="/admins/crash-reports" target="_blank" rel="noopener">打开 /admins/crash-reports</a>
      <span class="muted">可在 URL 追加 <span class="mono">?limit=100</span> 控制返回条数。</span>
    </div>
  </div>

  <div id="server" class="card">
    <h2>服务状态</h2>
    <h3>带宽限速（MB/s，填 0=不限速）</h3>
    <form method="post" action="/admins/bandwidth-limit" class="actions">
      <span>媒体下载/上传</span>
      <input name="media_rate_mb" value="{{.MediaRateMB}}" style="width:90px;">
      <span>更新包下载</span>
      <input name="update_rate_mb" value="{{.UpdateRateMB}}" style="width:90px;">
      <span>视频下载/上传</span>
      <input name="video_rate_mb" value="{{.VideoRateMB}}" style="width:90px;">
      <span>音乐播放下载</span>
      <input name="music_rate_mb" value="{{.MusicRateMB}}" style="width:90px;">
      <span style="margin-left:8px;">媒体下载并发</span>
      <input name="media_download_concurrency" value="{{.MediaDownloadConcurrency}}" style="width:72px;">
      <span>更新下载并发</span>
      <input name="update_download_concurrency" value="{{.UpdateDownloadConcurrency}}" style="width:72px;">
      <span>视频下载并发</span>
      <input name="video_download_concurrency" value="{{.VideoDownloadConcurrency}}" style="width:72px;">
      <span>音乐下载并发</span>
      <input name="music_download_concurrency" value="{{.MusicDownloadConcurrency}}" style="width:72px;">
      <button type="submit">保存带宽/并发配置</button>
    </form>
    <h3>数据服同步（A 上传后推送到 B）</h3>
    <form method="post" action="/admins/data-sync" class="actions">
      <span>A对外地址</span>
      <input name="public_base_url" value="{{.PublicBaseURL}}" placeholder="http://localhost:8080" style="width:220px;">
      <span>B地址</span>
      <input name="data_server_base_url" value="{{.DataServerBaseURL}}" placeholder="http://localhost:9090" style="width:220px;">
      <span>同步令牌(可空)</span>
      <input name="data_server_sync_token" value="{{.DataServerSyncToken}}" style="width:180px;">
      <button type="submit">保存同步配置</button>
    </form>
    <div class="actions">
      <a href="/admins">刷新页面</a>
      <a href="/admins/server-log?logs_limit={{.ServerLogLimit}}" target="_blank" rel="noopener">查看日志JSON</a>
      <span class="muted">日志条数：{{.ServerLogLimit}} / 最大 {{.ServerLogMax}}</span>
    </div>
    <h3>运行指标</h3>
    <ul>
      {{range .ServerStats}}<li>{{.}}</li>{{end}}
    </ul>
    <h3>最近日志</h3>
    <pre>{{range .ServerLogs}}{{.}}
{{end}}</pre>
  </div>


<div id="batchPanel" class="batch-panel">
  <div><strong>批量操作</strong> <span id="batchCount" class="muted">已选 0 项</span></div>
  <div class="line">
    <label>类型</label>
    <select id="batchType">
      <option value="">自动识别</option>
      <option value="user">用户</option>
      <option value="group">群组</option>
      <option value="device">设备</option>
    </select>
    <label>动作</label>
    <select id="batchAction"></select>
  </div>
  <div class="line">
    <input id="batchReason" placeholder="原因（封禁/停用时建议填写）" style="width:190px;">
    <input id="batchHours" placeholder="时长h（0永久）" style="width:110px;">
    <button type="button" id="batchRunBtn">执行</button>
    <button type="button" id="batchClearBtn" class="btn-muted">清空勾选</button>
  </div>
</div>

<div id="adminModalMask" class="modal-mask">
  <div class="modal">
    <div class="modal-head">
      <h3 id="adminModalTitle">操作</h3>
      <button type="button" class="btn-muted" id="adminModalClose">关闭</button>
    </div>
    <div id="adminModalBody"></div>
  </div>
</div>

<template id="tplCreateUser">
  <form method="post" action="/admins/user/create" class="actions">
    <input name="username" placeholder="用户名" required>
    <input name="password" placeholder="密码" required>
    <input name="uid" placeholder="UID(可选)">
    <input name="email" placeholder="邮箱(可选)">
    <input name="display_name" placeholder="显示名(可选)">
    <input name="coin_balance" placeholder="金币(默认10)" style="width:120px;">
    <button type="submit">创建</button>
  </form>
</template>

<template id="tplBanUser">
  <form method="post" action="/admins/ban/user" class="actions">
    <input name="user_uid" placeholder="UID（优先）">
    <input name="user_id" placeholder="UserID（可选）">
    <input name="reason" placeholder="封禁原因">
    <input name="duration_hours" placeholder="时长h，0=永久" style="width:120px;">
    <button type="submit">提交封禁</button>
  </form>
</template>

<template id="tplSendNotice">
  <form method="post" action="/admins/notification/send" class="actions">
    <input name="title" placeholder="标题(可选)" style="width:180px;">
    <textarea name="body" placeholder="通知内容" required></textarea>
    <label><input type="checkbox" name="important" value="1">重要通知</label>
    <button type="submit">发送</button>
  </form>
</template>

<script>
(function(){
  var tabIds = ['dashboard','quick','users','titles','groups','devices','reports','banappeals','resources','notifications','bugs','crash','server'];
  var panelMap = {};
  for (var i = 0; i < tabIds.length; i++) {
    var panel = document.getElementById(tabIds[i]);
    if (panel) { panelMap[tabIds[i]] = panel; }
  }

  function setActiveTab(id){
    var links = document.querySelectorAll('[data-tab]');
    for (var i=0;i<links.length;i++) {
      var active = links[i].getAttribute('data-tab') === id;
      if (active) links[i].classList.add('active'); else links[i].classList.remove('active');
    }
    for (var key in panelMap) {
      if (!panelMap.hasOwnProperty(key)) continue;
      if (id === 'all') {
        panelMap[key].classList.remove('hidden');
      } else {
        if (key === 'dashboard' || key === id) panelMap[key].classList.remove('hidden'); else panelMap[key].classList.add('hidden');
      }
    }
  }

  function getHashTab(){
    var hash = (window.location.hash || '').replace('#','');
    for (var i=0;i<tabIds.length;i++) if (tabIds[i] === hash) return hash;
    return 'dashboard';
  }

  document.addEventListener('click', function(e){
    var link = e.target.closest('[data-tab]');
    if (!link) return;
    e.preventDefault();
    var id = link.getAttribute('data-tab') || 'dashboard';
    if (history && history.replaceState) history.replaceState(null,'','#'+id); else window.location.hash = id;
    setActiveTab(id);
  });

  var showAllBtn = document.getElementById('showAllTabsBtn');
  if (showAllBtn) {
    showAllBtn.addEventListener('click', function(){
      if (history && history.replaceState) history.replaceState(null,'','#all'); else window.location.hash = '#all';
      setActiveTab('all');
    });
  }

  function initCollapsibleCards(){
    var cards = document.querySelectorAll('.card[id]');
    for (var i=0;i<cards.length;i++) {
      var card = cards[i];
      if (card.getAttribute('data-collapsible') === '1') continue;
      var h2 = card.querySelector('h2');
      if (!h2) continue;
      var body = document.createElement('div');
      body.className = 'card-body';
      while (h2.nextSibling) {
        body.appendChild(h2.nextSibling);
      }
      card.appendChild(body);
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'collapse-btn';
      btn.textContent = '折叠';
      btn.addEventListener('click', function(ev){
        ev.preventDefault();
        ev.stopPropagation();
        var thisCard = this.closest('.card');
        var thisBody = thisCard ? thisCard.querySelector('.card-body') : null;
        if (!thisBody) return;
        var collapsed = thisBody.classList.toggle('collapsed');
        this.textContent = collapsed ? '展开' : '折叠';
      });
      h2.appendChild(btn);
      card.setAttribute('data-collapsible', '1');
    }
  }

  function setAllCardsCollapsed(collapsed){
    var cards = document.querySelectorAll('.card[id]');
    for (var i=0;i<cards.length;i++) {
      var body = cards[i].querySelector('.card-body');
      var btn = cards[i].querySelector('.collapse-btn');
      if (!body || !btn) continue;
      if (collapsed) {
        body.classList.add('collapsed');
        btn.textContent = '展开';
      } else {
        body.classList.remove('collapsed');
        btn.textContent = '折叠';
      }
    }
  }

  var tableStates = [];
  function buildTableTools(table, state){
    var tools = document.createElement('div');
    tools.className = 'table-tools';
    tools.innerHTML = '<div class="left">'
      + '<label>每页</label>'
      + '<select class="page-size"><option value="10">10</option><option value="20">20</option><option value="50">50</option></select>'
      + '<button type="button" class="btn-muted prev">上一页</button>'
      + '<button type="button" class="btn-muted next">下一页</button>'
      + '</div>'
      + '<div class="right muted info"></div>';
    var select = tools.querySelector('.page-size');
    var prev = tools.querySelector('.prev');
    var next = tools.querySelector('.next');
    var info = tools.querySelector('.info');

    select.addEventListener('change', function(){
      state.size = parseInt(select.value,10) || 10;
      state.page = 1;
      renderTable(state);
    });
    prev.addEventListener('click', function(){
      if (state.page > 1) { state.page--; renderTable(state); }
    });
    next.addEventListener('click', function(){
      if (state.page < state.totalPages) { state.page++; renderTable(state); }
    });

    state.infoNode = info;
    state.prevBtn = prev;
    state.nextBtn = next;

    table.parentNode.insertBefore(tools, table);
  }

  function renderTable(state){
    var queryNode = document.getElementById('globalSearch');
    var q = queryNode ? (queryNode.value || '').toLowerCase().trim() : '';
    var filtered = [];
    for (var i=0;i<state.rows.length;i++) {
      var row = state.rows[i];
      var text = (row.innerText || row.textContent || '').toLowerCase();
      if (!q || text.indexOf(q) >= 0) filtered.push(row);
    }
    state.filtered = filtered;
    state.totalPages = Math.max(1, Math.ceil(filtered.length / state.size));
    if (state.page > state.totalPages) state.page = state.totalPages;
    if (state.page < 1) state.page = 1;

    for (var i=0;i<state.rows.length;i++) state.rows[i].style.display = 'none';
    var start = (state.page - 1) * state.size;
    var end = Math.min(start + state.size, filtered.length);
    for (var i=start;i<end;i++) filtered[i].style.display = '';

    if (state.infoNode) {
      if (filtered.length === 0) {
        state.infoNode.textContent = '无匹配数据';
      } else {
        state.infoNode.textContent = '第 ' + state.page + '/' + state.totalPages + ' 页 · 共 ' + filtered.length + ' 条';
      }
    }
    if (state.prevBtn) state.prevBtn.disabled = state.page <= 1;
    if (state.nextBtn) state.nextBtn.disabled = state.page >= state.totalPages;
  }

  function initTables(){
    var tables = document.querySelectorAll('.card table');
    for (var i=0;i<tables.length;i++) {
      var table = tables[i];
      if (table.getAttribute('data-enhanced') === '1') continue;
      var wrap = document.createElement('div');
      wrap.className = 'table-wrap';
      table.parentNode.insertBefore(wrap, table);
      wrap.appendChild(table);

      var body = table.tBodies && table.tBodies[0] ? table.tBodies[0] : null;
      if (!body) continue;
      var rows = Array.prototype.slice.call(body.rows || []);
      var state = {table:table, rows:rows, filtered:rows, page:1, size:10, totalPages:1, infoNode:null, prevBtn:null, nextBtn:null};
      buildTableTools(wrap, state);
      tableStates.push(state);
      table.setAttribute('data-enhanced','1');
      renderTable(state);
    }
  }

  function refreshAllTables(){
    for (var i=0;i<tableStates.length;i++) {
      if (tableStates[i].page !== 1) tableStates[i].page = 1;
      renderTable(tableStates[i]);
    }
  }

  var searchInput = document.getElementById('globalSearch');
  if (searchInput) {
    searchInput.addEventListener('input', refreshAllTables);
  }

  function insertBatchCheckboxes(table){
    if (!table || table.getAttribute('data-batch-init') === '1') return;
    var type = table.getAttribute('data-batch');
    var headRow = table.tHead && table.tHead.rows && table.tHead.rows[0] ? table.tHead.rows[0] : null;
    if (!headRow) return;
    var th = document.createElement('th');
    th.style.width = '36px';
    th.innerHTML = '<input type="checkbox" class="batch-all">';
    headRow.insertBefore(th, headRow.cells[0] || null);

    var body = table.tBodies && table.tBodies[0] ? table.tBodies[0] : null;
    if (!body) return;
    var rows = Array.prototype.slice.call(body.rows || []);
    for (var i=0;i<rows.length;i++) {
      var row = rows[i];
      if (!row.cells || row.cells.length === 0) continue;
      var idText = (row.cells[0].innerText || row.cells[0].textContent || '').trim();
      var td = document.createElement('td');
      td.innerHTML = '<input type="checkbox" class="batch-item" data-type="'+type+'" data-id="'+idText.replace(/"/g,'')+'">';
      row.insertBefore(td, row.cells[0] || null);
    }

    var all = th.querySelector('.batch-all');
    if (all) {
      all.addEventListener('change', function(){
        var cbs = table.querySelectorAll('.batch-item');
        for (var j=0;j<cbs.length;j++) cbs[j].checked = all.checked;
        refreshBatchCount();
      });
    }

    table.addEventListener('change', function(e){
      if (e.target && e.target.classList.contains('batch-item')) refreshBatchCount();
    });

    table.setAttribute('data-batch-init','1');
  }

  function initBatchTables(){
    insertBatchCheckboxes(document.getElementById('usersTable'));
    insertBatchCheckboxes(document.getElementById('groupsTable'));
    insertBatchCheckboxes(document.getElementById('devicesTable'));
    refreshBatchCount();
  }

  function selectedBatchItems(){
    var cbs = document.querySelectorAll('.batch-item:checked');
    var items = [];
    for (var i=0;i<cbs.length;i++) {
      items.push({type:cbs[i].getAttribute('data-type') || '', id:cbs[i].getAttribute('data-id') || ''});
    }
    return items;
  }

  function refreshBatchCount(){
    var items = selectedBatchItems();
    var countNode = document.getElementById('batchCount');
    if (countNode) countNode.textContent = '已选 ' + items.length + ' 项';
    syncBatchActions();
  }

  function detectBatchType(items){
    if (!items || items.length === 0) return '';
    var t = items[0].type;
    for (var i=1;i<items.length;i++) if (items[i].type !== t) return 'mixed';
    return t;
  }

  function syncBatchActions(){
    var items = selectedBatchItems();
    var autoType = detectBatchType(items);
    var typeSel = document.getElementById('batchType');
    var actionSel = document.getElementById('batchAction');
    if (!typeSel || !actionSel) return;

    var type = typeSel.value || autoType;
    if (autoType === 'mixed' && !typeSel.value) type = 'mixed';

    while (actionSel.options.length) actionSel.remove(0);
    var opts = [];
    if (type === 'user') opts = [['ban','封禁用户'],['unban','解封用户'],['deactivate','停用用户']];
    else if (type === 'group') opts = [['ban','封禁群'],['unban','解封群'],['delete','删除群']];
    else if (type === 'device') opts = [['ban','封禁设备'],['unban','解封设备']];
    else if (type === 'mixed') opts = [['','不能混合类型批量执行']];
    else opts = [['','请选择类型']];

    for (var i=0;i<opts.length;i++) {
      var op = document.createElement('option');
      op.value = opts[i][0];
      op.textContent = opts[i][1];
      actionSel.appendChild(op);
    }
  }

  function postForm(url, payload){
    return fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {'Content-Type':'application/x-www-form-urlencoded;charset=UTF-8'},
      body: new URLSearchParams(payload).toString()
    });
  }

  async function runBatch(){
    var items = selectedBatchItems();
    if (items.length === 0) { alert('请先勾选要批量处理的行'); return; }

    var typeSel = document.getElementById('batchType');
    var actionSel = document.getElementById('batchAction');
    var reason = document.getElementById('batchReason');
    var hours = document.getElementById('batchHours');
    var selectedType = typeSel ? (typeSel.value || detectBatchType(items)) : detectBatchType(items);

    if (!selectedType || selectedType === 'mixed') { alert('批量操作类型不一致，请只勾选同一类型'); return; }
    var action = actionSel ? actionSel.value : '';
    if (!action) { alert('请选择批量动作'); return; }

    if (!confirm('确认批量执行？共 ' + items.length + ' 项')) return;

    var ok = 0;
    for (var i=0;i<items.length;i++) {
      var item = items[i];
      if (item.type !== selectedType) continue;
      var url = '';
      var payload = {};

      if (selectedType === 'user') {
        if (action === 'ban') { url = '/admins/ban/user'; payload.user_uid = item.id; payload.reason = reason ? reason.value : ''; payload.duration_hours = hours ? hours.value : ''; }
        else if (action === 'unban') { url = '/admins/unban/user'; payload.user_uid = item.id; }
        else if (action === 'deactivate') { url = '/admins/user/deactivate'; payload.user_uid = item.id; payload.reason = reason ? reason.value : ''; }
      } else if (selectedType === 'group') {
        if (action === 'ban') { url = '/admins/group/ban'; payload.group_id = item.id; payload.reason = reason ? reason.value : ''; payload.duration_hours = hours ? hours.value : ''; }
        else if (action === 'unban') { url = '/admins/group/unban'; payload.group_id = item.id; }
        else if (action === 'delete') { url = '/admins/group/delete'; payload.group_id = item.id; }
      } else if (selectedType === 'device') {
        if (action === 'ban') { url = '/admins/ban/device'; payload.device_id = item.id; payload.reason = reason ? reason.value : ''; payload.duration_hours = hours ? hours.value : ''; }
        else if (action === 'unban') { url = '/admins/unban/device'; payload.device_id = item.id; }
      }
      if (!url) continue;

      try {
        var resp = await postForm(url, payload);
        if (resp && (resp.status >= 200 && resp.status < 400)) ok++;
      } catch (e) {}
    }

    alert('批量执行完成：成功 ' + ok + ' / ' + items.length + '。页面即将刷新');
    window.location.reload();
  }

  function clearBatch(){
    var cbs = document.querySelectorAll('.batch-item, .batch-all');
    for (var i=0;i<cbs.length;i++) cbs[i].checked = false;
    refreshBatchCount();
  }

  function openModal(templateId, title){
    var mask = document.getElementById('adminModalMask');
    var body = document.getElementById('adminModalBody');
    var titleNode = document.getElementById('adminModalTitle');
    var tpl = document.getElementById(templateId);
    if (!mask || !body || !tpl) return;
    body.innerHTML = '';
    var node = document.importNode(tpl.content, true);
    body.appendChild(node);
    titleNode.textContent = title || '操作';
    mask.style.display = 'flex';
  }

  function closeModal(){
    var mask = document.getElementById('adminModalMask');
    if (mask) mask.style.display = 'none';
  }

  document.addEventListener('click', function(e){
    var btn = e.target.closest('[data-open-modal]');
    if (btn) {
      var key = btn.getAttribute('data-open-modal');
      if (key === 'modalCreateUser') openModal('tplCreateUser','创建测试用户');
      else if (key === 'modalBanUser') openModal('tplBanUser','封禁用户');
      else if (key === 'modalSendNotice') openModal('tplSendNotice','发送系统通知');
      return;
    }
    if (e.target && e.target.id === 'adminModalMask') closeModal();
  });

  var closeBtn = document.getElementById('adminModalClose');
  if (closeBtn) closeBtn.addEventListener('click', closeModal);

  var expandBtn = document.getElementById('expandAllBtn');
  if (expandBtn) expandBtn.addEventListener('click', function(){ setAllCardsCollapsed(false); });
  var collapseBtn = document.getElementById('collapseAllBtn');
  if (collapseBtn) collapseBtn.addEventListener('click', function(){ setAllCardsCollapsed(true); });

  var batchRunBtn = document.getElementById('batchRunBtn');
  if (batchRunBtn) batchRunBtn.addEventListener('click', runBatch);
  var batchClearBtn = document.getElementById('batchClearBtn');
  if (batchClearBtn) batchClearBtn.addEventListener('click', clearBatch);
  var batchType = document.getElementById('batchType');
  if (batchType) batchType.addEventListener('change', syncBatchActions);

  initCollapsibleCards();
  initTables();
  initBatchTables();

  var initial = getHashTab();
  if ((window.location.hash || '').replace('#','') === 'all') setActiveTab('all'); else setActiveTab(initial);
})();
</script>

</div>
</div>
</body>
</html>`

