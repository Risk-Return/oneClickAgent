package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// JobStore handles job persistence.
type JobStore struct {
	db *DB
}

func NewJobStore(db *DB) *JobStore {
	return &JobStore{db: db}
}

func (s *JobStore) Create(ctx context.Context, job *model.Job) error {
	job.ID = model.NewUUID()
	job.CreatedAt = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO jobs (id, user_id, agent_id, device_id, command, skill_id, status, error_code,
		 queued_at, queue_expires_at, result, progress_percent, progress_message, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		job.ID, job.UserID, job.AgentID, job.DeviceID, job.Command, job.SkillID, job.Status,
		job.ErrorCode, job.QueuedAt, job.QueueExpiresAt, job.Result, job.ProgressPercent, job.ProgressMessage, job.CreatedAt,
	)
	return err
}

func (s *JobStore) GetByID(ctx context.Context, id model.UUID) (*model.Job, error) {
	j := &model.Job{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, user_id, agent_id, device_id, command, skill_id, status, error_code,
		 queued_at, queue_expires_at, result, progress_percent, progress_message,
		 created_at, started_at, completed_at
		 FROM jobs WHERE id = $1`, id,
	).Scan(&j.ID, &j.UserID, &j.AgentID, &j.DeviceID, &j.Command, &j.SkillID, &j.Status,
		&j.ErrorCode, &j.QueuedAt, &j.QueueExpiresAt, &j.Result, &j.ProgressPercent, &j.ProgressMessage,
		&j.CreatedAt, &j.StartedAt, &j.CompletedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (s *JobStore) UpdateStatus(ctx context.Context, id model.UUID, status model.JobStatus) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET status=$2, completed_at=CASE WHEN $2 IN ('SUCCEEDED','FAILED','CANCELLED') THEN $3 ELSE completed_at END
		 WHERE id=$1`, id, status, now,
	)
	return err
}

func (s *JobStore) UpdateProgress(ctx context.Context, id model.UUID, percent int, message string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET progress_percent=$2, progress_message=$3, status=CASE WHEN $4='RUNNING' THEN $4 ELSE status END WHERE id=$1`,
		id, percent, message, model.JobRunning,
	)
	return err
}

func (s *JobStore) UpdateResult(ctx context.Context, id model.UUID, status model.JobStatus, result *string) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET status=$2, result=$3, completed_at=$4 WHERE id=$1`,
		id, status, result, now,
	)
	return err
}

func (s *JobStore) SetAgent(ctx context.Context, jobID, agentID, deviceID model.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET agent_id=$2, device_id=$3, status=$4, started_at=CASE WHEN $4='DISPATCHED' THEN $5 ELSE started_at END WHERE id=$1`,
		jobID, agentID, deviceID, model.JobDispatched, now,
	)
	return err
}

func (s *JobStore) Cancel(ctx context.Context, id model.UUID, userID model.UUID) error {
	now := time.Now().UTC()
	result, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET status=$2, completed_at=$3 WHERE id=$1 AND user_id=$4 AND status NOT IN ('SUCCEEDED','FAILED','CANCELLED')`,
		id, model.JobCancelled, now, userID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// DequeueNext selects the next QUEUED job ordered by tier priority then FIFO.
func (s *JobStore) DequeueNext(ctx context.Context) (*model.Job, error) {
	j := &model.Job{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT j.id, j.user_id, j.agent_id, j.device_id, j.command, j.skill_id, j.status, j.error_code,
		 j.queued_at, j.queue_expires_at, j.result, j.progress_percent, j.progress_message,
		 j.created_at, j.started_at, j.completed_at
		 FROM jobs j
		 JOIN users u ON j.user_id = u.id
		 WHERE j.status = $1 AND j.queue_expires_at > $2
		 ORDER BY
		   CASE u.tier WHEN 'enterprise' THEN 0 WHEN 'pro' THEN 1 ELSE 2 END ASC,
		   j.created_at ASC
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`, model.JobQueued, time.Now().UTC(),
	).Scan(&j.ID, &j.UserID, &j.AgentID, &j.DeviceID, &j.Command, &j.SkillID, &j.Status,
		&j.ErrorCode, &j.QueuedAt, &j.QueueExpiresAt, &j.Result, &j.ProgressPercent, &j.ProgressMessage,
		&j.CreatedAt, &j.StartedAt, &j.CompletedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return j, err
}

// ExpireQueued marks expired QUEUED jobs as FAILED with QUEUE_TIMEOUT.
func (s *JobStore) ExpireQueued(ctx context.Context) (int64, error) {
	code := string(model.ErrCodeQueueTimeout)
	result, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET status=$2, error_code=$3, completed_at=$4
		 WHERE status=$1 AND queue_expires_at <= $4`,
		model.JobQueued, model.JobFailed, code, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// CountQueuedByUser returns the number of QUEUED jobs for a user.
func (s *JobStore) CountQueuedByUser(ctx context.Context, userID model.UUID) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND status=$2`, userID, model.JobQueued,
	).Scan(&count)
	return count, err
}

// GetQueuePosition returns the queue position of a job.
func (s *JobStore) GetQueuePosition(ctx context.Context, jobID model.UUID) (int, error) {
	var position int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs j
		 JOIN users u ON j.user_id = u.id
		 WHERE j.status = $2 AND j.created_at <= (SELECT created_at FROM jobs WHERE id=$1)`,
		jobID, model.JobQueued,
	).Scan(&position)
	return position, err
}

func (s *JobStore) ListByUser(ctx context.Context, userID model.UUID, cursor *model.UUID, limit int) ([]model.Job, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, user_id, agent_id, device_id, command, skill_id, status, error_code,
			 queued_at, queue_expires_at, result, progress_percent, progress_message,
			 created_at, started_at, completed_at
			 FROM jobs WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, user_id, agent_id, device_id, command, skill_id, status, error_code,
			 queued_at, queue_expires_at, result, progress_percent, progress_message,
			 created_at, started_at, completed_at
			 FROM jobs WHERE user_id=$1 AND created_at < (SELECT created_at FROM jobs WHERE id=$2)
			 ORDER BY created_at DESC LIMIT $3`, userID, *cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	jobs, err := scanJobs(rows)
	if err != nil {
		return nil, nil, err
	}

	var nextCursor *model.UUID
	if len(jobs) > limit {
		nextCursor = &jobs[limit-1].ID
		jobs = jobs[:limit]
	}

	return jobs, nextCursor, nil
}

// ListByAgent returns jobs for a specific agent.
func (s *JobStore) ListByAgent(ctx context.Context, agentID model.UUID, statuses []model.JobStatus) ([]model.Job, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, agent_id, device_id, command, skill_id, status, error_code,
		 queued_at, queue_expires_at, result, progress_percent, progress_message,
		 created_at, started_at, completed_at
		 FROM jobs WHERE agent_id=$1 AND status IN ($2,$3,$4,$5)
		 ORDER BY created_at DESC`, agentID, model.JobDispatched, model.JobRunning, model.JobPending, model.JobQueued,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanJobs(rows)
}

func scanJobs(rows pgx.Rows) ([]model.Job, error) {
	var jobs []model.Job
	for rows.Next() {
		var j model.Job
		if err := rows.Scan(&j.ID, &j.UserID, &j.AgentID, &j.DeviceID, &j.Command, &j.SkillID, &j.Status,
			&j.ErrorCode, &j.QueuedAt, &j.QueueExpiresAt, &j.Result, &j.ProgressPercent, &j.ProgressMessage,
			&j.CreatedAt, &j.StartedAt, &j.CompletedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
