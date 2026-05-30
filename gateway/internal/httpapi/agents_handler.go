// Agent handlers:
//   Customer: list agents currently allocated to their active jobs + detail.
//   Admin: view entire pool (all agents across fleet), drain, force-release.
// Customers never create agents — they are auto-allocated from the pool per job.
package httpapi
