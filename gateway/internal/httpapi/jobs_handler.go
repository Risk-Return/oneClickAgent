// Job handlers: submit (POST /jobs — system auto-allocates agent from pool;
// returns 201 if allocated immediately, 202 with queue_position if queued,
// 429 QUEUE_FULL if user at cap), cancel (releases agent + triggers dequeue),
// list, detail (includes queue info when QUEUED), result.
// Validates one-skill-per-job constraint and tenant ownership.
package httpapi
