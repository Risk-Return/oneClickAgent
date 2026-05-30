// Package httpapi registers all REST + WS routes on a chi router,
// applies the middleware chain, and wires handlers to store/tunnel/pubsub/pool.
// Customer routes: auth, jobs, files, visible skills, active agents.
// Admin routes: devices, agent pool (inspect/drain/release), skill vault,
//   fleet rollout, visibility grants, organizations.
package httpapi
