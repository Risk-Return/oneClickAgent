package store

import (
	"context"
	"fmt"
	"time"

	"github.com/iagent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// DeviceStore handles device persistence.
type DeviceStore struct {
	db *DB
}

func NewDeviceStore(db *DB) *DeviceStore {
	return &DeviceStore{db: db}
}

func (s *DeviceStore) Create(ctx context.Context, device *model.Device) error {
	device.ID = model.NewUUID()
	device.CreatedAt = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO devices (id, device_name, device_token_hash, status, host_info, agent_pool_size, enrollment_code, enrollment_code_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		device.ID, device.DeviceName, device.DeviceTokenHash, device.Status,
		device.HostInfo, device.AgentPoolSize, device.EnrollmentCode, device.EnrollmentCodeAt, device.CreatedAt,
	)
	return err
}

func (s *DeviceStore) GetByID(ctx context.Context, id model.UUID) (*model.Device, error) {
	d := &model.Device{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, device_name, device_token_hash, status, host_info, agent_pool_size, created_at, last_seen_at
		 FROM devices WHERE id = $1`, id,
	).Scan(&d.ID, &d.DeviceName, &d.DeviceTokenHash, &d.Status, &d.HostInfo, &d.AgentPoolSize, &d.CreatedAt, &d.LastSeenAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (s *DeviceStore) GetByEnrollmentCode(ctx context.Context, code string) (*model.Device, error) {
	d := &model.Device{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, device_name, device_token_hash, status, host_info, agent_pool_size, created_at, last_seen_at
		 FROM devices WHERE enrollment_code = $1 AND status = $2`, code, model.DeviceEnrolled,
	).Scan(&d.ID, &d.DeviceName, &d.DeviceTokenHash, &d.Status, &d.HostInfo, &d.AgentPoolSize, &d.CreatedAt, &d.LastSeenAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (s *DeviceStore) UpdateToken(ctx context.Context, id model.UUID, tokenHash string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE devices SET device_token_hash=$2 WHERE id=$1`, id, tokenHash,
	)
	return err
}

func (s *DeviceStore) UpdateStatus(ctx context.Context, id model.UUID, status model.DeviceStatus) error {
	if status == model.DeviceOnline {
		now := time.Now().UTC()
		_, err := s.db.Pool.Exec(ctx,
			`UPDATE devices SET status=$2, last_seen_at=$3 WHERE id=$1`, id, status, now,
		)
		return err
	}
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE devices SET status=$2 WHERE id=$1`, id, status,
	)
	return err
}

func (s *DeviceStore) UpdatePoolSize(ctx context.Context, id model.UUID, size int) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE devices SET agent_pool_size=$2 WHERE id=$1`, id, size,
	)
	return err
}

func (s *DeviceStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.Device, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, device_name, device_token_hash, status, host_info, agent_pool_size, created_at, last_seen_at
	           FROM devices ORDER BY created_at DESC LIMIT $1`
	args := []interface{}{limit + 1}

	if cursor != nil {
		query = `SELECT id, device_name, device_token_hash, status, host_info, agent_pool_size, created_at, last_seen_at
		         FROM devices WHERE created_at < (SELECT created_at FROM devices WHERE id=$1)
		         ORDER BY created_at DESC LIMIT $2`
		args = []interface{}{*cursor, limit + 1}
	}

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	var devices []model.Device
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.DeviceName, &d.DeviceTokenHash, &d.Status, &d.HostInfo, &d.AgentPoolSize, &d.CreatedAt, &d.LastSeenAt); err != nil {
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
		`SELECT id, device_name, device_token_hash, status, host_info, agent_pool_size, created_at, last_seen_at
		 FROM devices ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []model.Device
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.DeviceName, &d.DeviceTokenHash, &d.Status, &d.HostInfo, &d.AgentPoolSize, &d.CreatedAt, &d.LastSeenAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func (s *DeviceStore) ListByStatus(ctx context.Context, status model.DeviceStatus) ([]model.Device, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, device_name, device_token_hash, status, host_info, agent_pool_size, created_at, last_seen_at
		 FROM devices WHERE status=$1 ORDER BY created_at`, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []model.Device
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.DeviceName, &d.DeviceTokenHash, &d.Status, &d.HostInfo, &d.AgentPoolSize, &d.CreatedAt, &d.LastSeenAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func (s *DeviceStore) Delete(ctx context.Context, id model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM devices WHERE id=$1`, id)
	return err
}
