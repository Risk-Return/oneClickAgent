// Package pool manages the agent pool lifecycle and job queue.
package pool

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
	"github.com/oneClickAgent/gateway/internal/pubsub"
	"github.com/oneClickAgent/gateway/internal/store"
	"github.com/oneClickAgent/gateway/internal/tunnel"
)

// Allocator handles agent pool lifecycle and job queue management.
type Allocator struct {
	agents          store.AgentStoreInterface
	jobs            store.JobStoreInterface
	files           store.FileStoreInterface
	credIDs         func(ctx context.Context, jobID model.UUID) ([]model.UUID, error)
	pushCredentials func(ctx context.Context, jobID, agentID, deviceID model.UUID) error
	hub             *tunnel.Hub
	broker          *pubsub.Broker
	queueTTL        time.Duration
	dispatchTimeout time.Duration
	maxQueue        int
	logger          *slog.Logger
}

// NewAllocator creates a new agent pool allocator.
func NewAllocator(
	agents store.AgentStoreInterface,
	jobs store.JobStoreInterface,
	hub *tunnel.Hub,
	broker *pubsub.Broker,
	queueTTL time.Duration,
	maxQueuePerUser int,
) *Allocator {
	return &Allocator{
		agents:          agents,
		jobs:            jobs,
		hub:             hub,
		broker:          broker,
		queueTTL:        queueTTL,
		dispatchTimeout: 5 * time.Minute,
		maxQueue:        maxQueuePerUser,
		logger:          obs.Logger("pool"),
	}
}

// Allocate attempts to assign an idle agent to a job.
// Returns the allocated agent, or nil if none available (job will be queued).
func (a *Allocator) Allocate(ctx context.Context, job *model.Job) (*model.Agent, error) {
	// Check per-user queue cap
	if a.maxQueue > 0 {
		count, err := a.jobs.CountQueuedByUser(ctx, job.UserID)
		if err != nil {
			return nil, err
		}
		if count >= a.maxQueue {
			return nil, ErrQueueFull
		}
	}

	// Try to find an idle agent
	agent, err := a.agents.FindIdle(ctx)
	if err != nil {
		return nil, err
	}

	if agent == nil {
		// No idle agent - queue the job
		now := time.Now().UTC()
		expires := now.Add(a.queueTTL)
		job.QueuedAt = &now
		job.QueueExpiresAt = &expires
		job.Status = model.JobQueued

		// Compute queue position
		pos, err := a.jobs.GetQueuePosition(ctx, job.ID)
		if err == nil {
			job.QueuePosition = &pos
		}

		return nil, nil
	}

	// Allocate the agent
	if err := a.agents.Allocate(ctx, agent.ID, job.UserID, job.ID); err != nil {
		return nil, err
	}

	agent.Status = model.AgentBusy
	agent.UserID = &job.UserID
	agent.JobID = &job.ID

	job.AgentID = &agent.ID
	job.DeviceID = &agent.DeviceID
	job.Status = model.JobDispatched

	return agent, nil
}

// SetDispatchDeps wires the file store, credential lister, and credential push hook for dispatch payloads.
func (a *Allocator) SetDispatchDeps(
	files store.FileStoreInterface,
	credIDs func(ctx context.Context, jobID model.UUID) ([]model.UUID, error),
	pushCredentials func(ctx context.Context, jobID, agentID, deviceID model.UUID) error,
) {
	a.files = files
	a.credIDs = credIDs
	a.pushCredentials = pushCredentials
}

// Release returns an agent to the idle pool and triggers dequeue.
func (a *Allocator) Release(ctx context.Context, agentID model.UUID) error {
	if err := a.agents.Release(ctx, agentID); err != nil {
		return err
	}

	a.logger.Info("agent released to pool", "agent_id", agentID)

	// Wake-up: try to dequeue next job
	go a.dequeueNext(context.Background())

	return nil
}

// dequeueNext selects the next queued job and allocates an agent.
func (a *Allocator) dequeueNext(ctx context.Context) {
	// First expire any timed-out queued/dispatched jobs
	a.expireStale(ctx)

	for {
		// Find next queued job
		job, err := a.jobs.DequeueNext(ctx)
		if err != nil {
			a.logger.Error("dequeue error", "error", err)
			return
		}
		if job == nil {
			return // No more queued jobs
		}

		// Find idle agent
		agent, err := a.agents.FindIdle(ctx)
		if err != nil {
			a.logger.Error("find idle agent error", "error", err)
			return
		}
		if agent == nil {
			return // No idle agents, stop dequeuing
		}

		// Allocate
		if err := a.agents.Allocate(ctx, agent.ID, job.UserID, job.ID); err != nil {
			a.logger.Error("allocate error", "error", err)
			return
		}

		// Update job with agent info
		if err := a.jobs.SetAgent(ctx, job.ID, agent.ID, agent.DeviceID); err != nil {
			a.logger.Error("set agent error", "error", err)
			return
		}

		// Dispatch to device over tunnel
		if err := a.dispatchJob(ctx, job, agent); err != nil {
			a.logger.Error("dispatch error", "error", err, "job_id", job.ID)
			_ = a.agents.Release(ctx, agent.ID)
			// Requeue the job instead of failing immediately — the device
			// may reconnect shortly and the allocator will retry naturally.
			_ = a.jobs.ClearAgent(ctx, job.ID)
			_ = a.jobs.UpdateStatus(ctx, job.ID, model.JobQueued)
			continue
		}

		a.logger.Info("job dispatched from queue",
			"job_id", job.ID,
			"agent_id", agent.ID,
			"user_id", job.UserID,
		)
	}
}

