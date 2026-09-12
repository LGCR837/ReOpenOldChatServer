package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type ResourceReport struct {
	ID          string       `db:"id"`
	ItemID      string       `db:"item_id"`
	ReporterID  string       `db:"reporter_id"`
	ReporterUID string       `db:"reporter_uid"`
	Reason      string       `db:"reason"`
	Status      string       `db:"status"`
	Result      string       `db:"result"`
	HandledAt   sql.NullTime `db:"handled_at"`
	CreatedAt   time.Time    `db:"created_at"`
}

type ResourceReportRow struct {
	ID           string       `db:"id"`
	ItemID       string       `db:"item_id"`
	ItemName     string       `db:"item_name"`
	ItemURL      string       `db:"item_url"`
	ItemSize     int64        `db:"item_size"`
	SectionName  string       `db:"section_name"`
	ReporterID   string       `db:"reporter_id"`
	ReporterUID  string       `db:"reporter_uid"`
	ReporterName string       `db:"reporter_name"`
	Reason       string       `db:"reason"`
	Status       string       `db:"status"`
	Result       string       `db:"result"`
	HandledAt    sql.NullTime `db:"handled_at"`
	CreatedAt    time.Time    `db:"created_at"`
	UploaderUID  string       `db:"uploader_uid"`
	UploaderName string       `db:"uploader_name"`
}

type ResourceReportStore struct {
	db *sqlx.DB
}

func NewResourceReportStore(db *sqlx.DB) *ResourceReportStore {
	return &ResourceReportStore{db: db}
}

func (s *ResourceReportStore) Create(ctx context.Context, report *ResourceReport) error {
	if report == nil {
		return nil
	}
	if report.ID == "" {
		report.ID = NewID()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO resource_reports (id, item_id, reporter_id, reporter_uid, reason, created_at)
VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
`, report.ID, report.ItemID, report.ReporterID, report.ReporterUID, report.Reason)
	return err
}

func (s *ResourceReportStore) ListRecent(ctx context.Context, limit int) ([]ResourceReportRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []ResourceReportRow{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT rr.id, rr.item_id, rr.reporter_id, rr.reporter_uid, rr.reason, rr.status, rr.result, rr.handled_at, rr.created_at,
       i.name AS item_name, i.url AS item_url, i.size_bytes AS item_size,
       s.name AS section_name,
       ru.display_name AS reporter_name,
       uu.uid AS uploader_uid, uu.display_name AS uploader_name
FROM resource_reports rr
JOIN resource_items i ON i.id = rr.item_id
LEFT JOIN resource_sections s ON s.id = i.section_id
LEFT JOIN users uu ON uu.id = i.uploader_id
LEFT JOIN users ru ON ru.id = rr.reporter_id
ORDER BY rr.created_at DESC
LIMIT $1`, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ResourceReportStore) ListByReporter(ctx context.Context, reporterID string, limit int) ([]ResourceReportRow, error) {
	if reporterID == "" {
		return []ResourceReportRow{}, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows := []ResourceReportRow{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT rr.id, rr.item_id, rr.reporter_id, rr.reporter_uid, rr.reason, rr.status, rr.result, rr.handled_at, rr.created_at,
       i.name AS item_name, i.url AS item_url, i.size_bytes AS item_size,
       s.name AS section_name,
       ru.display_name AS reporter_name,
       uu.uid AS uploader_uid, uu.display_name AS uploader_name
FROM resource_reports rr
JOIN resource_items i ON i.id = rr.item_id
LEFT JOIN resource_sections s ON s.id = i.section_id
LEFT JOIN users uu ON uu.id = i.uploader_id
LEFT JOIN users ru ON ru.id = rr.reporter_id
WHERE rr.reporter_id = $1
ORDER BY rr.created_at DESC
LIMIT $2`, reporterID, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *ResourceReportStore) SetStatus(ctx context.Context, id, status, result string) error {
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
UPDATE resource_reports
SET status = $1,
    result = $2,
    handled_at = CASE WHEN $3 IS NULL THEN handled_at ELSE $3 END
WHERE id = $4
`, status, result, handledAt, id)
	return err
}

func (s *ResourceReportStore) DeleteByItemID(ctx context.Context, itemID string) error {
	if itemID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM resource_reports WHERE item_id = $1`, itemID)
	return err
}

func (s *ResourceReportStore) DeleteByID(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM resource_reports WHERE id = $1`, id)
	return err
}
