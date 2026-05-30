// Package pubsub provides an in-process topic broker for real-time event fan-out.
// Topics are keyed by job/agent/device IDs; subscribers are scoped to user_id
// for tenant isolation. Used by web WS to stream job.progress, agent.status, etc.
package pubsub
