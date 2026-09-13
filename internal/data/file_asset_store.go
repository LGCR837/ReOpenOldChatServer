package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

// FileAsset 是按 SHA-256 去重的文件资产。同一份内容只落一份磁盘文件，
// 多个消息 / 资源条目可共享同一个 id。
type FileAsset struct {
	ID         string    `db:"id"`
	SHA256     string    `db:"sha256"`
	Name       string    `db:"name"`
	StoredName string    `db:"stored_name"`
	SizeBytes  int64     `db:"size_bytes"`
	OwnerID    string    `db:"owner_id"`
	Created    time.Time `db:"created_at"`
}

const fileAssetColumns = `id, sha256, name, stored_name, size_bytes, COALESCE(owner_id, '') AS owner_id, created_at`

type FileAssetStore struct {
	db *sqlx.DB
}

func NewFileAssetStore(db *sqlx.DB) *FileAssetStore {
	return &FileAssetStore{db: db}
}

// FindBySHA256 秒传检查：命中则说明内容已在库里。
func (s *FileAssetStore) FindBySHA256(ctx context.Context, sha256 string) (*FileAsset, error) {
	if sha256 == "" {
		return nil, ErrNotFound
	}
	var out FileAsset
	err := s.db.GetContext(ctx, &out, `
SELECT `+fileAssetColumns+`
FROM file_assets
WHERE sha256 = $1
LIMIT 1`, sha256)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func (s *FileAssetStore) GetByID(ctx context.Context, id string) (*FileAsset, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	var out FileAsset
	err := s.db.GetContext(ctx, &out, `
SELECT `+fileAssetColumns+`
FROM file_assets
WHERE id = $1
LIMIT 1`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

// Create 落一条文件资产。sha256 上有唯一索引：并发同内容上传时靠它挡掉重复，
// 返回 created=false 由调用方改走「用已存在那条」的路径。
func (s *FileAssetStore) Create(ctx context.Context, a *FileAsset) (bool, error) {
	if a == nil || a.ID == "" || a.SHA256 == "" {
		return false, ErrNotFound
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO file_assets (id, sha256, name, stored_name, size_bytes, owner_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING
`, a.ID, a.SHA256, a.Name, a.StoredName, a.SizeBytes, nullableID(a.OwnerID))
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// nullableID 把空串转成 NULL，避免外键列被塞进 ” 而报约束错误。
func nullableID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
