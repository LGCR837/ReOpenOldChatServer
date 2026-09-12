package data

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// EnsureNCUIDColumn 给历史库补 users.ncuid 列并回填。
// 第三方库结构不必对齐官方，只需自身前后一致：缺列则补，空值用当前 uid 回填。
func EnsureNCUIDColumn(ctx context.Context, db *sqlx.DB) {
	var has bool
	row := db.QueryRowxContext(ctx, `SELECT COUNT(1) FROM pragma_table_info('users') WHERE name = 'ncuid'`)
	if err := row.Scan(&has); err != nil {
		return
	}
	if !has {
		_, _ = db.ExecContext(ctx, `ALTER TABLE users ADD COLUMN ncuid VARCHAR(32) NOT NULL DEFAULT ''`)
	}
	_, _ = db.ExecContext(ctx, `UPDATE users SET ncuid = uid WHERE ncuid = ''`)
	_, _ = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_users_ncuid ON users (ncuid)`)
}
