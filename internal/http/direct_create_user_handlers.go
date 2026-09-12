package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"metrochat/internal/auth"
	"metrochat/internal/data"
)

type directCreateUserRequest struct {
	AdminToken      string `json:"admin_token"`
	Email           string `json:"email"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	UID             string `json:"uid"`
	DisplayName     string `json:"display_name"`
	CoinBalance     int    `json:"coin_balance"`
	ReputationScore *int   `json:"reputation_score"`
}

type directCreateUserResult struct {
	User     *data.User
	Tokens   tokenPair
	Password string
}

func (a *API) handleDirectCreateUser(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	var req directCreateUserRequest
	if err := decodeJSONLenient(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid json")
		return
	}

	token := strings.TrimSpace(req.AdminToken)
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	}
	if !secureEqual(token, a.cfg.AdminPassword) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	result, status, code, message := a.createDirectUserForTest(ctx, req)
	if status != 0 {
		writeError(w, status, code, message)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"message":        "user created",
		"access_token":   result.Tokens.AccessToken,
		"refresh_token":  result.Tokens.RefreshToken,
		"login_password": result.Password,
		"user":           toSelfUserResponse(result.User),
	})
}

func (a *API) createDirectUserForTest(ctx context.Context, req directCreateUserRequest) (*directCreateUserResult, int, string, string) {

	username := strings.ToLower(strings.TrimSpace(req.Username))
	if username == "" {
		username = "test_" + strconv.FormatInt(time.Now().Unix(), 10)
	}
	if !isValidUsername(username) {
		return nil, http.StatusBadRequest, "invalid_username", "invalid username"
	}

	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = "test123456"
	} else if len(password) < 8 {
		return nil, http.StatusBadRequest, "invalid_password", "password too short"
	}

	uid := strings.ToUpper(strings.TrimSpace(req.UID))
	if uid == "" {
		generatedUID, err := newPublicID("USR-", 8)
		if err != nil {
			return nil, http.StatusInternalServerError, "id_failed", "internal error"
		}
		uid = generatedUID
	}
	if !isValidUID(uid) {
		return nil, http.StatusBadRequest, "invalid_uid", "invalid uid"
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		email = username + "@qq.com"
	}
	if !isValidEmail(email) {
		return nil, http.StatusBadRequest, "invalid_email", "invalid email"
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = username
	}

	coinBalance := req.CoinBalance
	if coinBalance < 0 {
		coinBalance = 0
	}
	reputationScore := 100
	if req.ReputationScore != nil {
		reputationScore = *req.ReputationScore
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, http.StatusInternalServerError, "hash_failed", "internal error"
	}

	user := &data.User{
		ID:              data.NewID(),
		UID:             uid,
		Email:           email,
		Username:        username,
		DisplayName:     displayName,
		AvatarURL:       "",
		Signature:       "",
		CoverURL:        "",
		PasswordHash:    hash,
		TokenVersion:    1,
		CoinBalance:     coinBalance,
		ReputationScore: reputationScore,
	}

	if err := a.users.Create(ctx, user); err != nil {
		if isUniqueViolation(err, "users_email_key") {
			return nil, http.StatusConflict, "email_taken", "email already used"
		}
		if isUniqueViolation(err, "users_username_key") {
			return nil, http.StatusConflict, "username_taken", "username already used"
		}
		if isUniqueViolation(err, "users_uid_key") {
			return nil, http.StatusConflict, "uid_taken", "uid already used"
		}
		return nil, http.StatusInternalServerError, "db_error", "internal error"
	}

	a.setTokenVersionCache(user.ID, user.TokenVersion)
	tokens, err := a.issueTokens(ctx, user)
	if err != nil {
		return nil, http.StatusInternalServerError, "token_failed", "internal error"
	}

	return &directCreateUserResult{
		User:     user,
		Tokens:   tokens,
		Password: password,
	}, 0, "", ""
}
