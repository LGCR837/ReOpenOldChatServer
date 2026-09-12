package httpapi

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"

	"metrochat/internal/auth"
	"metrochat/internal/data"
)

const shopTokenCookie = "shop_token"
const customTitlePrice = 200

const titleShopHTML = `
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>旧聊称号商店</title>
  <style>
    :root{--bg:#0f172a;--card:#111827;--muted:#94a3b8;--text:#f8fafc;--border:#1f2937;--accent:#38bdf8;--success:#22c55e;--danger:#ef4444;--shadow:0 18px 38px rgba(0,0,0,.35);} 
    *{box-sizing:border-box;}
    body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:var(--bg);color:var(--text);}
    .wrap{max-width:980px;margin:0 auto;padding:28px 18px 46px;}
    .hero{background:linear-gradient(135deg,#0b1220 0%,#111827 100%);border:1px solid var(--border);border-radius:18px;padding:22px;box-shadow:var(--shadow);} 
    .title{font-size:26px;font-weight:700;margin:0;}
    .sub{margin:8px 0 0;color:var(--muted);font-size:13px;line-height:1.6;}
    .user-card{margin-top:14px;display:grid;gap:12px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));}
    .pill{background:#0f172a;border:1px solid var(--border);border-radius:12px;padding:10px 12px;font-size:13px;color:var(--muted);} 
    .pill strong{color:var(--text);}
    .banner{margin-top:14px;padding:10px 12px;border-radius:12px;font-size:13px;}
    .banner.ok{background:rgba(34,197,94,.15);color:#bbf7d0;border:1px solid rgba(34,197,94,.3);} 
    .banner.err{background:rgba(239,68,68,.15);color:#fecaca;border:1px solid rgba(239,68,68,.3);} 
    .grid{margin-top:20px;display:grid;gap:14px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));}
    .card{background:var(--card);border:1px solid var(--border);border-radius:16px;padding:16px;display:flex;flex-direction:column;gap:10px;}
    .card h3{margin:0;font-size:16px;}
    .price{font-size:14px;color:var(--muted);} 
    .btn{display:inline-flex;align-items:center;justify-content:center;padding:10px 14px;border-radius:10px;border:1px solid transparent;font-weight:600;font-size:13px;cursor:pointer;}
    .btn.primary{background:var(--accent);color:#0b1220;}
    .btn.disabled{background:#1f2937;color:#64748b;cursor:not-allowed;}
    .footer{margin-top:18px;color:var(--muted);font-size:12px;line-height:1.6;}
  </style>
</head>
<body>
  <div class="wrap">
    <section class="hero">
      <h1 class="title">旧聊称号商店</h1>
      <p class="sub">在这里购买专属称号。更换称号会自动退还上一枚称号的购买旧币（未标价的强制称号默认按 100 旧币计）。</p>

      {{if .LoggedIn}}
      <div class="user-card">
        <div class="pill">账号：<strong>{{.UID}}</strong></div>
        <div class="pill">余额：<strong>{{.CoinBalance}}</strong> 旧币</div>
        <div class="pill">当前称号：<strong>{{if .UserTitle}}{{.UserTitle}}{{else}}无{{end}}</strong></div>
      </div>
      <div class="banner ok" style="margin-top:10px;display:flex;align-items:center;justify-content:space-between;gap:12px;">
        <span>已登录，可直接购买称号。</span>
        <a href="/shop/logout" style="color:#bbf7d0;text-decoration:underline;">退出登录</a>
      </div>
      {{else}}
      <div class="banner err">请先登录后购买。</div>
      <form method="post" action="/shop/login" style="margin-top:12px;display:grid;gap:10px;">
        <input name="identifier" placeholder="账号 / 邮箱" style="padding:10px 12px;border-radius:10px;border:1px solid var(--border);background:#0b1220;color:var(--text);">
        <input name="password" type="password" placeholder="密码" style="padding:10px 12px;border-radius:10px;border:1px solid var(--border);background:#0b1220;color:var(--text);">
        <div style="display:flex;gap:10px;flex-wrap:wrap;">
          <button class="btn primary" type="submit">登录</button>
        </div>
      </form>
      {{end}}

      {{if .Success}}
      <div class="banner ok">{{.Success}}</div>
      {{end}}
      {{if .Error}}
      <div class="banner err">{{.Error}}</div>
      {{end}}
    </section>

    <section class="grid">
      {{range .Titles}}
      <div class="card">
        <h3>{{.Title}}</h3>
        <div class="price">价格：{{.Price}} 旧币</div>
        {{if $.LoggedIn}}
        <form method="post" action="/shop/report">
          <input type="hidden" name="title_id" value="{{.ID}}">
          <input type="hidden" name="token" value="{{$.Token}}">
          <button class="btn primary" type="submit">购买</button>
        </form>
        {{else}}
        <button class="btn disabled" type="button">请先登录</button>
        {{end}}
      </div>
      {{else}}
      <div class="card">暂无可购买称号</div>
      {{end}}
    </section>

    <section class="card" style="margin-top:18px;">
      <h3>自定义称号</h3>
      <div class="price">定价：200 旧币 / 个（全服唯一，不可与已有称号重复）</div>
      {{if .LoggedIn}}
      <form method="post" action="/shop/report" style="display:flex;gap:10px;flex-wrap:wrap;">
        <input name="custom_title" placeholder="输入自定义称号（不超过20字）" style="flex:1;min-width:220px;padding:10px 12px;border-radius:10px;border:1px solid var(--border);background:#0b1220;color:var(--text);">
        <button class="btn primary" type="submit">购买</button>
      </form>
      {{else}}
      <div class="price" style="margin-top:6px;">请先登录后购买</div>
      {{end}}
    </section>

    <div class="footer">提示：购买后称号立即生效；同名称号不可重复购买。</div>
  </div>
</body>
</html>
`

