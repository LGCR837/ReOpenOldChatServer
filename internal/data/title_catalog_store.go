package data

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type TitleCatalogRow struct {
	ID        string         `db:"id"`
	Title     string         `db:"title"`
	Price     int            `db:"price"`
	Active    int            `db:"active"`
	IsCustom  int            `db:"is_custom"`
	OwnerID   sql.NullString `db:"owner_id"`
	CreatedAt time.Time      `db:"created_at"`
	UpdatedAt time.Time      `db:"updated_at"`
}

type TitleCatalogStore struct {
	db *sqlx.DB
}

func NewTitleCatalogStore(db *sqlx.DB) *TitleCatalogStore {
	return &TitleCatalogStore{db: db}
}

func (s *TitleCatalogStore) EnsureTable(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
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
	if err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "title_catalog", "owner_id", "VARCHAR(32) NULL REFERENCES users(id) ON DELETE SET NULL"); err != nil {
		return err
	}
	if err := addColumnIfMissing(s.db, "title_catalog", "is_custom", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_title_catalog_title ON title_catalog (title)`)
	_, _ = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_title_catalog_owner ON title_catalog (owner_id)`)
	return nil
}

func (s *TitleCatalogStore) List(ctx context.Context, includeInactive bool) ([]TitleCatalogRow, error) {
	q := `SELECT id, title, price, active, is_custom, owner_id, created_at, updated_at FROM title_catalog`
	if !includeInactive {
		q += " WHERE active = 1"
	}
	q += " ORDER BY created_at DESC"
	rows := []TitleCatalogRow{}
	if err := s.db.SelectContext(ctx, &rows, q); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *TitleCatalogStore) GetByID(ctx context.Context, id string) (*TitleCatalogRow, error) {
	var row TitleCatalogRow
	if err := s.db.GetContext(ctx, &row, `SELECT id, title, price, active, is_custom, owner_id, created_at, updated_at FROM title_catalog WHERE id = $1`, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (s *TitleCatalogStore) GetByTitle(ctx context.Context, title string) (*TitleCatalogRow, error) {
	t := strings.TrimSpace(title)
	if t == "" {
		return nil, ErrNotFound
	}
	var row TitleCatalogRow
	if err := s.db.GetContext(ctx, &row, `SELECT id, title, price, active, is_custom, owner_id, created_at, updated_at FROM title_catalog WHERE title = $1`, t); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (s *TitleCatalogStore) ListAvailable(ctx context.Context) ([]TitleCatalogRow, error) {
	rows := []TitleCatalogRow{}
	if err := s.db.SelectContext(ctx, &rows, `
SELECT id, title, price, active, is_custom, owner_id, created_at, updated_at
FROM title_catalog
WHERE active = 1 AND owner_id IS NULL AND is_custom = 0
ORDER BY created_at DESC`); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *TitleCatalogStore) Create(ctx context.Context, title string, price int) error {
	t := strings.TrimSpace(title)
	if t == "" {
		return ErrNotFound
	}
	if price <= 0 {
		price = 100
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO title_catalog (id, title, price, active, created_at, updated_at)
VALUES ($1, $2, $3, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
`, NewID(), t, price)
	return err
}

func (s *TitleCatalogStore) Update(ctx context.Context, id, title string, price int) error {
	t := strings.TrimSpace(title)
	if t == "" {
		return ErrNotFound
	}
	if price <= 0 {
		price = 100
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE title_catalog
SET title = $1, price = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $3
`, t, price, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *TitleCatalogStore) UpdateByTitle(ctx context.Context, title string, price int) error {
	t := strings.TrimSpace(title)
	if t == "" {
		return ErrNotFound
	}
	if price <= 0 {
		price = 100
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE title_catalog
SET price = $1, active = 1, updated_at = CURRENT_TIMESTAMP
WHERE title = $2
`, price, t)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *TitleCatalogStore) SetActive(ctx context.Context, id string, active bool) error {
	activeValue := 0
	if active {
		activeValue = 1
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE title_catalog
SET active = $1, updated_at = CURRENT_TIMESTAMP
WHERE id = $2
`, activeValue, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *TitleCatalogStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM title_catalog WHERE id = $1`, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
