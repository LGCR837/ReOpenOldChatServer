package data

import (
	"context"

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
