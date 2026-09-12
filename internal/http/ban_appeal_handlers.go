package httpapi

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"metrochat/internal/auth"
	"metrochat/internal/data"
)

type iGotBannedPageData struct {
	Error      string
	Success    string
	Username   string
	AppealText string
	Contact    string
	UID        string
	BanReason  string
	BanExpiry  string
}

var iGotBannedTpl = template.Must(template.New("igotbanned").Parse(iGotBannedHTML))

func (a *API) handleIGotBannedPage(w http.ResponseWriter, r *http.Request) {
	a.renderIGotBannedPage(w, iGotBannedPageData{})
}

func (a *API) handleIGotBannedSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !a.ipLimiter.Allow("igotbanned:" + clientIP(r)) {
		a.renderIGotBannedPage(w, iGotBannedPageData{Error: "提交过于频繁，请稍后再试"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		a.renderIGotBannedPage(w, iGotBannedPageData{Error: "提交失败，请稍后再试"})
		return
	}

	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := strings.TrimSpace(r.FormValue("password"))
	appealText := sanitizeBanAppealText(r.FormValue("appeal_text"))
	contact := sanitizeBanAppealContact(r.FormValue("contact"))
	pageData := iGotBannedPageData{
		Username:   username,
		AppealText: appealText,
		Contact:    contact,
	}

	if username == "" || password == "" {
		pageData.Error = "请输入用户名和密码"
		a.renderIGotBannedPage(w, pageData)
		return
	}
	if !isValidUsername(username) {
		pageData.Error = "用户名格式不正确"
		a.renderIGotBannedPage(w, pageData)
		return
	}
	if appealText == "" {
		pageData.Error = "请填写申诉内容"
		a.renderIGotBannedPage(w, pageData)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	user, err := a.users.GetByEmailOrUsername(ctx, username)
	if err != nil {
		_, _ = auth.VerifyPassword(password, dummyHash)
		pageData.Error = "账号或密码错误"
		a.renderIGotBannedPage(w, pageData)
		return
	}
	ok, err := auth.VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		pageData.Error = "账号或密码错误"
		a.renderIGotBannedPage(w, pageData)
		return
	}

	banned, err := a.devices.IsUserBanned(ctx, user.ID)
	if err != nil {
		pageData.Error = "系统繁忙，请稍后再试"
		a.renderIGotBannedPage(w, pageData)
		return
	}
	if !banned {
		pageData.Error = "该账号当前未封禁，无需提交申诉"
		a.renderIGotBannedPage(w, pageData)
		return
	}

	hasPending, err := a.banAppeals.HasPendingByUser(ctx, user.ID)
	if err != nil {
		pageData.Error = "系统繁忙，请稍后再试"
		a.renderIGotBannedPage(w, pageData)
		return
	}
	if hasPending {
		pageData.Error = "你已有待处理申诉，请勿重复提交"
		a.renderIGotBannedPage(w, pageData)
		return
	}

	banReason := ""
	banExpiry := ""
	banUntil := time.Time{}
	if banRow, banErr := a.devices.GetBannedUserByUserID(ctx, user.ID); banErr == nil && banRow != nil {
		banReason = strings.TrimSpace(banRow.Reason)
		if banRow.BannedUntil.Valid {
			banUntil = banRow.BannedUntil.Time
		}
		banExpiry = formatBanExpiry(banRow.BannedUntil)
	}

	appeal := &data.BanAppeal{
		UserID:     user.ID,
		UID:        user.UID,
		Username:   user.Username,
		BanReason:  banReason,
		AppealText: appealText,
		Contact:    contact,
		Status:     "pending",
	}
	if !banUntil.IsZero() {
		appeal.BannedUntil.Valid = true
		appeal.BannedUntil.Time = banUntil
	}
	if err := a.banAppeals.Create(ctx, appeal); err != nil {
		pageData.Error = "提交失败，请稍后重试"
		a.renderIGotBannedPage(w, pageData)
		return
	}

	pageData.Success = "申诉已提交，管理员会尽快审核"
	pageData.UID = user.UID
	pageData.BanReason = banReason
	pageData.BanExpiry = banExpiry
	pageData.AppealText = ""
	a.renderIGotBannedPage(w, pageData)
}

func (a *API) handleAdminBanAppealStatus(w http.ResponseWriter, r *http.Request) {
	if !a.ensureAdminPost(w, r) {
		return
	}
	appealID := strings.TrimSpace(r.FormValue("id"))
	status := normalizeBanAppealStatusInput(r.FormValue("status"))
	adminNote := strings.TrimSpace(r.FormValue("admin_note"))
	unbanOnApprove := strings.TrimSpace(r.FormValue("unban_on_approve"))
	if appealID == "" {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("缺少申诉ID")+"#banappeals", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	appeal, err := a.banAppeals.GetByID(ctx, appealID)
	if err != nil {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("申诉不存在")+"#banappeals", http.StatusSeeOther)
		return
	}

	if err = a.banAppeals.SetStatus(ctx, appealID, status, adminNote); err != nil {
		http.Redirect(w, r, "/admins?err="+url.QueryEscape("申诉处理失败")+"#banappeals", http.StatusSeeOther)
		return
	}

	shouldUnban := status == "approved"
	if shouldUnban {
		if unbanOnApprove == "0" || strings.EqualFold(unbanOnApprove, "false") {
			shouldUnban = false
		}
	}
	if shouldUnban && appeal != nil && strings.TrimSpace(appeal.UserID) != "" {
		_ = a.devices.UnbanUser(ctx, appeal.UserID)
	}

	okMsg := "申诉状态已更新"
	if shouldUnban {
		okMsg = "申诉已通过并解封账号"
	}
	http.Redirect(w, r, "/admins?ok="+url.QueryEscape(okMsg)+"#banappeals", http.StatusSeeOther)
}

func (a *API) renderIGotBannedPage(w http.ResponseWriter, pageData iGotBannedPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = iGotBannedTpl.Execute(w, pageData)
}

func sanitizeBanAppealText(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if utf8.RuneCountInString(text) > 1000 {
		runes := []rune(text)
		text = string(runes[:1000])
	}
	return text
}

func sanitizeBanAppealContact(value string) string {
	contact := strings.TrimSpace(value)
	if contact == "" {
		return ""
	}
	contact = strings.ReplaceAll(contact, "\r", " ")
	contact = strings.ReplaceAll(contact, "\n", " ")
	if utf8.RuneCountInString(contact) > 120 {
		runes := []rune(contact)
		contact = string(runes[:120])
	}
	return contact
}

func formatBanExpiry(v sql.NullTime) string {
	if v.Valid {
		return v.Time.Format("2006-01-02 15:04")
	}
	return "永久"
}

func normalizeBanAppealStatusInput(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "pending", "open", "todo":
		return "pending"
	case "approved", "approve", "pass", "accepted":
		return "approved"
	case "rejected", "reject", "deny", "declined":
		return "rejected"
	default:
		return "pending"
	}
}

