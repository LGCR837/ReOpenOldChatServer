package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/aidarkhanov/nanoid"

	"metrochat/internal/auth"
	"metrochat/internal/data"
)

type registerRequest struct {
	Email      string `json:"email"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	EmailCode  string `json:"email_code"`
	DeviceID   string `json:"device_id"`
	IMEI       string `json:"imei"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceID   string `json:"device_id"`
	IMEI       string `json:"imei"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type resetPasswordRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	EmailCode   string `json:"email_code"`
	NewPassword string `json:"new_password"`
}

type userResponse struct {
	ID          string `json:"id"`
	UID         string `json:"uid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	UserTitle   string `json:"user_title"`
	AvatarURL   string `json:"avatar_url"`
	Signature   string `json:"signature"`
	CoverURL    string `json:"cover_url"`
}

type selfUserResponse struct {
	ID              string `json:"id"`
	UID             string `json:"uid"`
	Username        string `json:"username"`
	DisplayName     string `json:"display_name"`
	UserTitle       string `json:"user_title"`
	AvatarURL       string `json:"avatar_url"`
	Signature       string `json:"signature"`
	CoverURL        string `json:"cover_url"`
	CoinBalance     int    `json:"coin_balance"`
	ReputationScore int    `json:"reputation_score"`
}

type authResponse struct {
	AccessToken  string           `json:"access_token"`
	RefreshToken string           `json:"refresh_token"`
	User         selfUserResponse `json:"user"`
}

type statusResponse struct {
	Status string `json:"status"`
}

var dummyHash string

func init() {
	h, err := auth.HashPassword("dummy-password")
	if err != nil {
		panic(err)
	}
	dummyHash = h
}

