package data

import (
	"context"
	"log"

	"github.com/jmoiron/sqlx"
)

// EnsureNCUIDColumn 给历史库补 ncuid 相关列并回填。
// 第三方库结构不必对齐官方，只需自身前后一致：缺列则补，空值用当前 uid 回填。
func EnsureNCUIDColumn(ctx context.Context, db *sqlx.DB) {
	ensureColumn(ctx, db, "users", "ncuid", `UPDATE users SET ncuid = uid WHERE ncuid = ''`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_users_ncuid ON users (ncuid)`)

	ensureColumn(ctx, db, "direct_messages", "sender_ncuid", `
UPDATE direct_messages
SET sender_ncuid = COALESCE((SELECT u.ncuid FROM users u WHERE u.id = direct_messages.sender_id), '')
WHERE sender_ncuid = ''`)

	ensureColumn(ctx, db, "group_messages", "sender_ncuid", `
UPDATE group_messages
SET sender_ncuid = COALESCE((SELECT u.ncuid FROM users u WHERE u.id = group_messages.sender_id), '')
WHERE sender_ncuid = ''`)

	EnsurePTSColumns(ctx, db)
}

// EnsurePTSColumns 建 pts 事件流所需表。schema.sql 只在新库生效，老库必须靠这里补。
// 注意 SQLite 的 AUTOINCREMENT 只接受 INTEGER PRIMARY KEY 这一种写法，
// 用 BIGINT 会整条 DDL 失败（且 ExecContext 的 error 被忽略，症状是「表莫名不存在」）。
func EnsurePTSColumns(ctx context.Context, db *sqlx.DB) {
	ensureTable(ctx, db, `
CREATE TABLE IF NOT EXISTS account_updates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id VARCHAR(32) NOT NULL,
    pts INTEGER NOT NULL,
    pts_count INTEGER NOT NULL DEFAULT 1,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_account_updates_user_pts ON account_updates (user_id, pts)`)
	ensureTable(ctx, db, `
CREATE TABLE IF NOT EXISTS pts_state (
    key TEXT PRIMARY KEY,
    value INTEGER NOT NULL DEFAULT 0
)`)
}

// ensureTable 建表并落地失败原因，避免 DDL 语法错误被无声吞掉。
func ensureTable(ctx context.Context, db *sqlx.DB, ddl string) {
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		log.Printf("data: ensure table failed: %v", err)
	}
}

func ensureColumn(ctx context.Context, db *sqlx.DB, table, column, backfill string) {
	var has bool
	row := db.QueryRowxContext(ctx, `SELECT COUNT(1) FROM pragma_table_info(?) WHERE name = ?`, table, column)
	if err := row.Scan(&has); err != nil {
		return
	}
	if !has {
		_, _ = db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` VARCHAR(32) NOT NULL DEFAULT ''`)
	}
	if backfill != "" {
		_, _ = db.ExecContext(ctx, backfill)
	}
}
