package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

type DeviceStore struct{ db *DB }

func NewDeviceStore(db *DB) *DeviceStore { return &DeviceStore{db: db} }

func (s *DeviceStore) Create(ctx context.Context, d *model.Device) error {
	d.ID = model.NewUUID()
	d.CreatedAt = time.Now().UTC()
	d.UpdatedAt = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO devices (id, operator_id, name, description, platform, status, token_hash, token_rotated_at, resources, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		d.ID, d.OperatorID, d.Name, d.Description, d.Platform, d.Status,
		d.TokenHash, d.TokenRotatedAt, d.Resources, d.CreatedAt, d.UpdatedAt,
	)
	return err
}

func (s *DeviceStore) GetByID(ctx context.Context, id model.UUID) (*model.Device, error) {
	d := &model.Device{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, operator_id, name, description, platform, status, token_hash, token_rotated_at, last_seen_at, resources, created_at, updated_at
		 FROM devices WHERE id=$1`, id,
	).Scan(&d.ID, &d.OperatorID, &d.Name, &d.Description, &d.Platform, &d.Status,
		&d.TokenHash, &d.TokenRotatedAt, &d.LastSeenAt, &d.Resources, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (s *DeviceStore) UpdateToken(ctx context.Context, id model.UUID, tokenHash string) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE devices SET token_hash=$2, token_rotated_at=$3 WHERE id=$1`, id, tokenHash, now,
	)
	return err
}

func (s *DeviceStore) Update(ctx context.Context, d *model.Device) error {
	d.UpdatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE devices SET name=$2, description=$3, updated_at=$4 WHERE id=$1`,
		d.ID, d.Name, d.Description, d.UpdatedAt,
	)
	return err
}

func (s *DeviceStore) UpdateStatus(ctx context.Context, id model.UUID, status model.DeviceStatus) error {
	var err error
	if status == model.DeviceOnline {
		now := time.Now().UTC()
		_, err = s.db.Pool.Exec(ctx,
			`UPDATE devices SET status=$2, last_seen_at=$3 WHERE id=$1`, id, status, now,
		)
	} else {
		_, err = s.db.Pool.Exec(ctx,
			`UPDATE devices SET status=$2 WHERE id=$1`, id, status,
		)
	}
	return err
}

func (s *DeviceStore) UpdatePlatform(ctx context.Context, id model.UUID, platform string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE devices SET platform=$2 WHERE id=$1`, id, platform,
	)
	return err
}

func (s *DeviceStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.Device, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}
	cols := `id, operator_id, name, description, platform, status, token_hash, token_rotated_at, last_seen_at, resources, created_at, updated_at`

	query := `SELECT ` + cols + ` FROM devices ORDER BY created_at DESC LIMIT $1`
	args := []interface{}{limit + 1}
	if cursor != nil {
		query = `SELECT ` + cols + ` FROM devices WHERE created_at < (SELECT created_at FROM devices WHERE id=$1) ORDER BY created_at DESC LIMIT $2`
		args = []interface{}{*cursor, limit + 1}
	}

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var devices []model.Device
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.OperatorID, &d.Name, &d.Description, &d.Platform, &d.Status,
			&d.TokenHash, &d.TokenRotatedAt, &d.LastSeenAt, &d.Resources, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, nil, err
		}
		devices = append(devices, d)
	}

	var nextCursor *model.UUID
	if len(devices) > limit {
		nextCursor = &devices[limit-1].ID
		devices = devices[:limit]
	}
	return devices, nextCursor, nil
}

func (s *DeviceStore) ListAll(ctx context.Context) ([]model.Device, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, operator_id, name, description, platform, status, token_hash, token_rotated_at, last_seen_at, resources, created_at, updated_at
		 FROM devices ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDevices(rows)
}

// GetEnrolledDevice retrieves a device by enrollment code.
func (s *DeviceStore) GetEnrolledDevice(ctx context.Context, code string) (*model.Device, error) {
	d := &model.Device{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, operator_id, name, description, platform, status, token_hash, token_rotated_at, last_seen_at, resources, created_at, updated_at
		 FROM devices WHERE token_hash=$1 AND status=$2`,
		code, model.DeviceEnrolled,
	).Scan(&d.ID, &d.OperatorID, &d.Name, &d.Description, &d.Platform, &d.Status,
		&d.TokenHash, &d.TokenRotatedAt, &d.LastSeenAt, &d.Resources, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return d, err
}

// GetByTokenHash retrieves a device by its token hash.
func (s *DeviceStore) GetByTokenHash(ctx context.Context, tokenHash string) (*model.Device, error) {
	d := &model.Device{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, operator_id, name, description, platform, status, token_hash, token_rotated_at, last_seen_at, resources, created_at, updated_at
		 FROM devices WHERE token_hash=$1`,
		tokenHash,
	).Scan(&d.ID, &d.OperatorID, &d.Name, &d.Description, &d.Platform, &d.Status,
		&d.TokenHash, &d.TokenRotatedAt, &d.LastSeenAt, &d.Resources, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *DeviceStore) Delete(ctx context.Context, id model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM devices WHERE id=$1`, id)
	return err
}

func scanDevices(rows pgx.Rows) ([]model.Device, error) {
	var devices []model.Device
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.OperatorID, &d.Name, &d.Description, &d.Platform, &d.Status,
			&d.TokenHash, &d.TokenRotatedAt, &d.LastSeenAt, &d.Resources, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, nil
}
