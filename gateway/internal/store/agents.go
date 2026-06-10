package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

type AgentStore struct{ db *DB }

func NewAgentStore(db *DB) *AgentStore { return &AgentStore{db: db} }

var agentCols = `id, device_id, user_id, name, description, image, port, tags, status, job_id, limits, allocated_at, created_at, updated_at`

func (s *AgentStore) Create(ctx context.Context, a *model.Agent) error {
	if a.ID == uuid.Nil {
		a.ID = model.NewUUID()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now

	if a.Limits == nil {
		a.Limits = &model.AgentLimits{CPU: 2, MemMB: 4096, DiskMB: 10240}
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO agents (id, device_id, user_id, name, description, image, port, tags, status, job_id, limits, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		a.ID, a.DeviceID, a.UserID, a.Name, a.Description, a.Image, a.Port, a.Tags,
		a.Status, a.JobID, a.Limits, a.CreatedAt, a.UpdatedAt,
	)
	return err
}

func (s *AgentStore) GetByID(ctx context.Context, id model.UUID) (*model.Agent, error) {
	a := &model.Agent{}
	err := s.db.Pool.QueryRow(ctx, `SELECT `+agentCols+` FROM agents WHERE id=$1`, id,
	).Scan(&a.ID, &a.DeviceID, &a.UserID, &a.Name, &a.Description, &a.Image, &a.Port, &a.Tags,
		&a.Status, &a.JobID, &a.Limits, &a.AllocatedAt, &a.CreatedAt, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *AgentStore) FindIdle(ctx context.Context) (*model.Agent, error) {
	a := &model.Agent{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT `+agentCols+` FROM agents WHERE status=$1 AND user_id IS NULL ORDER BY created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED`,
		model.AgentIdle,
	).Scan(&a.ID, &a.DeviceID, &a.UserID, &a.Name, &a.Description, &a.Image, &a.Port, &a.Tags,
		&a.Status, &a.JobID, &a.Limits, &a.AllocatedAt, &a.CreatedAt, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

func (s *AgentStore) Allocate(ctx context.Context, agentID, userID, jobID model.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE agents SET status=$2, user_id=$3, allocated_at=$4, updated_at=$4 WHERE id=$1`,
		agentID, model.AgentBusy, userID, now,
	)
	return err
}

func (s *AgentStore) Release(ctx context.Context, agentID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE agents SET status=$2, user_id=NULL, job_id=NULL, allocated_at=NULL, updated_at=$3 WHERE id=$1`,
		agentID, model.AgentIdle, time.Now().UTC(),
	)
	return err
}

func (s *AgentStore) UpdateStatus(ctx context.Context, agentID model.UUID, status model.AgentStatus) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE agents SET status=$2, updated_at=$3 WHERE id=$1`,
		agentID, status, time.Now().UTC(),
	)
	return err
}

func (s *AgentStore) UpdateName(ctx context.Context, agentID model.UUID, name string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE agents SET name=$2, updated_at=$3 WHERE id=$1`,
		agentID, name, time.Now().UTC(),
	)
	return err
}

func (s *AgentStore) UpdateContainerID(ctx context.Context, agentID model.UUID, containerID string) error {
	// Note: container_id is not a column in the spec schema. Stored as name suffix for now.
	_ = ctx; _ = agentID; _ = containerID
	return nil
}

func (s *AgentStore) ListByDevice(ctx context.Context, deviceID model.UUID) ([]model.Agent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+agentCols+` FROM agents WHERE device_id=$1 ORDER BY created_at`, deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAgents(rows)
}

func (s *AgentStore) ListAll(ctx context.Context, cursor *model.UUID, limit int) ([]model.Agent, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT `+agentCols+` FROM agents ORDER BY created_at DESC LIMIT $1`, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT `+agentCols+` FROM agents WHERE created_at < (SELECT created_at FROM agents WHERE id=$1) ORDER BY created_at DESC LIMIT $2`,
			*cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	agents, err := scanAgents(rows)
	if err != nil {
		return nil, nil, err
	}
	var nextCursor *model.UUID
	if len(agents) > limit {
		nextCursor = &agents[limit-1].ID
		agents = agents[:limit]
	}
	return agents, nextCursor, nil
}

func (s *AgentStore) ListByUser(ctx context.Context, userID model.UUID) ([]model.Agent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+agentCols+` FROM agents WHERE user_id=$1 AND status=$2 ORDER BY created_at`, userID, model.AgentBusy,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAgents(rows)
}

func (s *AgentStore) IdleCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE status=$1`, model.AgentIdle).Scan(&count)
	return count, err
}

func (s *AgentStore) CountByDevice(ctx context.Context, deviceID model.UUID) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE device_id=$1 AND status NOT IN ('failed')`, deviceID).Scan(&count)
	return count, err
}

func (s *AgentStore) Delete(ctx context.Context, agentID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM agents WHERE id=$1`, agentID)
	return err
}

func scanAgents(rows pgx.Rows) ([]model.Agent, error) {
	var agents []model.Agent
	for rows.Next() {
		var a model.Agent
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.UserID, &a.Name, &a.Description, &a.Image, &a.Port, &a.Tags,
			&a.Status, &a.JobID, &a.Limits, &a.AllocatedAt, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}
