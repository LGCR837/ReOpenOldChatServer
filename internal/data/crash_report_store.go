package data

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

type CrashReport struct {
	ID             string    `db:"id"`
	UserID         *string   `db:"user_id"`
	CrashLog       string    `db:"crash_log"`
	DeviceModel    string    `db:"device_model"`
	AndroidVersion string    `db:"android_version"`
	CreatedAt      time.Time `db:"created_at"`
}

type CrashReportStore struct {
	db *sqlx.DB
}

func NewCrashReportStore(db *sqlx.DB) *CrashReportStore {
	return &CrashReportStore{db: db}
}

func (s *CrashReportStore) Create(ctx context.Context, report *CrashReport) error {
	query := `
		INSERT INTO crash_reports (id, user_id, crash_log, device_model, android_version, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		report.ID,
		report.UserID,
		report.CrashLog,
		report.DeviceModel,
		report.AndroidVersion,
		time.Now(),
	)
	return err
}

func (s *CrashReportStore) ListRecent(ctx context.Context, limit int) ([]CrashReport, error) {
	query := `
		SELECT id, user_id, crash_log, device_model, android_version, created_at
		FROM crash_reports
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []CrashReport
	for rows.Next() {
		var report CrashReport
		err := rows.Scan(
			&report.ID,
			&report.UserID,
			&report.CrashLog,
			&report.DeviceModel,
			&report.AndroidVersion,
			&report.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, rows.Err()
}

func (s *CrashReportStore) GetByID(ctx context.Context, id string) (*CrashReport, error) {
	query := `
		SELECT id, user_id, crash_log, device_model, android_version, created_at
		FROM crash_reports
		WHERE id = ?
	`
	var report CrashReport
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&report.ID,
		&report.UserID,
		&report.CrashLog,
		&report.DeviceModel,
		&report.AndroidVersion,
		&report.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &report, nil
}

func (s *CrashReportStore) Count(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM crash_reports`
	var count int
	err := s.db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}
