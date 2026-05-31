package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// AgentStore handles agent pool persistence.
type AgentStore struct {
	db *DB
}

func NewAgentStore(db *DB) *AgentStore {
	return &AgentStore{db: db}
}

func (s *AgentStore) Create(ctx context.Context, agent *model.Agent) error {
	agent.ID = model.NewUUID()
	now := time.Now().UTC()
	agent.CreatedAt = now
	agent.UpdatedAt = now

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO agents (id, device_id, container_id, status, user_id, job_id, agent_name, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		agent.ID, agent.DeviceID, agent.ContainerID, agent.Status, agent.UserID, agent.JobID, agent.AgentName, agent.CreatedAt, agent.UpdatedAt,
	)
	return err
}

func (s *AgentStore) GetByID(ctx context.Context, id model.UUID) (*model.Agent, error) {
	a := &model.Agent{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, device_id, container_id, status, user_id, job_id, agent_name, created_at, updated_at
		 FROM agents WHERE id = $1`, id,
	).Scan(&a.ID, &a.DeviceID, &a.ContainerID, &a.Status, &a.UserID, &a.JobID, &a.AgentName, &a.CreatedAt, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// FindIdle finds an idle agent available for allocation, ordered by FIFO.
func (s *AgentStore) FindIdle(ctx context.Context) (*model.Agent, error) {
	a := &model.Agent{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, device_id, container_id, status, user_id, job_id, agent_name, created_at, updated_at
		 FROM agents
		 WHERE status = $1 AND user_id IS NULL
		 ORDER BY created_at ASC
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`, model.AgentIdle,
	).Scan(&a.ID, &a.DeviceID, &a.ContainerID, &a.Status, &a.UserID, &a.JobID, &a.AgentName, &a.CreatedAt, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// Allocate sets an agent as BUSY and assigns it to a user/job.
func (s *AgentStore) Allocate(ctx context.Context, agentID model.UUID, userID model.UUID, jobID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE agents SET status=$2, user_id=$3, job_id=$4, updated_at=$5
		 WHERE id=$1`,
		agentID, model.AgentBusy, userID, jobID, time.Now().UTC(),
	)
	return err
}

// Release returns an agent to the pool (IDLE, clear user/job).
func (s *AgentStore) Release(ctx context.Context, agentID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE agents SET status=$2, user_id=NULL, job_id=NULL, updated_at=$3
		 WHERE id=$1`,
		agentID, model.AgentIdle, time.Now().UTC(),
	)
	return err
}

// UpdateStatus updates an agent's pool status.
func (s *AgentStore) UpdateStatus(ctx context.Context, agentID model.UUID, status model.AgentStatus) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE agents SET status=$2, updated_at=$3 WHERE id=$1`,
		agentID, status, time.Now().UTC(),
	)
	return err
}

// UpdateContainerID sets the container ID for an agent.
func (s *AgentStore) UpdateContainerID(ctx context.Context, agentID model.UUID, containerID string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE agents SET container_id=$2, updated_at=$3 WHERE id=$1`,
		agentID, containerID, time.Now().UTC(),
	)
	return err
}

// ListByDevice returns all agents on a device.
func (s *AgentStore) ListByDevice(ctx context.Context, deviceID model.UUID) ([]model.Agent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, device_id, container_id, status, user_id, job_id, agent_name, created_at, updated_at
		 FROM agents WHERE device_id=$1 ORDER BY created_at`, deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAgents(rows)
}

// ListAll returns all agents across all devices (admin view).
func (s *AgentStore) ListAll(ctx context.Context, cursor *model.UUID, limit int) ([]model.Agent, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, device_id, container_id, status, user_id, job_id, agent_name, created_at, updated_at
			 FROM agents ORDER BY created_at DESC LIMIT $1`, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, device_id, container_id, status, user_id, job_id, agent_name, created_at, updated_at
			 FROM agents WHERE created_at < (SELECT created_at FROM agents WHERE id=$1)
			 ORDER BY created_at DESC LIMIT $2`, *cursor, limit+1,
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

// ListByUser returns agents currently allocated to a user's active jobs.
func (s *AgentStore) ListByUser(ctx context.Context, userID model.UUID) ([]model.Agent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, device_id, container_id, status, user_id, job_id, agent_name, created_at, updated_at
		 FROM agents WHERE user_id=$1 AND status=$2 ORDER BY created_at`, userID, model.AgentBusy,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAgents(rows)
}

// IdleCount returns the number of idle agents in the pool.
func (s *AgentStore) IdleCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM agents WHERE status=$1`, model.AgentIdle,
	).Scan(&count)
	return count, err
}

// CountByDevice returns the number of agents on a device.
func (s *AgentStore) CountByDevice(ctx context.Context, deviceID model.UUID) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM agents WHERE device_id=$1`, deviceID,
	).Scan(&count)
	return count, err
}

// Delete removes an agent from the pool.
func (s *AgentStore) Delete(ctx context.Context, agentID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM agents WHERE id=$1`, agentID)
	return err
}

func scanAgents(rows pgx.Rows) ([]model.Agent, error) {
	var agents []model.Agent
	for rows.Next() {
		var a model.Agent
		if err := rows.Scan(&a.ID, &a.DeviceID, &a.ContainerID, &a.Status, &a.UserID, &a.JobID, &a.AgentName, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}