const iGotBannedHTML = `
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>旧聊封禁申诉</title>
  <style>
    :root{--bg:#f7f1e8;--ink:#1d2730;--muted:#6d7680;--panel:#fffdf8;--border:#e7d9c8;--accent:#0f5b72;--accent2:#0c4354;--danger:#b4233c;--ok:#1f8f55;--shadow:0 22px 48px rgba(20,34,44,.18);} 
    *{box-sizing:border-box;}
    body{margin:0;min-height:100vh;background:
      radial-gradient(900px 460px at -10% 0%, #f2ddc6 0%, rgba(242,221,198,0) 60%),
      radial-gradient(860px 480px at 100% 0%, #d8ecf1 0%, rgba(216,236,241,0) 60%),
      var(--bg);color:var(--ink);font-family:"Noto Sans SC","PingFang SC","Microsoft YaHei",sans-serif;}
    .wrap{max-width:860px;margin:0 auto;padding:34px 16px 48px;}
    .panel{background:var(--panel);border:1px solid var(--border);border-radius:20px;box-shadow:var(--shadow);overflow:hidden;}
    .hero{padding:22px 24px;background:linear-gradient(130deg,#f7efe4 0%,#edf5f7 100%);border-bottom:1px solid var(--border);} 
    h1{margin:0;font-size:24px;letter-spacing:.3px;}
    .sub{margin:8px 0 0;color:var(--muted);font-size:13px;line-height:1.7;}
    .body{padding:22px 24px;display:grid;gap:14px;}
    label{display:block;font-size:12px;color:var(--muted);text-transform:uppercase;letter-spacing:.08em;margin:2px 0 6px;}
    input,textarea{width:100%;padding:11px 12px;border:1px solid var(--border);border-radius:11px;background:#fff;color:var(--ink);font-size:14px;transition:border-color .2s,box-shadow .2s;}
    textarea{min-height:128px;resize:vertical;line-height:1.65;white-space:pre-wrap;}
    input:focus,textarea:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px rgba(15,91,114,.14);} 
    .btn{border:0;border-radius:11px;padding:12px 18px;background:linear-gradient(135deg,var(--accent),var(--accent2));color:#fff;font-size:15px;font-weight:700;cursor:pointer;box-shadow:0 10px 24px rgba(15,91,114,.24);} 
    .btn:hover{transform:translateY(-1px);} 
    .msg{border-radius:12px;padding:11px 12px;font-size:13px;line-height:1.6;}
    .msg.err{background:rgba(180,35,60,.1);border:1px solid rgba(180,35,60,.26);color:var(--danger);} 
    .msg.ok{background:rgba(31,143,85,.12);border:1px solid rgba(31,143,85,.3);color:var(--ok);} 
    .note{font-size:12px;color:var(--muted);line-height:1.7;border-top:1px dashed var(--border);padding-top:12px;}
    .badge{display:inline-flex;align-items:center;padding:3px 9px;border-radius:999px;background:#e9f2f5;color:#144759;font-size:12px;margin-right:8px;}
    .meta{display:grid;gap:8px;}
    .meta-item{font-size:13px;color:var(--muted);} 
    .meta-item strong{color:var(--ink);} 
  </style>
</head>
<body>
  <div class="wrap">
    <div class="panel">
      <div class="hero">
        <h1>封禁申诉通道</h1>
        <p class="sub">请使用被封禁账号的 <strong>用户名 + 密码</strong> 登录后提交申诉。系统会保留提交记录，管理员可在后台直接审核。</p>
      </div>
      <div class="body">
        {{if .Success}}
        <div class="msg ok">
          <div>{{.Success}}</div>
          <div class="meta" style="margin-top:6px;">
            <div class="meta-item"><span class="badge">账号</span><strong>{{if .UID}}{{.UID}}{{else}}-{{end}}</strong></div>
            <div class="meta-item"><span class="badge">封禁原因</span><strong>{{if .BanReason}}{{.BanReason}}{{else}}未记录{{end}}</strong></div>
            <div class="meta-item"><span class="badge">封禁到期</span><strong>{{if .BanExpiry}}{{.BanExpiry}}{{else}}未知{{end}}</strong></div>
          </div>
        </div>
        {{end}}
        {{if .Error}}<div class="msg err">{{.Error}}</div>{{end}}

        <form method="post" action="/igotbanned">
          <label>用户名</label>
          <input name="username" value="{{.Username}}" placeholder="请输入被封禁账号用户名" required>

          <label>密码</label>
          <input name="password" type="password" placeholder="请输入账号密码" required>

          <label>申诉内容</label>
          <textarea name="appeal_text" placeholder="请说明封禁经过、申诉理由，以及你愿意如何整改。" required>{{.AppealText}}</textarea>

          <label>联系方式（可选）</label>
          <input name="contact" value="{{.Contact}}" placeholder="例如 QQ / 邮箱 / 备用账号">

          <div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap;">
            <button type="submit" class="btn">提交申诉</button>
          </div>
        </form>

        <div class="note">
          审核说明：同一账号存在待处理申诉时，不可重复提交。申诉通过后可由管理员直接解封；驳回后可补充新证据再次提交。
        </div>
      </div>
    </div>
  </div>
</body>
</html>`