var titleShopTpl = template.Must(template.New("title_shop").Parse(titleShopHTML))

type titleShopData struct {
	Error       string
	Success     string
	LoggedIn    bool
	Token       string
	UID         string
	UserTitle   string
	CoinBalance int
	Titles      []data.TitleCatalogRow
}

func (a *API) handleLanding(w http.ResponseWriter, r *http.Request) {
	a.renderTitleShop(w, r, "", "")
}

func (a *API) handleReportPage(w http.ResponseWriter, r *http.Request) {
	a.renderTitleShop(w, r, "", "")
}

func (a *API) handleReportSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		a.renderTitleShop(w, r, "提交失败，请稍后再试", "")
		return
	}
	customRaw := strings.TrimSpace(r.FormValue("custom_title"))
	titleID := strings.TrimSpace(r.FormValue("title_id"))
	token := extractShopToken(r)
	if token == "" {
		a.renderTitleShop(w, r, "请先登录后再购买", "")
		return
	}
	if titleID == "" && customRaw == "" {
		a.renderTitleShop(w, r, "请选择要购买的称号", "")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	user, authErr := a.shopUserFromToken(ctx, token)
	if authErr != "" || user == nil {
		a.renderTitleShop(w, r, authErr, "")
		return
	}

	if customRaw != "" {
		customTitle := sanitizeShopTitle(customRaw)
		if customTitle == "" {
			a.renderTitleShop(w, r, "称号不能为空", "")
			return
		}
		err := a.purchaseCustomTitle(ctx, user, customTitle)
		if err != "" {
			a.renderTitleShop(w, r, err, "")
			return
		}
		a.renderTitleShop(w, r, "", "购买成功，称号已生效")
		return
	}

	titleRow, err := a.titles.GetByID(ctx, titleID)
	if err != nil || titleRow == nil || titleRow.Active == 0 || titleRow.IsCustom != 0 {
		a.renderTitleShop(w, r, "称号不可用或已下架", "")
		return
	}
	if titleRow.OwnerID.Valid && strings.TrimSpace(titleRow.OwnerID.String) != "" {
		a.renderTitleShop(w, r, "该称号已被购买", "")
		return
	}
	price := titleRow.Price
	if price <= 0 {
		price = 100
	}

	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		a.renderTitleShop(w, r, "系统繁忙，请稍后再试", "")
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	type userRow struct {
		UserTitle      string `db:"user_title"`
		UserTitlePrice int    `db:"user_title_price"`
		CoinBalance    int    `db:"coin_balance"`
	}

	var current userRow
	if err = tx.GetContext(ctx, &current, `SELECT user_title, user_title_price, coin_balance FROM users WHERE id = $1`, user.ID); err != nil {
		a.renderTitleShop(w, r, "账号信息读取失败", "")
		return
	}
	if strings.TrimSpace(current.UserTitle) == strings.TrimSpace(titleRow.Title) {
		a.renderTitleShop(w, r, "该称号已拥有，无需重复购买", "")
		return
	}

	refund := 0
	if strings.TrimSpace(current.UserTitle) != "" {
		refund = current.UserTitlePrice
		if refund <= 0 {
			refund = 100
		}
	}

	newBalance := current.CoinBalance + refund - price
	if newBalance < 0 {
		a.renderTitleShop(w, r, "旧币不足", "")
		return
	}

	if err := releaseOldTitleToMarket(ctx, tx, strings.TrimSpace(current.UserTitle), current.UserTitlePrice, user.ID); err != nil {
		a.renderTitleShop(w, r, "购买失败，请稍后再试", "")
		return
	}

	res, err := tx.ExecContext(ctx, `
UPDATE title_catalog
SET owner_id = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2 AND owner_id IS NULL
`, user.ID, titleRow.ID)
	if err != nil {
		a.renderTitleShop(w, r, "购买失败，请稍后再试", "")
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		a.renderTitleShop(w, r, "该称号已被购买", "")
		return
	}

	_, err = tx.ExecContext(ctx, `
UPDATE users
SET user_title = $1, user_title_price = $2, coin_balance = $3, updated_at = CURRENT_TIMESTAMP
WHERE id = $4
`, titleRow.Title, price, newBalance, user.ID)
	if err != nil {
		a.renderTitleShop(w, r, "购买失败，请稍后再试", "")
		return
	}

	if err = tx.Commit(); err != nil {
		a.renderTitleShop(w, r, "购买失败，请稍后再试", "")
		return
	}
	committed = true

	a.renderTitleShop(w, r, "", "购买成功，称号已生效")
}

