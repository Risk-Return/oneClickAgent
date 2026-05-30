// Package pool manages the agent pool lifecycle and job queue.
//   - Allocate: select an IDLE agent, set user_id + job_id, mark BUSY
//   - Release: clear user_id + job_id, mark IDLE on job completion → wake-up dequeue
//   - Dequeue: on agent release, select next QUEUED job (ORDER BY user_tier ASC, created_at ASC)
//     skip expired jobs (QUEUED_TIMEOUT), allocate if idle agent available
//   - Enqueue: when no idle agent, set status=QUEUED, queued_at, queue_expires_at
//   - Scale: ensure N idle agents per device (create/drain/recycle)
//   - Admin: drain (finish + remove), force-release stuck BUSY agents
package pool
