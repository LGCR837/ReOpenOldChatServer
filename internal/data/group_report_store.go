package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type GroupReport struct {
	ID          string       `db:"id"`
	ReporterID  string       `db:"reporter_id"`
	ReporterUID string       `db:"reporter_uid"`
	GroupID     string       `db:"group_id"`
	GroupName   string       `db:"group_name"`
	Reason      string       `db:"reason"`
	Status      string       `db:"status"`
	Result      string       `db:"result"`
	HandledAt   sql.NullTime `db:"handled_at"`
	CreatedAt   time.Time    `db:"created_at"`
}

type GroupReportStore struct {
	db *sqlx.DB
}

func NewGroupReportStore(db *sqlx.DB) *GroupReportStore {
	return &GroupReportStore{db: db}
}

func (s *GroupReportStore) Create(ctx context.Context, report *GroupReport) error {
	if report == nil {
		return nil
	}
	if report.ID == "" {
		report.ID = NewID()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO group_reports (id, reporter_id, reporter_uid, group_id, group_name, reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
`, report.ID, report.ReporterID, report.ReporterUID, report.GroupID, report.GroupName, report.Reason)
	return err
}

func (s *GroupReportStore) ListRecent(ctx context.Context, limit int) ([]GroupReport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []GroupReport{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT id, reporter_id, reporter_uid, group_id, group_name, reason, status, result, handled_at, created_at
FROM group_reports
ORDER BY created_at DESC
LIMIT $1`, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *GroupReportStore) ListByReporter(ctx context.Context, reporterID string, limit int) ([]GroupReport, error) {
	if reporterID == "" {
		return []GroupReport{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []GroupReport{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT id, reporter_id, reporter_uid, group_id, group_name, reason, status, result, handled_at, created_at
FROM group_reports
WHERE reporter_id = $1
ORDER BY created_at DESC
LIMIT $2`, reporterID, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *GroupReportStore) SetStatus(ctx context.Context, id, status, result string) error {
	if id == "" {
		return nil
	}
	if status == "" {
		status = "pending"
	}
	handledAt := interface{}(nil)
	if status != "pending" {
		handledAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE group_reports
SET status = $1,
    result = $2,
    handled_at = CASE WHEN $3 IS NULL THEN handled_at ELSE $3 END
WHERE id = $4
`, status, result, handledAt, id)
	return err
}
