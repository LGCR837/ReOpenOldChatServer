package httpapi

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"metrochat/internal/data"
	"metrochat/internal/mail"
	"metrochat/internal/verify"
)

type captchaResponse struct {
	CaptchaID   string `json:"captcha_id"`
	ImageBase64 string `json:"image_base64"`
}

type emailCodeRequest struct {
	Email       string `json:"email"`
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
	Username    string `json:"username"`
}

func (a *API) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	code := verify.RandomDigits(5)
	a.captchas.PruneExpired()
	id := a.captchas.New(code, 5*time.Minute)
	img, err := verify.RenderCaptchaPNG(code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "captcha_failed", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, captchaResponse{
		CaptchaID:   id,
		ImageBase64: base64.StdEncoding.EncodeToString(img),
	})
}

func (a *API) handleEmailCode(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	ip := clientIP(r)
	if !a.ipLimiter.Allow("email:" + ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	var req emailCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	captchaID := strings.TrimSpace(req.CaptchaID)
	captchaCode := strings.TrimSpace(req.CaptchaCode)
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if !isValidEmail(email) {
		writeError(w, http.StatusBadRequest, "invalid_email", "invalid email")
		return
	}
	if username == "" && !isQQEmail(email) {
		writeError(w, http.StatusBadRequest, "invalid_email_domain", "only qq email allowed")
		return
	}
	if username != "" {
		user, err := a.users.GetByEmailOrUsername(r.Context(), username)
		if err != nil {
			if err == data.ErrNotFound {
				writeError(w, http.StatusBadRequest, "invalid_account", "invalid account")
				return
			}
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		if !strings.EqualFold(user.Email, email) || !strings.EqualFold(user.Username, username) {
			writeError(w, http.StatusBadRequest, "invalid_account", "invalid account")
			return
		}
	}
	if captchaID == "" || captchaCode == "" || !a.captchas.Verify(captchaID, captchaCode) {
		writeError(w, http.StatusBadRequest, "invalid_captcha", "invalid captcha")
		return
	}

	if !a.cfg.EmailVerifyEnabled {
		writeError(w, http.StatusBadRequest, "email_verify_disabled", "未开启邮箱验证请乱填")
		return
	}

	a.sendLimiter.PruneOlderThan(10 * time.Minute)
	ok, wait := a.sendLimiter.Allow(email, 120*time.Second)
	if !ok {
		if wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())))
		}
		writeError(w, http.StatusTooManyRequests, "email_cooldown", "cooldown")
		return
	}

	code := verify.RandomDigits(6)
	if err := mail.SendCode(email, code); err != nil {
		writeError(w, http.StatusInternalServerError, "mail_failed", "send mail failed")
		return
	}
	a.emailCodes.PruneExpired()
	a.emailCodes.Set(email, code, 10*time.Minute)
	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}
