package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// VNCSessionStore handles VNC session persistence.
type VNCSessionStore struct{ db *DB }

func NewVNCSessionStore(db *DB) *VNCSessionStore { return &VNCSessionStore{db: db} }

func (s *VNCSessionStore) Create(ctx context.Context, sess *model.VNCSession) error {
	sess.ID = model.NewUUID()
	sess.CreatedAt = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO vnc_sessions (id, job_id, user_id, device_id, agent_id, session_token_hash,
		 status, gateway_node, idle_ttl_secs, max_ttl_secs, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		sess.ID, sess.JobID, sess.UserID, sess.DeviceID, sess.AgentID, sess.SessionTokenHash,
		sess.Status, sess.GatewayNode, sess.IdleTTLSecs, sess.MaxTTLSecs, sess.CreatedAt,
	)
	return err
}

func (s *VNCSessionStore) GetByID(ctx context.Context, id model.UUID) (*model.VNCSession, error) {
	sess := &model.VNCSession{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, job_id, user_id, device_id, agent_id, session_token_hash, rfb_password,
		 status, gateway_node, idle_ttl_secs, max_ttl_secs, last_active_at, created_at, closed_at
		 FROM vnc_sessions WHERE id=$1`, id,
	).Scan(&sess.ID, &sess.JobID, &sess.UserID, &sess.DeviceID, &sess.AgentID, &sess.SessionTokenHash,
		&sess.RFBPassword, &sess.Status, &sess.GatewayNode, &sess.IdleTTLSecs, &sess.MaxTTLSecs,
		&sess.LastActiveAt, &sess.CreatedAt, &sess.ClosedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return sess, err
}

func (s *VNCSessionStore) UpdateStatus(ctx context.Context, id model.UUID, status model.VNCSessionStatus, rfbPassword string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE vnc_sessions SET status=$2, rfb_password=$3, last_active_at=$4 WHERE id=$1`,
		id, status, rfbPassword, time.Now().UTC(),
	)
	return err
}

func (s *VNCSessionStore) Close(ctx context.Context, id model.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE vnc_sessions SET status='closed', closed_at=$2 WHERE id=$1`, id, now,
	)
	return err
}

func (s *VNCSessionStore) Touch(ctx context.Context, id model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE vnc_sessions SET last_active_at=$2 WHERE id=$1`, id, time.Now().UTC(),
	)
	return err
}

func (s *VNCSessionStore) CountActiveByUser(ctx context.Context, userID model.UUID) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM vnc_sessions WHERE user_id=$1 AND status IN ('pending','ready','active')`, userID,
	).Scan(&count)
	return count, err
}

func (s *VNCSessionStore) ListActive(ctx context.Context) ([]model.VNCSession, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, job_id, user_id, device_id, agent_id, session_token_hash, rfb_password,
		 status, gateway_node, idle_ttl_secs, max_ttl_secs, last_active_at, created_at, closed_at
		 FROM vnc_sessions WHERE status IN ('pending','ready','active') ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []model.VNCSession
	for rows.Next() {
		var sess model.VNCSession
		if err := rows.Scan(&sess.ID, &sess.JobID, &sess.UserID, &sess.DeviceID, &sess.AgentID, &sess.SessionTokenHash,
			&sess.RFBPassword, &sess.Status, &sess.GatewayNode, &sess.IdleTTLSecs, &sess.MaxTTLSecs,
			&sess.LastActiveAt, &sess.CreatedAt, &sess.ClosedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}
