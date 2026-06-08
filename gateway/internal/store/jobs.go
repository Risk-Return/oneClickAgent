package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

type JobStore struct{ db *DB }

func NewJobStore(db *DB) *JobStore { return &JobStore{db: db} }

var jobCols = `id, user_id, user_tier, agent_id, device_id, channel, command, params, skill_id,
 status, percent, progress_message, result, error_code, error_message,
 queued_at, queue_expires_at, submitted_at, started_at, finished_at, created_at, updated_at`

func (s *JobStore) Create(ctx context.Context, j *model.Job) error {
	j.ID = model.NewUUID()
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now
	j.SubmittedAt = now

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO jobs (id, user_id, user_tier, agent_id, device_id, channel, command, params, skill_id,
		 status, error_code, queued_at, queue_expires_at, submitted_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		j.ID, j.UserID, j.UserTier, j.AgentID, j.DeviceID, j.Channel, j.Command, j.Params, j.SkillID,
		j.Status, j.ErrorCode, j.QueuedAt, j.QueueExpiresAt, j.SubmittedAt, j.CreatedAt, j.UpdatedAt,
	)
	return err
}

func (s *JobStore) GetByID(ctx context.Context, id model.UUID) (*model.Job, error) {
	j := &model.Job{}
	err := s.db.Pool.QueryRow(ctx, `SELECT `+jobCols+` FROM jobs WHERE id=$1`, id,
	).Scan(&j.ID, &j.UserID, &j.UserTier, &j.AgentID, &j.DeviceID, &j.Channel, &j.Command, &j.Params, &j.SkillID,
		&j.Status, &j.Percent, &j.ProgressMessage, &j.Result, &j.ErrorCode, &j.ErrorMessage,
		&j.QueuedAt, &j.QueueExpiresAt, &j.SubmittedAt, &j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (s *JobStore) UpdateStatus(ctx context.Context, id model.UUID, status model.JobStatus) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET status=$2, finished_at=CASE WHEN $2 IN ('succeeded','failed','cancelled') THEN $3 ELSE finished_at END, updated_at=$3 WHERE id=$1`,
		id, status, now,
	)
	return err
}

func (s *JobStore) UpdateProgress(ctx context.Context, id model.UUID, percent int, message string, status model.JobStatus) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET percent=$2, progress_message=$3, status=CASE WHEN $4 NOT IN ('succeeded','failed','cancelled') THEN $4 ELSE status END, updated_at=$5 WHERE id=$1`,
		id, percent, message, status, now,
	)
	return err
}

func (s *JobStore) UpdateResult(ctx context.Context, id model.UUID, status model.JobStatus, result *json.RawMessage) error {
	now := time.Now().UTC()
	var resultVal interface{}
	if result != nil {
		resultVal = *result
	}
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET status=$2, result=$3, finished_at=$4, updated_at=$4 WHERE id=$1`,
		id, status, resultVal, now,
	)
	return err
}

func (s *JobStore) SetAgent(ctx context.Context, jobID, agentID, deviceID model.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET agent_id=$2, device_id=$3, status=$4, started_at=$5, updated_at=$5 WHERE id=$1`,
		jobID, agentID, deviceID, model.JobDispatched, now,
	)
	return err
}

func (s *JobStore) ClearAgent(ctx context.Context, jobID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET agent_id=NULL, device_id=NULL WHERE id=$1`, jobID,
	)
	return err
}

func (s *JobStore) Cancel(ctx context.Context, id, userID model.UUID) error {
	now := time.Now().UTC()
	result, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET status=$2, finished_at=$3, updated_at=$3 WHERE id=$1 AND user_id=$4 AND status NOT IN ('succeeded','failed','cancelled')`,
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

func (s *JobStore) DequeueNext(ctx context.Context) (*model.Job, error) {
	j := &model.Job{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT `+jobCols+` FROM jobs
		 WHERE status=$1 AND queue_expires_at > $2
		 ORDER BY user_tier ASC, created_at ASC
		 LIMIT 1 FOR UPDATE SKIP LOCKED`,
		model.JobQueued, time.Now().UTC(),
	).Scan(&j.ID, &j.UserID, &j.UserTier, &j.AgentID, &j.DeviceID, &j.Channel, &j.Command, &j.Params, &j.SkillID,
		&j.Status, &j.Percent, &j.ProgressMessage, &j.Result, &j.ErrorCode, &j.ErrorMessage,
		&j.QueuedAt, &j.QueueExpiresAt, &j.SubmittedAt, &j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (s *JobStore) ExpireQueued(ctx context.Context) (int64, error) {
	code := string(model.ErrCodeQueueTimeout)
	result, err := s.db.Pool.Exec(ctx,
		`UPDATE jobs SET status=$2, error_code=$3, error_message=$3, finished_at=$4, updated_at=$4
		 WHERE status=$1 AND queue_expires_at <= $4`,
		model.JobQueued, model.JobFailed, code, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *JobStore) CountQueuedByUser(ctx context.Context, userID model.UUID) (int, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs WHERE user_id=$1 AND status=$2`, userID, model.JobQueued,
	).Scan(&count)
	return count, err
}

func (s *JobStore) GetQueuePosition(ctx context.Context, jobID model.UUID) (int, error) {
	var pos int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM jobs WHERE status=$2 AND created_at <= (SELECT created_at FROM jobs WHERE id=$1)`,
		jobID, model.JobQueued,
	).Scan(&pos)
	return pos, err
}

func (s *JobStore) ListByUser(ctx context.Context, userID model.UUID, cursor *model.UUID, limit int) ([]model.Job, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT `+jobCols+` FROM jobs WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT `+jobCols+` FROM jobs WHERE user_id=$1 AND created_at < (SELECT created_at FROM jobs WHERE id=$2) ORDER BY created_at DESC LIMIT $3`,
			userID, *cursor, limit+1,
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

func (s *JobStore) ListByAgent(ctx context.Context, agentID model.UUID) ([]model.Job, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+jobCols+` FROM jobs WHERE agent_id=$1 ORDER BY created_at`, agentID,
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
		if err := rows.Scan(&j.ID, &j.UserID, &j.UserTier, &j.AgentID, &j.DeviceID, &j.Channel, &j.Command, &j.Params, &j.SkillID,
			&j.Status, &j.Percent, &j.ProgressMessage, &j.Result, &j.ErrorCode, &j.ErrorMessage,
			&j.QueuedAt, &j.QueueExpiresAt, &j.SubmittedAt, &j.StartedAt, &j.FinishedAt, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