func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	ip := clientIP(r)
	if !a.ipLimiter.Allow("reg:" + ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	var req registerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	username := strings.ToLower(strings.TrimSpace(req.Username))
	password := strings.TrimSpace(req.Password)
	emailCode := strings.TrimSpace(req.EmailCode)
	deviceID := strings.TrimSpace(req.DeviceID)
	imei := strings.TrimSpace(req.IMEI)
	deviceName := strings.TrimSpace(req.DeviceName)
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	appVersion := strings.TrimSpace(req.AppVersion)

	if !isValidEmail(email) {
		writeError(w, http.StatusBadRequest, "invalid_email", "invalid email")
		return
	}
	if a.cfg.RegistrationLimit > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		count, err := a.users.Count(ctx)
		cancel()
		if err == nil && count >= a.cfg.RegistrationLimit {
			writeError(w, http.StatusForbidden, "registration_closed", "registration limit reached")
			return
		}
	}
	if !isQQEmail(email) {
		writeError(w, http.StatusBadRequest, "invalid_email_domain", "only qq email allowed")
		return
	}
	if !isValidUsername(username) {
		writeError(w, http.StatusBadRequest, "invalid_username", "invalid username")
		return
	}
	if len(password) < 8 {
		writeError(w, http.StatusBadRequest, "invalid_password", "password too short")
		return
	}
	if a.cfg.EmailVerifyEnabled {
		if emailCode == "" || !a.emailCodes.Verify(email, emailCode) {
			writeError(w, http.StatusBadRequest, "invalid_email_code", "invalid email code")
			return
		}
	}
	if deviceID != "" {
		banned, err := a.devices.IsDeviceBanned(r.Context(), deviceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		if banned {
			writeError(w, http.StatusForbidden, "device_banned", "device banned")
			return
		}
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash_failed", "internal error")
		return
	}

	id := nanoid.New()
	uid, err := newPublicID("USR-", 8)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "id_failed", "internal error")
		return
	}

	user := &data.User{
		ID:              id,
		UID:             uid,
		Email:           email,
		Username:        username,
		DisplayName:     username,
		AvatarURL:       "",
		Signature:       "",
		CoverURL:        "",
		PasswordHash:    hash,
		TokenVersion:    1,
		CoinBalance:     10,
		ReputationScore: 100,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.users.Create(ctx, user); err != nil {
		if isUniqueViolation(err, "users_email_key") {
			writeError(w, http.StatusConflict, "email_taken", "email already used")
			return
		}
		if isUniqueViolation(err, "users_username_key") {
			writeError(w, http.StatusConflict, "username_taken", "username already used")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	a.setTokenVersionCache(user.ID, user.TokenVersion)

	_ = a.devices.UpsertUserDevice(ctx, user.ID, deviceID, imei)
	_ = a.devices.UpsertLoginDevice(ctx, user.ID, deviceID, deviceName, platform, appVersion)

	tokens, err := a.issueTokens(ctx, user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed", "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		User:         toSelfUserResponse(user),
	})
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	ip := clientIP(r)
	if !a.ipLimiter.Allow("login:" + ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	var req loginRequest
	if err := decodeJSONLenient(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	identifier := strings.ToLower(strings.TrimSpace(req.Identifier))
	if identifier == "" {
		identifier = strings.ToLower(strings.TrimSpace(req.Username))
	}
	if identifier == "" {
		identifier = strings.ToLower(strings.TrimSpace(req.Email))
	}
	password := strings.TrimSpace(req.Password)
	deviceID := strings.TrimSpace(req.DeviceID)
	imei := strings.TrimSpace(req.IMEI)
	deviceName := strings.TrimSpace(req.DeviceName)
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	appVersion := strings.TrimSpace(req.AppVersion)
	if identifier == "" || password == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "invalid credentials")
		return
	}
	if !a.idLimiter.Allow("id:" + identifier) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	if deviceID != "" {
		banned, err := a.devices.IsDeviceBanned(r.Context(), deviceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", "internal error")
			return
		}
		if banned {
			writeError(w, http.StatusForbidden, "device_banned", "device banned")
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByEmailOrUsername(ctx, identifier)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			_, _ = auth.VerifyPassword(password, dummyHash)
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	ok, err := auth.VerifyPassword(password, user.PasswordHash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}

	if banned, err := a.devices.IsUserBanned(ctx, user.ID); err == nil && banned {
		writeError(w, http.StatusForbidden, "user_banned", "user banned")
		return
	}

	_ = a.refresh.RevokeAllByUser(ctx, user.ID)
	newVersion, err := a.users.IncrementTokenVersion(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed", "internal error")
		return
	}
	user.TokenVersion = newVersion
	a.setTokenVersionCache(user.ID, user.TokenVersion)

	a.maybeRehashPassword(user, password)

	tokens, err := a.issueTokens(ctx, user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed", "internal error")
		return
	}

	_ = a.devices.UpsertUserDevice(ctx, user.ID, deviceID, imei)
	_ = a.devices.UpsertLoginDevice(ctx, user.ID, deviceID, deviceName, platform, appVersion)

	writeJSON(w, http.StatusOK, authResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		User:         toSelfUserResponse(user),
	})
}

func (a *API) maybeRehashPassword(user *data.User, password string) {
	if user == nil || password == "" {
		return
	}
	if !auth.RehashEnabled() {
		return
	}
	if !auth.NeedsRehash(user.PasswordHash) {
		return
	}
	userID := user.ID
	go func() {
		hash, err := auth.HashPassword(password)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.users.UpdatePassword(ctx, userID, hash)
	}()
}

func (a *API) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	ip := clientIP(r)
	if !a.ipLimiter.Allow("reset:" + ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	var req resetPasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	username := strings.ToLower(strings.TrimSpace(req.Username))
	email := strings.ToLower(strings.TrimSpace(req.Email))
	emailCode := strings.TrimSpace(req.EmailCode)
	newPassword := strings.TrimSpace(req.NewPassword)
	if !isValidUsername(username) {
		writeError(w, http.StatusBadRequest, "invalid_username", "invalid username")
		return
	}
	if !isValidEmail(email) {
		writeError(w, http.StatusBadRequest, "invalid_email", "invalid email")
		return
	}
	if len(newPassword) < 8 {
		writeError(w, http.StatusBadRequest, "invalid_password", "password too short")
		return
	}
	if a.cfg.EmailVerifyEnabled {
		if emailCode == "" || !a.emailCodes.Verify(email, emailCode) {
			writeError(w, http.StatusBadRequest, "invalid_email_code", "invalid email code")
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	user, err := a.users.GetByEmailOrUsername(ctx, username)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
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

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash_failed", "internal error")
		return
	}
	if err := a.users.UpdatePassword(ctx, user.ID, hash); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	ip := clientIP(r)
	if !a.ipLimiter.Allow("refresh:" + ip) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}

	var req refreshRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "invalid refresh token")
		return
	}

	tokenHash := auth.HashToken(refreshToken)
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	stored, err := a.refresh.GetByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if stored.RevokedAt.Valid || time.Now().After(stored.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}

	user, err := a.users.GetByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}
	if banned, err := a.devices.IsUserBanned(ctx, user.ID); err == nil && banned {
		writeError(w, http.StatusForbidden, "user_banned", "user banned")
		return
	}

	newRefresh, rawRefresh, err := a.newRefreshToken(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed", "internal error")
		return
	}

	if err := a.refresh.Rotate(ctx, stored.ID, newRefresh); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", "internal error")
		return
	}

	accessToken, err := auth.NewAccessToken(a.cfg.JWTSecret, a.cfg.JWTIssuer, a.cfg.AccessTokenTTL, user.ID, user.UID, user.Username, user.TokenVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		User:         toSelfUserResponse(user),
	})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var req refreshRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "invalid refresh token")
		return
	}

	tokenHash := auth.HashToken(refreshToken)
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	stored, err := a.refresh.GetByHash(ctx, tokenHash)
	if err == nil {
		_ = a.refresh.Revoke(ctx, stored.ID)
	}

	writeJSON(w, http.StatusOK, statusResponse{Status: "ok"})
}

