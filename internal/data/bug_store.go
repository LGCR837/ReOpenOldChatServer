package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type BugReport struct {
	ID             string       `db:"id"`
	UserID         string       `db:"user_id"`
	UserUID        string       `db:"user_uid"`
	Content        string       `db:"content"`
	DeviceModel    string       `db:"device_model"`
	AndroidVersion string       `db:"android_version"`
	AppVersion     string       `db:"app_version"`
	Status         string       `db:"status"`
	AdminNote      string       `db:"admin_note"`
	ResolvedAt     sql.NullTime `db:"resolved_at"`
	CreatedAt      time.Time    `db:"created_at"`
}

type BugReportStore struct {
	db *sqlx.DB
}

func NewBugReportStore(db *sqlx.DB) *BugReportStore {
	return &BugReportStore{db: db}
}

func (s *BugReportStore) EnsureTable(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
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
	return err
}

func (s *BugReportStore) Create(ctx context.Context, report *BugReport) error {
	if report == nil {
		return nil
	}
	if report.ID == "" {
		report.ID = NewID()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO bug_reports (id, user_id, user_uid, content, device_model, android_version, app_version, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
`, report.ID, report.UserID, report.UserUID, report.Content, report.DeviceModel, report.AndroidVersion, report.AppVersion)
	return err
}

func (s *BugReportStore) ListRecent(ctx context.Context, limit int) ([]BugReport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []BugReport{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT id, user_id, user_uid, content, device_model, android_version, app_version, status, admin_note, resolved_at, created_at
FROM bug_reports
ORDER BY created_at DESC
LIMIT $1`, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *BugReportStore) ListByUser(ctx context.Context, userID string, limit int) ([]BugReport, error) {
	if userID == "" {
		return []BugReport{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []BugReport{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT id, user_id, user_uid, content, device_model, android_version, app_version, status, admin_note, resolved_at, created_at
FROM bug_reports
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2`, userID, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *BugReportStore) SetStatus(ctx context.Context, id, status, note string) error {
	if id == "" {
		return nil
	}
	if status == "" {
		status = "open"
	}
	resolvedAt := interface{}(nil)
	if status == "resolved" || status == "closed" {
		resolvedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE bug_reports
SET status = $1,
    admin_note = $2,
    resolved_at = CASE WHEN $3 IS NULL THEN resolved_at ELSE $3 END
WHERE id = $4
`, status, note, resolvedAt, id)
	return err
}

func (s *BugReportStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bug_reports WHERE id = $1`, id)
	return err
}
