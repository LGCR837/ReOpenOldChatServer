package data

import (
	"context"
	"database/sql"
	_ "embed"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var embeddedSchema []byte

func OpenDB(ctx context.Context, dsn string) (*sqlx.DB, error) {
	// SQLite 默认使用 sqlite 驱动名 (modernc.org/sqlite)
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	maxOpen := getEnvInt("SQLITE_MAX_OPEN_CONNS", 1)
	if maxOpen < 1 {
		maxOpen = 1
	}
	db.SetMaxOpenConns(maxOpen) // SQLite 默认单连接，必要时可调大
	db.SetMaxIdleConns(maxOpen)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	applySQLitePragmas(db)

	// 自动初始化数据库表
	if err := initSchema(db); err != nil {
		return nil, err
	}

	return db, nil
}

func applySQLitePragmas(db *sqlx.DB) {
	if db == nil {
		return
	}
	journal := getEnvString("SQLITE_JOURNAL_MODE", "WAL")
	if journal != "" {
		_, _ = db.Exec("PRAGMA journal_mode = " + journal)
	}
	sync := getEnvString("SQLITE_SYNCHRONOUS", "NORMAL")
	if sync != "" {
		_, _ = db.Exec("PRAGMA synchronous = " + sync)
	}
	tempStore := getEnvString("SQLITE_TEMP_STORE", "MEMORY")
	if tempStore != "" {
		_, _ = db.Exec("PRAGMA temp_store = " + tempStore)
	}
	_, _ = db.Exec("PRAGMA foreign_keys = ON")
	if busy := getEnvInt("SQLITE_BUSY_TIMEOUT_MS", 5000); busy > 0 {
		_, _ = db.Exec("PRAGMA busy_timeout = " + strconv.Itoa(busy))
	}
	if cacheKB := getEnvInt("SQLITE_CACHE_SIZE_KB", 0); cacheKB > 0 {
		_, _ = db.Exec("PRAGMA cache_size = -" + strconv.Itoa(cacheKB))
	}
	if mmapMB := getEnvInt("SQLITE_MMAP_SIZE_MB", 0); mmapMB > 0 {
		_, _ = db.Exec("PRAGMA mmap_size = " + strconv.Itoa(mmapMB*1024*1024))
	}
}

func getEnvString(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func getEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	val, err := strconv.Atoi(v)
	if err != nil || val <= 0 {
		return def
	}
	return val
}

func initSchema(db *sqlx.DB) error {
	hasUsers, err := hasUsersTable(db)
	if err != nil {
		return err
	}
	if hasUsers {
		ensureMessageColumns(db)
		ensureExtraSchema(db)
		return nil
	}

	schema, err := readSchemaFile()
	if err != nil {
		return err
	}

	if _, err = db.Exec(string(schema)); err != nil {
		return err
	}
	ensureMessageColumns(db)
	ensureExtraSchema(db)
	return nil
}

func readSchemaFile() ([]byte, error) {
	if len(embeddedSchema) > 0 {
		return embeddedSchema, nil
	}
	// Fallback: try disk for development/override
	if schema, err := os.ReadFile("schema.sql"); err == nil {
		return schema, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(filepath.Dir(exe), "schema.sql"))
}

func hasUsersTable(db *sqlx.DB) (bool, error) {
	var name string
	err := db.Get(&name, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'users'`)
	if err == nil {
		return name == "users", nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, err
}

func ensureMessageColumns(db *sqlx.DB) {
	_ = addColumnIfMissing(db, "direct_messages", "delivered_at", "DATETIME NULL")
	_ = addColumnIfMissing(db, "direct_messages", "read_at", "DATETIME NULL")
	_ = addColumnIfMissing(db, "direct_messages", "msg_type", "TEXT NOT NULL DEFAULT 'text'")
	_ = addColumnIfMissing(db, "direct_messages", "media_url", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "direct_messages", "thumb_url", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "direct_messages", "duration_ms", "INTEGER NOT NULL DEFAULT 0")
	_ = addColumnIfMissing(db, "group_messages", "msg_type", "TEXT NOT NULL DEFAULT 'text'")
	_ = addColumnIfMissing(db, "group_messages", "media_url", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "group_messages", "thumb_url", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "group_messages", "duration_ms", "INTEGER NOT NULL DEFAULT 0")
	_ = addColumnIfMissing(db, "groups", "avatar_url", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "groups", "announcement", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "groups", "announcement_mode", "INTEGER NOT NULL DEFAULT 0")
	_ = addColumnIfMissing(db, "groups", "announcement_updated_at", "DATETIME NOT NULL DEFAULT '1970-01-01 00:00:00'")
	_ = addColumnIfMissing(db, "group_members", "announcement_read_at", "DATETIME NULL")
	_ = addColumnIfMissing(db, "friends", "remark_name", "TEXT NOT NULL DEFAULT ''")
}

func ensureExtraSchema(db *sqlx.DB) {
	// Bug report table is also ensured here so schema migrations can safely run
	// during OpenDB (before http layer bootstraps stores).
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS bug_reports (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32),
    user_uid VARCHAR(32) NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    device_model VARCHAR(128) NOT NULL DEFAULT '',
    android_version VARCHAR(32) NOT NULL DEFAULT '',
    app_version VARCHAR(32) NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    admin_note TEXT NOT NULL DEFAULT '',
    resolved_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS crash_reports (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(32) REFERENCES users(id) ON DELETE SET NULL,
    crash_log TEXT NOT NULL,
    device_model VARCHAR(128) NOT NULL DEFAULT '',
    android_version VARCHAR(32) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_crash_reports_created ON crash_reports (created_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_crash_reports_user ON crash_reports (user_id, created_at)`)
	_ = addColumnIfMissing(db, "bug_reports", "status", "TEXT NOT NULL DEFAULT 'open'")
	_ = addColumnIfMissing(db, "bug_reports", "admin_note", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "bug_reports", "resolved_at", "DATETIME NULL")

	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS system_notifications (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    important INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`)
	_ = addColumnIfMissing(db, "system_notifications", "important", "INTEGER NOT NULL DEFAULT 0")
	_ = addColumnIfMissing(db, "users", "signature", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "users", "cover_url", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "users", "token_version", "INTEGER NOT NULL DEFAULT 0")
	_ = addColumnIfMissing(db, "users", "user_title", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "users", "user_title_price", "INTEGER NOT NULL DEFAULT 0")
	_ = addColumnIfMissing(db, "users", "coin_balance", "INTEGER NOT NULL DEFAULT 10")
	_, _ = db.Exec(`UPDATE users SET coin_balance = 10 WHERE coin_balance IS NULL`)
	_, _ = db.Exec(`UPDATE users SET coin_balance = 0 WHERE coin_balance < 0`)
	_ = addColumnIfMissing(db, "users", "reputation_score", "INTEGER NOT NULL DEFAULT 100")
	_, _ = db.Exec(`UPDATE users SET reputation_score = 100 WHERE reputation_score IS NULL`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS title_catalog (
    id VARCHAR(32) PRIMARY KEY,
    title TEXT NOT NULL,
    price INTEGER NOT NULL DEFAULT 100,
    active INTEGER NOT NULL DEFAULT 1,
    is_custom INTEGER NOT NULL DEFAULT 0,
    owner_id VARCHAR(32) NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_title_catalog_title ON title_catalog (title)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_title_catalog_owner ON title_catalog (owner_id)`)
	_ = addColumnIfMissing(db, "title_catalog", "owner_id", "VARCHAR(32) NULL REFERENCES users(id) ON DELETE SET NULL")
	_ = addColumnIfMissing(db, "title_catalog", "is_custom", "INTEGER NOT NULL DEFAULT 0")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS user_daily_checkins (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    checkin_date VARCHAR(16) NOT NULL,
    coin_reward INTEGER NOT NULL DEFAULT 0,
    reputation_reward INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, checkin_date)
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_daily_checkins_user_created ON user_daily_checkins (user_id, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_direct_messages_created ON direct_messages (created_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_direct_messages_read ON direct_messages (read_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_group_messages_created ON group_messages (created_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_moments_created ON moments (created_at)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS user_devices (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id VARCHAR(128) NOT NULL,
    imei VARCHAR(64) NOT NULL DEFAULT '',
    last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_device_pair ON user_devices (user_id, device_id)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_devices_user ON user_devices (user_id)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS user_login_devices (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id VARCHAR(128) NOT NULL,
    device_name VARCHAR(128) NOT NULL DEFAULT '',
    platform VARCHAR(32) NOT NULL DEFAULT '',
    app_version VARCHAR(32) NOT NULL DEFAULT '',
    last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_login_device_pair ON user_login_devices (user_id, device_id)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_login_devices_user ON user_login_devices (user_id)`)
	_ = addColumnIfMissing(db, "group_members", "last_read_at", "DATETIME NULL")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS banned_devices (
    device_id VARCHAR(128) PRIMARY KEY,
    reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_ = addColumnIfMissing(db, "banned_devices", "banned_until", "DATETIME NULL")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS banned_users (
    user_id VARCHAR(32) PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_ = addColumnIfMissing(db, "banned_users", "banned_until", "DATETIME NULL")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS ban_appeals (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    uid VARCHAR(32) NOT NULL DEFAULT '',
    username VARCHAR(64) NOT NULL DEFAULT '',
    ban_reason TEXT NOT NULL DEFAULT '',
    banned_until DATETIME NULL,
    appeal_text TEXT NOT NULL DEFAULT '',
    contact TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    admin_note TEXT NOT NULL DEFAULT '',
    handled_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ban_appeals_status_created ON ban_appeals (status, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ban_appeals_user_status ON ban_appeals (user_id, status, created_at DESC)`)
	_ = addColumnIfMissing(db, "ban_appeals", "contact", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "ban_appeals", "admin_note", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "ban_appeals", "handled_at", "DATETIME NULL")
	_ = addColumnIfMissing(db, "ban_appeals", "updated_at", "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS banned_groups (
    group_id VARCHAR(32) PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_ = addColumnIfMissing(db, "banned_groups", "banned_until", "DATETIME NULL")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS moment_likes (
    moment_id VARCHAR(32) NOT NULL REFERENCES moments(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (moment_id, user_id)
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_moment_likes_moment ON moment_likes (moment_id)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS moment_comments (
    id VARCHAR(32) PRIMARY KEY,
    moment_id VARCHAR(32) NOT NULL REFERENCES moments(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_moment_comments_moment ON moment_comments (moment_id, created_at)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS resource_sections (
    id VARCHAR(32) PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    owner_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_resource_sections_name ON resource_sections (name COLLATE NOCASE)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_resource_sections_owner ON resource_sections (owner_id, created_at)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS resource_items (
    id VARCHAR(32) PRIMARY KEY,
    section_id VARCHAR(32) NOT NULL REFERENCES resource_sections(id) ON DELETE CASCADE,
    uploader_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    url VARCHAR(1024) NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_resource_items_section ON resource_items (section_id, created_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_resource_items_uploader ON resource_items (uploader_id)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS resource_likes (
    item_id VARCHAR(32) NOT NULL REFERENCES resource_items(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (item_id, user_id)
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_resource_likes_item ON resource_likes (item_id)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS resource_comments (
    id VARCHAR(32) PRIMARY KEY,
    item_id VARCHAR(32) NOT NULL REFERENCES resource_items(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_resource_comments_item ON resource_comments (item_id, created_at)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS emoji_plaza_items (
    id VARCHAR(32) PRIMARY KEY,
    owner_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    media_url VARCHAR(1024) NOT NULL,
    package_url VARCHAR(1024) NOT NULL DEFAULT '',
    cover_url VARCHAR(1024) NOT NULL DEFAULT '',
    item_count INTEGER NOT NULL DEFAULT 1,
    is_gif INTEGER NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_emoji_plaza_created ON emoji_plaza_items (created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_emoji_plaza_owner ON emoji_plaza_items (owner_id, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_emoji_plaza_name ON emoji_plaza_items (name COLLATE NOCASE)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_emoji_plaza_owner_media ON emoji_plaza_items (owner_id, media_url)`)
	_ = addColumnIfMissing(db, "emoji_plaza_items", "package_url", "VARCHAR(1024) NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "emoji_plaza_items", "cover_url", "VARCHAR(1024) NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "emoji_plaza_items", "item_count", "INTEGER NOT NULL DEFAULT 1")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS music_plaza_items (
    id VARCHAR(32) PRIMARY KEY,
    owner_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,
    song_url VARCHAR(1024) NOT NULL,
    cover_url VARCHAR(1024) NOT NULL DEFAULT '',
    lyrics_url VARCHAR(1024) NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    play_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_plaza_created ON music_plaza_items (created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_plaza_owner ON music_plaza_items (owner_id, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_plaza_name ON music_plaza_items (name COLLATE NOCASE)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_music_plaza_owner_song ON music_plaza_items (owner_id, song_url)`)
	_ = addColumnIfMissing(db, "music_plaza_items", "cover_url", "VARCHAR(1024) NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "music_plaza_items", "lyrics_url", "VARCHAR(1024) NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "music_plaza_items", "duration_ms", "INTEGER NOT NULL DEFAULT 0")
	_ = addColumnIfMissing(db, "music_plaza_items", "play_count", "INTEGER NOT NULL DEFAULT 0")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS music_plaza_likes (
    item_id VARCHAR(32) NOT NULL REFERENCES music_plaza_items(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (item_id, user_id)
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_plaza_likes_item ON music_plaza_likes (item_id)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS music_plaza_comments (
    id VARCHAR(32) PRIMARY KEY,
    item_id VARCHAR(32) NOT NULL REFERENCES music_plaza_items(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_plaza_comments_item ON music_plaza_comments (item_id, created_at)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS music_plaza_play_logs (
    id VARCHAR(32) PRIMARY KEY,
    item_id VARCHAR(32) NOT NULL REFERENCES music_plaza_items(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_plaza_play_logs_item_time ON music_plaza_play_logs (item_id, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_music_plaza_play_logs_user_time ON music_plaza_play_logs (user_id, created_at DESC)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS user_favorites (
    id VARCHAR(32) PRIMARY KEY,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fav_type VARCHAR(32) NOT NULL,
    target_id VARCHAR(64) NOT NULL,
    title VARCHAR(200) NOT NULL DEFAULT '',
    subtitle VARCHAR(300) NOT NULL DEFAULT '',
    media_url VARCHAR(1024) NOT NULL DEFAULT '',
    extra_json TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_favorites_unique ON user_favorites (user_id, fav_type, target_id)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_favorites_user_created ON user_favorites (user_id, created_at DESC)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS external_coin_transfers (
    id VARCHAR(32) PRIMARY KEY,
    client_order_no VARCHAR(64) NOT NULL DEFAULT '',
    payer_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    payee_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount INTEGER NOT NULL,
    remark TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'paid',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    verified_at DATETIME NULL
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ext_coin_payer ON external_coin_transfers (payer_id, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ext_coin_payee ON external_coin_transfers (payee_id, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_ext_coin_client_order ON external_coin_transfers (client_order_no, created_at DESC)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_ext_coin_payer_client_unique ON external_coin_transfers (payer_id, client_order_no) WHERE client_order_no <> ''`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS resource_reports (
    id VARCHAR(32) PRIMARY KEY,
    item_id VARCHAR(32) NOT NULL REFERENCES resource_items(id) ON DELETE CASCADE,
    reporter_id VARCHAR(32) REFERENCES users(id) ON DELETE SET NULL,
    reporter_uid VARCHAR(32) NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_resource_reports_item ON resource_reports (item_id)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_resource_reports_created ON resource_reports (created_at)`)
	_ = addColumnIfMissing(db, "resource_reports", "status", "TEXT NOT NULL DEFAULT 'pending'")
	_ = addColumnIfMissing(db, "resource_reports", "result", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "resource_reports", "handled_at", "DATETIME NULL")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS user_reports (
    id VARCHAR(32) PRIMARY KEY,
    reporter_id VARCHAR(32) REFERENCES users(id) ON DELETE SET NULL,
    reporter_uid VARCHAR(32) NOT NULL DEFAULT '',
    target_user_id VARCHAR(32) REFERENCES users(id) ON DELETE SET NULL,
    target_uid VARCHAR(32) NOT NULL DEFAULT '',
    target_device_id VARCHAR(128) NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_reports_created ON user_reports (created_at)`)
	_ = addColumnIfMissing(db, "user_reports", "status", "TEXT NOT NULL DEFAULT 'pending'")
	_ = addColumnIfMissing(db, "user_reports", "result", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "user_reports", "handled_at", "DATETIME NULL")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS public_court_cases (
    id VARCHAR(32) PRIMARY KEY,
    report_id VARCHAR(32) NOT NULL DEFAULT '',
    reporter_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reporter_uid VARCHAR(32) NOT NULL DEFAULT '',
    defendant_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    defendant_uid VARCHAR(32) NOT NULL DEFAULT '',
    report_reason TEXT NOT NULL DEFAULT '',
    report_evidence TEXT NOT NULL DEFAULT '',
    defense_reason TEXT NOT NULL DEFAULT '',
    defense_evidence TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',
    verdict TEXT NOT NULL DEFAULT '',
    admin_note TEXT NOT NULL DEFAULT '',
    ban_hours INTEGER NOT NULL DEFAULT 0,
    reward_processed INTEGER NOT NULL DEFAULT 0,
    closed_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_public_court_cases_created ON public_court_cases (created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_public_court_cases_status ON public_court_cases (status, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_public_court_cases_report ON public_court_cases (report_id)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_public_court_open_pair ON public_court_cases (reporter_id, defendant_id) WHERE status = 'open'`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS public_court_votes (
    case_id VARCHAR(32) NOT NULL REFERENCES public_court_cases(id) ON DELETE CASCADE,
    voter_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    vote TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    evidence TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (case_id, voter_id)
)`)
	_ = addColumnIfMissing(db, "public_court_votes", "evidence", "TEXT NOT NULL DEFAULT ''")
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_public_court_votes_case ON public_court_votes (case_id, created_at DESC)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS public_court_statements (
    case_id VARCHAR(32) NOT NULL REFERENCES public_court_cases(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    evidence TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (case_id, user_id)
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_public_court_statements_case ON public_court_statements (case_id, updated_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_public_court_statements_user ON public_court_statements (user_id, updated_at DESC)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS public_court_discussions (
    id VARCHAR(32) PRIMARY KEY,
    case_id VARCHAR(32) NOT NULL REFERENCES public_court_cases(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_public_court_discussions_case ON public_court_discussions (case_id, created_at DESC)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_public_court_discussions_user ON public_court_discussions (user_id, created_at DESC)`)
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS group_reports (
    id VARCHAR(32) PRIMARY KEY,
    reporter_id VARCHAR(32) REFERENCES users(id) ON DELETE SET NULL,
    reporter_uid VARCHAR(32) NOT NULL DEFAULT '',
    group_id VARCHAR(32) REFERENCES groups(id) ON DELETE SET NULL,
    group_name VARCHAR(64) NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_group_reports_created ON group_reports (created_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_group_reports_group ON group_reports (group_id, created_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_group_reports_reporter ON group_reports (reporter_id, created_at)`)
	_ = addColumnIfMissing(db, "group_reports", "status", "TEXT NOT NULL DEFAULT 'pending'")
	_ = addColumnIfMissing(db, "group_reports", "result", "TEXT NOT NULL DEFAULT ''")
	_ = addColumnIfMissing(db, "group_reports", "handled_at", "DATETIME NULL")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS red_packets (
    id VARCHAR(32) PRIMARY KEY,
    creator_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id VARCHAR(32) NULL REFERENCES groups(id) ON DELETE CASCADE,
    to_user_id VARCHAR(32) NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    cover_url VARCHAR(1024) NOT NULL DEFAULT '',
    total_amount INTEGER NOT NULL,
    total_count INTEGER NOT NULL,
    remaining_amount INTEGER NOT NULL,
    remaining_count INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_red_packets_creator ON red_packets (creator_id, created_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_red_packets_group ON red_packets (group_id, created_at)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_red_packets_to_user ON red_packets (to_user_id, created_at)`)
	_ = addColumnIfMissing(db, "red_packets", "cover_url", "VARCHAR(1024) NOT NULL DEFAULT ''")
	_, _ = db.Exec(`
CREATE TABLE IF NOT EXISTS red_packet_claims (
    id VARCHAR(32) PRIMARY KEY,
    packet_id VARCHAR(32) NOT NULL REFERENCES red_packets(id) ON DELETE CASCADE,
    user_id VARCHAR(32) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_red_packet_claim_unique ON red_packet_claims (packet_id, user_id)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_red_packet_claims_packet ON red_packet_claims (packet_id, created_at)`)
}

func addColumnIfMissing(db *sqlx.DB, table, column, colDef string) error {
	rows, err := db.Queryx("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(new(interface{}), &name, new(interface{}), new(interface{}), new(interface{}), new(interface{})); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + colDef)
	return err
}

// EnsureSystemUser creates the SYSTEM user if it doesn't exist
func EnsureSystemUser(db *sqlx.DB) error {
	_, err := db.Exec(`
INSERT OR IGNORE INTO users (id, uid, username, display_name, email, password_hash, verified, created_at)
VALUES ('SYSTEM', 'SYSTEM', '系统通知', '系统通知', 'system@localhost', '', 1, CURRENT_TIMESTAMP)`)
	return err
}
