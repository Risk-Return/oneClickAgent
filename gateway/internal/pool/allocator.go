// Package pool manages the agent pool lifecycle.
//   - Allocate: select an IDLE agent, set user_id + job_id, mark BUSY
//   - Release: clear user_id + job_id, mark IDLE on job completion
//   - Scale: ensure N idle agents per device (create/drain/recycle)
//   - Admin: drain (finish + remove), force-release stuck BUSY agents
package pool