func (a *API) handleShopLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !a.ipLimiter.Allow("shop_login:" + clientIP(r)) {
		a.renderTitleShop(w, r, "登录过于频繁，请稍后再试", "")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		a.renderTitleShop(w, r, "登录失败，请稍后再试", "")
		return
	}
	identifier := strings.ToLower(strings.TrimSpace(r.FormValue("identifier")))
	password := strings.TrimSpace(r.FormValue("password"))
	if identifier == "" || password == "" {
		a.renderTitleShop(w, r, "请输入账号和密码", "")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByEmailOrUsername(ctx, identifier)
	if err != nil {
		if err == data.ErrNotFound {
			_, _ = auth.VerifyPassword(password, dummyHash)
			a.renderTitleShop(w, r, "账号或密码错误", "")
			return
		}
		a.renderTitleShop(w, r, "登录失败，请稍后再试", "")
		return
	}

	ok, err := auth.VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		a.renderTitleShop(w, r, "账号或密码错误", "")
		return
	}

	if banned, err := a.devices.IsUserBanned(ctx, user.ID); err == nil && banned {
		a.renderTitleShop(w, r, "账号已被封禁", "")
		return
	}

	tokens, err := a.issueTokens(ctx, user)
	if err != nil {
		a.renderTitleShop(w, r, "登录失败，请稍后再试", "")
		return
	}

	setShopTokenCookie(w, r, tokens.AccessToken, a.cfg.AccessTokenTTL)
	http.Redirect(w, r, "/shop", http.StatusSeeOther)
}

func (a *API) handleShopLogout(w http.ResponseWriter, r *http.Request) {
	clearShopTokenCookie(w, r)
	http.Redirect(w, r, "/shop", http.StatusSeeOther)
}