func toUserResponse(u *data.User) userResponse {
	return userResponse{
		ID:          u.ID,
		UID:         u.UID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		UserTitle:   u.UserTitle,
		AvatarURL:   u.AvatarURL,
		Signature:   u.Signature,
		CoverURL:    u.CoverURL,
	}
}

func toSelfUserResponse(u *data.User) selfUserResponse {
	return selfUserResponse{
		ID:              u.ID,
		UID:             u.UID,
		Username:        u.Username,
		DisplayName:     u.DisplayName,
		UserTitle:       u.UserTitle,
		AvatarURL:       u.AvatarURL,
		Signature:       u.Signature,
		CoverURL:        u.CoverURL,
		CoinBalance:     u.CoinBalance,
		ReputationScore: u.ReputationScore,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("extra data")
	}
	return nil
}

func decodeJSONLenient(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("extra data")
	}
	return nil
}

func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return true
	}
	ct = strings.ToLower(ct)
	if !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media", "content-type must be application/json")
		return false
	}
	return true
}

func isValidEmail(email string) bool {
	if len(email) < 3 || len(email) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return addr.Address == email
}

func isQQEmail(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	return strings.HasSuffix(e, "@qq.com") || strings.HasSuffix(e, "@vip.qq.com")
}

func isValidUsername(username string) bool {
	if len(username) < 3 || len(username) > 24 {
		return false
	}
	for i := 0; i < len(username); i++ {
		c := username[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return strings.TrimSpace(xr)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func newPublicID(prefix string, size int) (string, error) {
	id, err := nanoid.Generate("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", size)
	if err != nil {
		return "", err
	}
	return prefix + id, nil
}

func isUniqueViolation(err error, constraint string) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	if !strings.Contains(s, "UNIQUE constraint failed") {
		return false
	}
	if strings.Contains(s, constraint) {
		return true
	}
	if sqliteSig := sqliteUniqueSignature(constraint); sqliteSig != "" {
		return strings.Contains(s, sqliteSig)
	}
	return false
}

func sqliteUniqueSignature(constraint string) string {
	switch constraint {
	case "users_email_key":
		return "users.email"
	case "users_username_key":
		return "users.username"
	case "users_uid_key":
		return "users.uid"
	case "idx_friend_req_pending":
		return "friend_requests.from_user_id, friend_requests.to_user_id"
	case "idx_group_join_pending":
		return "group_join_requests.group_id, group_join_requests.user_id"
	case "idx_red_packet_claim_unique":
		return "red_packet_claims.packet_id, red_packet_claims.user_id"
	default:
		return ""
	}
}