// expireStale marks expired queued and dispatched jobs as FAILED.
func (a *Allocator) expireStale(ctx context.Context) {
	queued, err := a.jobs.ExpireQueued(ctx)
	if err != nil {
		a.logger.Error("expire queued jobs error", "error", err)
	} else if queued > 0 {
		a.logger.Info("expired queued jobs", "count", queued)
	}

	dispatched, err := a.jobs.ExpireDispatched(ctx, a.dispatchTimeout)
	if err != nil {
		a.logger.Error("expire dispatched jobs error", "error", err)
	} else if dispatched > 0 {
		a.logger.Info("expired dispatched jobs (no device response)", "count", dispatched)
	}
}

// dispatchJob sends a JOB_DISPATCH frame over the tunnel to the device.
func (a *Allocator) dispatchJob(ctx context.Context, job *model.Job, agent *model.Agent) error {
	var fileIDs []model.UUID
	if a.files != nil {
		files, _ := a.files.ListByJob(ctx, job.ID)
		for _, f := range files {
			fileIDs = append(fileIDs, f.ID)
		}
	}

	var credIDs []model.UUID
	if a.credIDs != nil {
		credIDs, _ = a.credIDs(ctx, job.ID)
	}

	payload := model.JobDispatchPayload{
		JobID:         job.ID,
		UserID:        job.UserID,
		AgentID:       agent.ID,
		Command:       job.Command,
		SkillID:       job.SkillID,
		FileIDs:       fileIDs,
		CredentialIDs: credIDs,
		SubmittedAt:   job.SubmittedAt.UnixMilli(),
	}

	if job.Params != nil {
		payload.Params = *job.Params
	}

	frame, err := tunnel.NewFrame(model.FrameJobDispatch, payload)
	if err != nil {
		return err
	}

	if err := a.hub.SendFrame(agent.DeviceID, frame); err != nil {
		return err
	}

	if a.pushCredentials != nil {
		_ = a.pushCredentials(ctx, job.ID, agent.ID, agent.DeviceID)
	}

	return nil
}

// StartExpiryTicker runs a background goroutine to expire queued jobs.
func (a *Allocator) StartExpiryTicker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.expireStale(ctx)
		}
	}
}

// EnsurePoolSize ensures a device has the desired number of agent containers.
func (a *Allocator) EnsurePoolSize(ctx context.Context, deviceID model.UUID, desiredSize int) error {
	currentCount, err := a.agents.CountByDevice(ctx, deviceID)
	if err != nil {
		return err
	}

	for i := currentCount; i < desiredSize; i++ {
		agentID := model.NewUUID()
		agentIDStr := agentID.String()

		// Create agent record first (so it survives offline device)
		agent := &model.Agent{
			ID:       agentID,
			DeviceID: deviceID,
			Name:     "agent-" + agentIDStr[:8],
			Port:     i + 42000,
			Image:    "iagent/agent:dev",
			Tags:     []string{"opencode", "camoufox"},
			Status:   model.AgentCreating,
			Limits:   &model.AgentLimits{CPU: 2, MemMB: 4096, DiskMB: 10240},
		}
		if err := a.agents.Create(ctx, agent); err != nil {
			a.logger.Error("failed to create agent record", "error", err)
			return err
		}

		// Send AGENT_CREATE to device (best-effort; ReconcilePool handles retry)
		payload := model.AgentCreatePayload{
			AgentID: agentID,
			Image:   "iagent/agent:dev",
			Tags:    []string{"opencode", "camoufox"},
			Limits: model.AgentLimits{
				CPU:    2,
				MemMB:  4096,
				DiskMB: 10240,
			},
		}
		frame, err := tunnel.NewFrame(model.FrameAgentCreate, payload)
		if err != nil {
			return err
		}
		if err := a.hub.SendFrame(deviceID, frame); err != nil {
			a.logger.Warn("AGENT_CREATE not delivered (device offline, will reconcile on reconnect)",
				"agent_id", agentID, "device_id", deviceID)
		}
	}

	return nil
}

