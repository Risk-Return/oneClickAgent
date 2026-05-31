package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// FileStore handles file metadata persistence.
type FileStore struct {
	db *DB
}

func NewFileStore(db *DB) *FileStore {
	return &FileStore{db: db}
}

func (s *FileStore) Create(ctx context.Context, f *model.File) error {
	f.ID = model.NewUUID()
	f.CreatedAt = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO files (id, user_id, file_name, size_bytes, mime_type, sha256, status, storage_path, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		f.ID, f.UserID, f.FileName, f.SizeBytes, f.MimeType, f.SHA256, f.Status, f.StoragePath, f.CreatedAt,
	)
	return err
}

func (s *FileStore) GetByID(ctx context.Context, id model.UUID) (*model.File, error) {
	f := &model.File{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, user_id, file_name, size_bytes, mime_type, sha256, status, storage_path, created_at, purged_at
		 FROM files WHERE id = $1`, id,
	).Scan(&f.ID, &f.UserID, &f.FileName, &f.SizeBytes, &f.MimeType, &f.SHA256, &f.Status, &f.StoragePath, &f.CreatedAt, &f.PurgedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (s *FileStore) UpdateStatus(ctx context.Context, id model.UUID, status model.FileStatus) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE files SET status=$2 WHERE id=$1`, id, status,
	)
	return err
}

func (s *FileStore) MarkPurged(ctx context.Context, id model.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE files SET status=$2, purged_at=$3 WHERE id=$1`, id, model.FilePurged, now,
	)
	return err
}

func (s *FileStore) UpdateSHA256(ctx context.Context, id model.UUID, sha256 string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE files SET sha256=$2 WHERE id=$1`, id, sha256,
	)
	return err
}

func (s *FileStore) ListByUser(ctx context.Context, userID model.UUID, cursor *model.UUID, limit int) ([]model.File, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, user_id, file_name, size_bytes, mime_type, sha256, status, storage_path, created_at, purged_at
			 FROM files WHERE user_id=$1 AND status != $2 ORDER BY created_at DESC LIMIT $3`,
			userID, model.FilePurged, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, user_id, file_name, size_bytes, mime_type, sha256, status, storage_path, created_at, purged_at
			 FROM files WHERE user_id=$1 AND status != $2 AND created_at < (SELECT created_at FROM files WHERE id=$3)
			 ORDER BY created_at DESC LIMIT $4`,
			userID, model.FilePurged, *cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var files []model.File
	for rows.Next() {
		var f model.File
		if err := rows.Scan(&f.ID, &f.UserID, &f.FileName, &f.SizeBytes, &f.MimeType, &f.SHA256, &f.Status, &f.StoragePath, &f.CreatedAt, &f.PurgedAt); err != nil {
			return nil, nil, err
		}
		files = append(files, f)
	}

	var nextCursor *model.UUID
	if len(files) > limit {
		nextCursor = &files[limit-1].ID
		files = files[:limit]
	}

	return files, nextCursor, nil
}

func (s *FileStore) ListStagedCloud(ctx context.Context, olderThan time.Time) ([]model.File, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, file_name, size_bytes, mime_type, sha256, status, storage_path, created_at, purged_at
		 FROM files WHERE status=$1 AND created_at < $2`, model.FileStagedCloud, olderThan,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []model.File
	for rows.Next() {
		var f model.File
		if err := rows.Scan(&f.ID, &f.UserID, &f.FileName, &f.SizeBytes, &f.MimeType, &f.SHA256, &f.Status, &f.StoragePath, &f.CreatedAt, &f.PurgedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func (s *FileStore) LinkToJob(ctx context.Context, fileID, jobID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO job_files (job_id, file_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		jobID, fileID,
	)
	return err
}

func (s *FileStore) ListByJob(ctx context.Context, jobID model.UUID) ([]model.File, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT f.id, f.user_id, f.file_name, f.size_bytes, f.mime_type, f.sha256, f.status, f.storage_path, f.created_at, f.purged_at
		 FROM files f JOIN job_files jf ON f.id = jf.file_id WHERE jf.job_id=$1`, jobID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []model.File
	for rows.Next() {
		var f model.File
		if err := rows.Scan(&f.ID, &f.UserID, &f.FileName, &f.SizeBytes, &f.MimeType, &f.SHA256, &f.Status, &f.StoragePath, &f.CreatedAt, &f.PurgedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}
