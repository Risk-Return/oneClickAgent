// Agent repository (pool + queue): find idle agents, allocate/release,
// dequeue next QUEUED job (ORDER BY user_tier ASC, created_at ASC),
// expire timed-out QUEUED jobs, list pool state, CRUD for admin pool management.
package store