// ReconcilePool syncs agent state with the device on reconnect.
// Agents reported by device that are stuck in "creating" are promoted to "idle".
// Stale "creating" agents (not reported and older than grace period) are deleted.
func (a *Allocator) ReconcilePool(ctx context.Context, deviceID model.UUID, helloAgents []model.HelloAgent) error {
	dbAgents, err := a.agents.ListByDevice(ctx, deviceID)
	if err != nil {
		return err
	}

	helloIDs := make(map[model.UUID]bool, len(helloAgents))
	for _, ha := range helloAgents {
		helloIDs[ha.AgentID] = true
	}

	dbByID := make(map[model.UUID]model.Agent, len(dbAgents))
	for _, da := range dbAgents {
		dbByID[da.ID] = da
	}

	// Promote creating agents that the device reports as present to idle
	for _, ha := range helloAgents {
		if da, ok := dbByID[ha.AgentID]; ok && da.Status == model.AgentCreating {
			if err := a.agents.UpdateStatus(ctx, ha.AgentID, model.AgentIdle); err != nil {
				a.logger.Error("failed to promote agent to idle", "agent_id", ha.AgentID, "error", err)
			}
		}
	}

	// Only delete creating agents that have been stuck for >2 minutes
	const gracePeriod = 2 * time.Minute
	cutoff := time.Now().UTC().Add(-gracePeriod)
	deleted := 0
	for _, da := range dbAgents {
		if !helloIDs[da.ID] && da.Status == model.AgentCreating && da.CreatedAt.Before(cutoff) {
			if err := a.agents.Delete(ctx, da.ID); err != nil {
				a.logger.Error("failed to delete stuck agent", "agent_id", da.ID, "error", err)
				continue
			}
			deleted++
		}
	}

	if deleted > 0 {
		a.logger.Info("pool reconciled: deleted stuck agents, re-creating",
			"device_id", deviceID, "deleted", deleted, "remaining", len(dbAgents)-deleted)
		return a.EnsurePoolSize(ctx, deviceID, len(dbAgents))
	}

	// Device came online — attempt to dequeue any waiting jobs.
	go a.dequeueNext(context.Background())

	return nil
}

// DrainAgent marks an agent for drain (stop container and remove).
// If all referencing jobs are in terminal state, nulls the FK and deletes.
func (a *Allocator) DrainAgent(ctx context.Context, agentID model.UUID) error {
	agent, err := a.agents.GetByID(ctx, agentID)
	if err != nil {
		return err
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	// Send AGENT_ACTION to device to stop the container
	actionFrame, err := tunnel.NewFrame(model.FrameAgentAction, model.AgentActionPayload{
		AgentID: agentID,
		Action:  "drain",
	})
	if err == nil {
		_ = a.hub.SendFrame(agent.DeviceID, actionFrame)
	}

	if agent.Status == model.AgentIdle || agent.Status == model.AgentFailed {
		if delErr := a.agents.Delete(ctx, agentID); delErr != nil {
			// FK constraint likely — check if all referencing jobs are terminal
			if err := a.nullTerminalJobsForAgent(ctx, agentID); err != nil {
				return err
			}
			return a.agents.Delete(ctx, agentID)
		}
		return nil
	}

	// If busy, mark for drain - it will be removed after job completion
	return a.agents.UpdateStatus(ctx, agentID, model.AgentFailed)
}

// nullTerminalJobsForAgent nulls the agent_id FK on all terminal jobs for this agent.
// Returns error if any job is non-terminal (still active).
func (a *Allocator) nullTerminalJobsForAgent(ctx context.Context, agentID model.UUID) error {
	jobs, err := a.jobs.ListByAgent(ctx, agentID)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if !j.Status.IsTerminal() {
			return fmt.Errorf("agent has active job %s (status=%s)", j.ID, j.Status)
		}
	}
	for _, j := range jobs {
		_ = a.jobs.ClearAgent(ctx, j.ID)
	}
	return nil
}

// ForceRelease releases a stuck BUSY agent back to IDLE.
func (a *Allocator) ForceRelease(ctx context.Context, agentID model.UUID) error {
	return a.Release(ctx, agentID)
}

// PoolStats returns basic pool statistics.
type PoolStats struct {
	TotalAgents  int `json:"total_agents"`
	IdleAgents   int `json:"idle_agents"`
	BusyAgents   int `json:"busy_agents"`
	OnlineDevices int `json:"online_devices"`
}

// Stats returns current pool statistics.
func (a *Allocator) Stats(ctx context.Context) (*PoolStats, error) {
	idle, err := a.agents.IdleCount(ctx)
	if err != nil {
		return nil, err
	}

	// For simplicity, we fetch all. In production this would use COUNT queries.
	online := a.hub.OnlineCount()

	return &PoolStats{
		IdleAgents:   idle,
		OnlineDevices: online,
	}, nil
}

// Errors
var (
	ErrQueueFull    = &poolError{"queue full: max queued jobs per user reached"}
	ErrAgentNotFound = &poolError{"agent not found"}
)

type poolError struct {
	msg string
}

func (e *poolError) Error() string {
	return e.msg
}