func (a *API) renderTitleShop(w http.ResponseWriter, r *http.Request, errMsg, okMsg string) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	titles, _ := a.titles.ListAvailable(ctx)
	token := extractShopToken(r)

	data := titleShopData{
		Error:   errMsg,
		Success: okMsg,
		Token:   token,
		Titles:  titles,
	}

	if token != "" {
		user, authErr := a.shopUserFromToken(ctx, token)
		if authErr != "" {
			if data.Error == "" {
				data.Error = authErr
			}
		} else if user != nil {
			data.LoggedIn = true
			data.UID = user.UID
			data.UserTitle = user.UserTitle
			data.CoinBalance = user.CoinBalance
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = titleShopTpl.Execute(w, data)
}

func extractShopToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header != "" {
		parts := strings.SplitN(header, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		return token
	}
	if token := strings.TrimSpace(r.FormValue("token")); token != "" {
		return token
	}
	if cookie, err := r.Cookie(shopTokenCookie); err == nil {
		return strings.TrimSpace(cookie.Value)
	}
	return ""
}

func (a *API) shopUserFromToken(ctx context.Context, token string) (*data.User, string) {
	if token == "" {
		return nil, ""
	}
	claims, ok := a.authenticateFromToken(token)
	if !ok || claims == nil {
		return nil, "登录已失效，请重新登录"
	}
	if !a.validateTokenVersion(ctx, claims.Subject, claims.Version) {
		return nil, "登录已失效，请重新登录"
	}
	banned, err := a.devices.IsUserBanned(ctx, claims.Subject)
	if err == nil && banned {
		return nil, "账号已被封禁"
	}
	user, err := a.users.GetByID(ctx, claims.Subject)
	if err != nil {
		return nil, "账号不存在"
	}
	return user, ""
}

func (a *API) purchaseCustomTitle(ctx context.Context, user *data.User, title string) string {
	if user == nil {
		return "账号无效"
	}
	tx, err := a.db.BeginTxx(ctx, nil)
	if err != nil {
		return "系统繁忙，请稍后再试"
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var exists string
	err = tx.GetContext(ctx, &exists, `SELECT id FROM title_catalog WHERE title = $1 LIMIT 1`, title)
	if err == nil {
		return "该称号已存在"
	}
	if err != sql.ErrNoRows {
		return "系统繁忙，请稍后再试"
	}

	err = tx.GetContext(ctx, &exists, `SELECT id FROM users WHERE user_title = $1 LIMIT 1`, title)
	if err == nil {
		return "该称号已被使用"
	}
	if err != sql.ErrNoRows {
		return "系统繁忙，请稍后再试"
	}

	var current struct {
		UserTitle      string `db:"user_title"`
		UserTitlePrice int    `db:"user_title_price"`
		CoinBalance    int    `db:"coin_balance"`
	}
	if err = tx.GetContext(ctx, &current, `SELECT user_title, user_title_price, coin_balance FROM users WHERE id = $1`, user.ID); err != nil {
		return "账号信息读取失败"
	}
	if strings.TrimSpace(current.UserTitle) == title {
		return "该称号已拥有，无需重复购买"
	}

	refund := 0
	if strings.TrimSpace(current.UserTitle) != "" {
		refund = current.UserTitlePrice
		if refund <= 0 {
			refund = 100
		}
	}
	newBalance := current.CoinBalance + refund - customTitlePrice
	if newBalance < 0 {
		return "旧币不足"
	}

	if err := releaseOldTitleToMarket(ctx, tx, strings.TrimSpace(current.UserTitle), current.UserTitlePrice, user.ID); err != nil {
		return "购买失败，请稍后再试"
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO title_catalog (id, title, price, active, is_custom, owner_id, created_at, updated_at)
VALUES ($1, $2, $3, 1, 1, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
`, data.NewID(), title, customTitlePrice, user.ID)
	if err != nil {
		if strings.Contains(err.Error(), "title_catalog.title") {
			return "该称号已存在"
		}
		return "购买失败，请稍后再试"
	}

	_, err = tx.ExecContext(ctx, `
UPDATE users
SET user_title = $1, user_title_price = $2, coin_balance = $3, updated_at = CURRENT_TIMESTAMP
WHERE id = $4
`, title, customTitlePrice, newBalance, user.ID)
	if err != nil {
		return "购买失败，请稍后再试"
	}

	if err = tx.Commit(); err != nil {
		return "购买失败，请稍后再试"
	}
	committed = true
	return ""
}

func releaseOldTitleToMarket(ctx context.Context, tx *sqlx.Tx, oldTitle string, oldPrice int, userID string) error {
	if tx == nil || strings.TrimSpace(oldTitle) == "" {
		return nil
	}
	var old struct {
		ID       string `db:"id"`
		IsCustom int    `db:"is_custom"`
	}
	err := tx.GetContext(ctx, &old, `SELECT id, is_custom FROM title_catalog WHERE title = $1`, oldTitle)
	if err == nil && old.ID != "" {
		if old.IsCustom != 0 {
			_, _ = tx.ExecContext(ctx, `
UPDATE title_catalog
SET owner_id = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND (owner_id = $2 OR owner_id IS NULL)
`, old.ID, userID)
			return nil
		}
		_, _ = tx.ExecContext(ctx, `
UPDATE title_catalog
SET owner_id = NULL, active = 1, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND (owner_id = $2 OR owner_id IS NULL)
`, old.ID, userID)
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	if oldPrice <= 0 {
		oldPrice = 100
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO title_catalog (id, title, price, active, is_custom, owner_id, created_at, updated_at)
VALUES ($1, $2, $3, 1, 0, NULL, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
`, data.NewID(), oldTitle, oldPrice)
	return err
}

func sanitizeShopTitle(value string) string {
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

func setShopTokenCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	if w == nil {
		return
	}
	secure := isHTTPS(r)
	cookie := &http.Cookie{
		Name:     shopTokenCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	if ttl > 0 {
		cookie.MaxAge = int(ttl.Seconds())
		cookie.Expires = time.Now().Add(ttl)
	}
	http.SetCookie(w, cookie)
}

func clearShopTokenCookie(w http.ResponseWriter, r *http.Request) {
	if w == nil {
		return
	}
	secure := isHTTPS(r)
	http.SetCookie(w, &http.Cookie{
		Name:     shopTokenCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
