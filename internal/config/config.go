package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                      string
	DatabaseURL               string
	UploadDir                 string
	UpdateDir                 string
	PublicBaseURL             string
	DataServerBaseURL         string
	DataServerSyncToken       string
	AdminUser                 string
	AdminPassword             string
	JWTSecret                 []byte
	JWTIssuer                 string
	JWTIssuerLegacy           []string
	AccessTokenTTL            time.Duration
	RefreshTokenTTL           time.Duration
	ResourceQuota             int64
	RegistrationLimit         int
	VideoEnabled              bool
	MediaRateBytes            int64
	UpdateRateBytes           int64
	VideoRateBytes            int64
	MusicRateBytes            int64
	MediaDownloadConcurrency  int
	UpdateDownloadConcurrency int
	VideoDownloadConcurrency  int
	MusicDownloadConcurrency  int
	EmailVerifyEnabled        bool
}

func Load() (Config, error) {
	cfg := Config{}

	// 1. Load settings.json first (lowest priority after defaults)
	settingsPath := getEnv("SETTINGS_JSON", "settings.json")
	settings, settingsOk := loadSettingsFromJSON(settingsPath)

	// 2. Apply defaults
	cfg.Port = "8080"
	cfg.DatabaseURL = "metrochat.db"
	cfg.UploadDir = "uploads"
	cfg.UpdateDir = "update"
	cfg.AdminUser = "admin"
	cfg.AdminPassword = "admin123456"
	cfg.ResourceQuota = 10 * 1024 * 1024 * 1024 // 10GB
	cfg.MediaRateBytes = 5 << 20
	cfg.UpdateRateBytes = 5 << 20
	cfg.VideoRateBytes = 5 << 20
	cfg.MusicRateBytes = 5 << 20
	cfg.MediaDownloadConcurrency = 12
	cfg.UpdateDownloadConcurrency = 4
	cfg.VideoDownloadConcurrency = 2
	cfg.MusicDownloadConcurrency = 8
	cfg.VideoEnabled = false
	cfg.EmailVerifyEnabled = true
	cfg.JWTIssuer = "metrochat"
	cfg.RefreshTokenTTL = 30 * 24 * time.Hour

	// 3. Override with settings.json values
	if settingsOk {
		if v := strings.TrimSpace(settings.AdminUser); v != "" {
			cfg.AdminUser = v
		}
		if v := strings.TrimSpace(settings.AdminPassword); v != "" {
			cfg.AdminPassword = v
		}
		if v := strings.TrimSpace(settings.PublicBaseURL); v != "" {
			cfg.PublicBaseURL = v
		}
		if v := strings.TrimRight(strings.TrimSpace(settings.DataServerBaseURL), "/"); v != "" {
			cfg.DataServerBaseURL = v
		}
		if v := strings.TrimSpace(settings.DataServerSyncToken); v != "" {
			cfg.DataServerSyncToken = v
		}
		if v := strings.TrimSpace(settings.JWTSecret); v != "" {
			cfg.JWTSecret = []byte(v)
		}
		if settings.ResourceQuotaBytes > 0 {
			cfg.ResourceQuota = settings.ResourceQuotaBytes
		}
		if settings.RegistrationLimit > 0 {
			cfg.RegistrationLimit = settings.RegistrationLimit
		}
		if settings.MediaRateBytes != nil && *settings.MediaRateBytes >= 0 {
			cfg.MediaRateBytes = *settings.MediaRateBytes
		}
		if settings.UpdateRateBytes != nil && *settings.UpdateRateBytes >= 0 {
			cfg.UpdateRateBytes = *settings.UpdateRateBytes
		}
		if settings.VideoRateBytes != nil && *settings.VideoRateBytes >= 0 {
			cfg.VideoRateBytes = *settings.VideoRateBytes
		}
		if settings.MusicRateBytes != nil && *settings.MusicRateBytes >= 0 {
			cfg.MusicRateBytes = *settings.MusicRateBytes
		}
		if settings.MediaDownloadConcurrency != nil && *settings.MediaDownloadConcurrency > 0 {
			cfg.MediaDownloadConcurrency = *settings.MediaDownloadConcurrency
		}
		if settings.UpdateDownloadConcurrency != nil && *settings.UpdateDownloadConcurrency > 0 {
			cfg.UpdateDownloadConcurrency = *settings.UpdateDownloadConcurrency
		}
		if settings.VideoDownloadConcurrency != nil && *settings.VideoDownloadConcurrency > 0 {
			cfg.VideoDownloadConcurrency = *settings.VideoDownloadConcurrency
		}
		if settings.MusicDownloadConcurrency != nil && *settings.MusicDownloadConcurrency > 0 {
			cfg.MusicDownloadConcurrency = *settings.MusicDownloadConcurrency
		}
		if settings.VideoEnabled != nil {
			cfg.VideoEnabled = *settings.VideoEnabled
		}
		if settings.EmailVerifyEnabled != nil {
			cfg.EmailVerifyEnabled = *settings.EmailVerifyEnabled
		}
	}

	// 4. Override with environment variables (highest priority)
	if v, ok := getEnvRaw("PORT"); ok {
		cfg.Port = v
	}
	dbURL, dbURLSet := getEnvRaw("DATABASE_URL")
	if dbURLSet {
		if dbURL == "" {
			dbURL = "metrochat.db"
		}
		cfg.DatabaseURL = resolveDatabaseURL(dbURL, true)
	} else {
		cfg.DatabaseURL = resolveDatabaseURL(cfg.DatabaseURL, false)
	}
	if v, ok := getEnvRaw("UPLOAD_DIR"); ok {
		cfg.UploadDir = v
	}
	if v, ok := getEnvRaw("UPDATE_DIR"); ok {
		cfg.UpdateDir = v
	}
	if raw, ok := getEnvRaw("PUBLIC_BASE_URL"); ok {
		cfg.PublicBaseURL = strings.TrimSpace(raw)
	}
	if raw, ok := getEnvRaw("DATA_SERVER_BASE_URL"); ok {
		cfg.DataServerBaseURL = strings.TrimRight(strings.TrimSpace(raw), "/")
	}
	if raw, ok := getEnvRaw("DATA_SERVER_SYNC_TOKEN"); ok {
		cfg.DataServerSyncToken = strings.TrimSpace(raw)
	}
	if v, ok := getEnvRaw("ADMIN_USER"); ok {
		cfg.AdminUser = v
	}
	if v, ok := getEnvRaw("ADMIN_PASSWORD"); ok {
		cfg.AdminPassword = v
	}
	if envQuota := getInt64Env("RESOURCE_QUOTA_BYTES", 0); envQuota > 0 {
		cfg.ResourceQuota = envQuota
	}
	if envLimit := getIntEnv("REGISTRATION_LIMIT", 0); envLimit > 0 {
		cfg.RegistrationLimit = envLimit
	}
	if v, ok := getInt64EnvAllowZero("MEDIA_TRANSFER_RATE_BYTES"); ok && v >= 0 {
		cfg.MediaRateBytes = v
	}
	if v, ok := getInt64EnvAllowZero("UPDATE_TRANSFER_RATE_BYTES"); ok && v >= 0 {
		cfg.UpdateRateBytes = v
	}
	if v, ok := getInt64EnvAllowZero("VIDEO_TRANSFER_RATE_BYTES"); ok && v >= 0 {
		cfg.VideoRateBytes = v
	}
	if v, ok := getInt64EnvAllowZero("MUSIC_TRANSFER_RATE_BYTES"); ok && v >= 0 {
		cfg.MusicRateBytes = v
	}
	if v, ok := getIntEnvAllowZero("MEDIA_DOWNLOAD_CONCURRENCY"); ok && v > 0 {
		cfg.MediaDownloadConcurrency = v
	}
	if v, ok := getIntEnvAllowZero("UPDATE_DOWNLOAD_CONCURRENCY"); ok && v > 0 {
		cfg.UpdateDownloadConcurrency = v
	}
	if v, ok := getIntEnvAllowZero("VIDEO_DOWNLOAD_CONCURRENCY"); ok && v > 0 {
		cfg.VideoDownloadConcurrency = v
	}
	if v, ok := getIntEnvAllowZero("MUSIC_DOWNLOAD_CONCURRENCY"); ok && v > 0 {
		cfg.MusicDownloadConcurrency = v
	}
	if raw, ok := getEnvRaw("VIDEO_ENABLED"); ok {
		cfg.VideoEnabled = parseBool(raw, cfg.VideoEnabled)
	}
	if raw, ok := getEnvRaw("EMAIL_VERIFY_ENABLED"); ok {
		cfg.EmailVerifyEnabled = parseBool(raw, cfg.EmailVerifyEnabled)
	}

	// JWT secret handling
	secret, secretSet := getEnvRaw("JWT_SECRET")
	jwtGenerated := false
	if secretSet && secret != "" {
		cfg.JWTSecret = []byte(secret)
	}
	if len(cfg.JWTSecret) == 0 {
		// Generate a random 48-byte secret (64 chars in base64)
		raw := make([]byte, 48)
		if _, err := rand.Read(raw); err != nil {
			return cfg, errors.New("failed to generate JWT secret: " + err.Error())
		}
		cfg.JWTSecret = []byte(base64.RawURLEncoding.EncodeToString(raw))
		jwtGenerated = true
	}
	if len(cfg.JWTSecret) < 32 {
		return cfg, errors.New("JWT_SECRET must be at least 32 bytes")
	}

	if v, ok := getEnvRaw("JWT_ISSUER"); ok {
		cfg.JWTIssuer = v
	}
	if legacy := strings.TrimSpace(getEnv("JWT_ISSUER_LEGACY", "")); legacy != "" {
		cfg.JWTIssuerLegacy = nil
		for _, item := range strings.Split(legacy, ",") {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				cfg.JWTIssuerLegacy = append(cfg.JWTIssuerLegacy, trimmed)
			}
		}
	}
	if v := getDurationEnv("ACCESS_TOKEN_TTL", 0); v > 0 {
		cfg.AccessTokenTTL = v
	}
	if v := getDurationEnv("REFRESH_TOKEN_TTL", 0); v > 0 {
		cfg.RefreshTokenTTL = v
	}

	// 5. Persist effective config back to settings.json (auto-fill missing fields)
	if settingsPath != "" {
		persistSettings(settingsPath, cfg, secretSet, jwtGenerated)
	}

	return cfg, nil
}

