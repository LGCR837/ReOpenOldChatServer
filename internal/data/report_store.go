package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type UserReport struct {
	ID           string       `db:"id"`
	ReporterID   string       `db:"reporter_id"`
	ReporterUID  string       `db:"reporter_uid"`
	TargetUserID string       `db:"target_user_id"`
	TargetUID    string       `db:"target_uid"`
	TargetDevice string       `db:"target_device_id"`
	Reason       string       `db:"reason"`
	Status       string       `db:"status"`
	Result       string       `db:"result"`
	HandledAt    sql.NullTime `db:"handled_at"`
	CreatedAt    time.Time    `db:"created_at"`
}

type ReportStore struct {
	db *sqlx.DB
}

func NewReportStore(db *sqlx.DB) *ReportStore {
	return &ReportStore{db: db}
}

func (s *ReportStore) CreateUserReport(ctx context.Context, report *UserReport) error {
	if report == nil {
		return nil
	}
	if report.ID == "" {
		report.ID = NewID()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO user_reports (id, reporter_id, reporter_uid, target_user_id, target_uid, target_device_id, reason, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
`, report.ID, report.ReporterID, report.ReporterUID, report.TargetUserID, report.TargetUID, report.TargetDevice, report.Reason)
	return err
}

func (s *ReportStore) ListRecentUserReports(ctx context.Context, limit int) ([]UserReport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []UserReport{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT id, reporter_id, reporter_uid, target_user_id, target_uid, target_device_id, reason, status, result, handled_at, created_at
FROM user_reports
ORDER BY created_at DESC
LIMIT $1`, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ReportStore) ListUserReportsByReporter(ctx context.Context, reporterID string, limit int) ([]UserReport, error) {
	if reporterID == "" {
		return []UserReport{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []UserReport{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT id, reporter_id, reporter_uid, target_user_id, target_uid, target_device_id, reason, status, result, handled_at, created_at
FROM user_reports
WHERE reporter_id = $1
ORDER BY created_at DESC
LIMIT $2`, reporterID, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ReportStore) SetUserReportStatus(ctx context.Context, id, status, result string) error {
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
UPDATE user_reports
SET status = $1,
    result = $2,
    handled_at = CASE WHEN $3 IS NULL THEN handled_at ELSE $3 END
WHERE id = $4
`, status, result, handledAt, id)
	return err
}
