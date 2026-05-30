// Job handlers: submit (POST /jobs — system auto-allocates agent from pool),
// cancel (releases agent on terminal), list, detail, result.
// Validates one-skill-per-job constraint and tenant ownership.
package httpapi