// persistSettings writes the effective configuration back to settings.json,
// filling in any missing fields with their current effective values.
// envHasJWT: whether JWT_SECRET was explicitly provided via environment variable
// jwtGenerated: whether the JWT secret was freshly generated (always persist in that case)
func persistSettings(path string, cfg Config, envHasJWT, jwtGenerated bool) {
	// Load existing file to preserve what's there, then fill in gaps
	existing, _ := loadSettingsFromJSON(path)

	changed := false

	// AdminUser
	if strings.TrimSpace(existing.AdminUser) == "" {
		existing.AdminUser = cfg.AdminUser
		changed = true
	}
	// AdminPassword
	if strings.TrimSpace(existing.AdminPassword) == "" {
		existing.AdminPassword = cfg.AdminPassword
		changed = true
	}
	// PublicBaseURL
	if strings.TrimSpace(existing.PublicBaseURL) == "" {
		existing.PublicBaseURL = cfg.PublicBaseURL
		changed = true
	}
	// DataServerBaseURL
	if strings.TrimSpace(existing.DataServerBaseURL) == "" {
		existing.DataServerBaseURL = cfg.DataServerBaseURL
		changed = true
	}
	// DataServerSyncToken
	if strings.TrimSpace(existing.DataServerSyncToken) == "" {
		existing.DataServerSyncToken = cfg.DataServerSyncToken
		changed = true
	}
	// JWTSecret — persist if:
	//   - it was freshly generated (must save it or tokens will be invalid on restart)
	//   - existing config has no secret AND it's not from env var (use defaults/settings value)
	if jwtGenerated {
		existing.JWTSecret = string(cfg.JWTSecret)
		changed = true
	} else if strings.TrimSpace(existing.JWTSecret) == "" && !envHasJWT {
		existing.JWTSecret = string(cfg.JWTSecret)
		changed = true
	}
	// ResourceQuotaBytes
	if existing.ResourceQuotaBytes <= 0 {
		existing.ResourceQuotaBytes = cfg.ResourceQuota
		changed = true
	}
	// RegistrationLimit
	if existing.RegistrationLimit <= 0 {
		existing.RegistrationLimit = cfg.RegistrationLimit
		changed = true
	}
	// VideoEnabled
	if existing.VideoEnabled == nil {
		v := cfg.VideoEnabled
		existing.VideoEnabled = &v
		changed = true
	}
	// EmailVerifyEnabled
	if existing.EmailVerifyEnabled == nil {
		v := cfg.EmailVerifyEnabled
		existing.EmailVerifyEnabled = &v
		changed = true
	}
	// MediaRateBytes
	if existing.MediaRateBytes == nil {
		v := cfg.MediaRateBytes
		existing.MediaRateBytes = &v
		changed = true
	}
	if existing.UpdateRateBytes == nil {
		v := cfg.UpdateRateBytes
		existing.UpdateRateBytes = &v
		changed = true
	}
	if existing.VideoRateBytes == nil {
		v := cfg.VideoRateBytes
		existing.VideoRateBytes = &v
		changed = true
	}
	if existing.MusicRateBytes == nil {
		v := cfg.MusicRateBytes
		existing.MusicRateBytes = &v
		changed = true
	}
	if existing.MediaDownloadConcurrency == nil {
		v := cfg.MediaDownloadConcurrency
		existing.MediaDownloadConcurrency = &v
		changed = true
	}
	if existing.UpdateDownloadConcurrency == nil {
		v := cfg.UpdateDownloadConcurrency
		existing.UpdateDownloadConcurrency = &v
		changed = true
	}
	if existing.VideoDownloadConcurrency == nil {
		v := cfg.VideoDownloadConcurrency
		existing.VideoDownloadConcurrency = &v
		changed = true
	}
	if existing.MusicDownloadConcurrency == nil {
		v := cfg.MusicDownloadConcurrency
		existing.MusicDownloadConcurrency = &v
		changed = true
	}

	if !changed {
		return
	}

	// Ensure directory exists
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

func getEnv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func getEnvRaw(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	return v, ok
}

func resolveDatabaseURL(value string, explicit bool) string {
	if value == "" {
		return value
	}
	if explicit {
		return value
	}
	// If default relative path doesn't exist, try common locations.
	if looksLikeURL(value) || filepath.IsAbs(value) {
		return value
	}
	if fileExists(value) {
		return value
	}
	if fileExists(filepath.Join("server", value)) {
		return filepath.Join("server", value)
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidate := filepath.Join(exeDir, value)
		if fileExists(candidate) {
			return candidate
		}
	}
	return value
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func looksLikeURL(value string) bool {
	return strings.Contains(value, "://")
}

func getInt64Env(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	val, err := strconv.ParseInt(v, 10, 64)
	if err != nil || val <= 0 {
		return def
	}
	return val
}

func getInt64EnvAllowZero(key string) (int64, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	val, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return val, true
}

func getIntEnvAllowZero(key string) (int, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return 0, false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	val, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return val, true
}

func getIntEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	val, err := strconv.Atoi(v)
	if err != nil || val <= 0 {
		return def
	}
	return val
}

func parseBool(raw string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "1" || v == "true" || v == "yes" || v == "on" {
		return true
	}
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	return def
}

type settingsFile struct {
	AdminUser                 string `json:"admin_user"`
	AdminPassword             string `json:"admin_password"`
	ResourceQuotaBytes        int64  `json:"resource_quota_bytes"`
	RegistrationLimit         int    `json:"registration_limit"`
	PublicBaseURL             string `json:"public_base_url"`
	DataServerBaseURL         string `json:"data_server_base_url"`
	DataServerSyncToken       string `json:"data_server_sync_token"`
	JWTSecret                 string `json:"jwt_secret"`
	VideoEnabled              *bool  `json:"video_enabled"`
	MediaRateBytes            *int64 `json:"media_transfer_rate_bytes"`
	UpdateRateBytes           *int64 `json:"update_transfer_rate_bytes"`
	VideoRateBytes            *int64 `json:"video_transfer_rate_bytes"`
	MusicRateBytes            *int64 `json:"music_transfer_rate_bytes"`
	MediaDownloadConcurrency  *int   `json:"media_download_concurrency"`
	UpdateDownloadConcurrency *int   `json:"update_download_concurrency"`
	VideoDownloadConcurrency  *int   `json:"video_download_concurrency"`
	MusicDownloadConcurrency  *int   `json:"music_download_concurrency"`
	EmailVerifyEnabled        *bool  `json:"email_verify_enabled"`
}

func loadSettingsFromJSON(path string) (settingsFile, bool) {
	if path == "" {
		return settingsFile{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return settingsFile{}, false
	}
	var cfg settingsFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return settingsFile{}, false
	}
	return cfg, true
}

func getDurationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return def
	}
	return time.Duration(secs) * time.Second
}
