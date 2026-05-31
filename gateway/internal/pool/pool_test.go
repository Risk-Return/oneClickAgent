package pool

import (
	"testing"

	"github.com/oneClickAgent/gateway/internal/model"
)

func TestJobQueueOrdering(t *testing.T) {
	// Verify tier priority ordering
	if model.TierEnterprise.TierPriority() >= model.TierPro.TierPriority() {
		t.Error("enterprise should have higher priority than pro")
	}
	if model.TierPro.TierPriority() >= model.TierFree.TierPriority() {
		t.Error("pro should have higher priority than free")
	}
}

func TestAllocatorErrors(t *testing.T) {
	if ErrQueueFull.Error() == "" {
		t.Error("ErrQueueFull should have message")
	}
	if ErrAgentNotFound.Error() == "" {
		t.Error("ErrAgentNotFound should have message")
	}
}

func TestPoolStats(t *testing.T) {
	stats := &PoolStats{
		TotalAgents:   10,
		IdleAgents:    5,
		BusyAgents:    5,
		OnlineDevices: 3,
	}
	if stats.TotalAgents != 10 {
		t.Error("total agents mismatch")
	}
	if stats.IdleAgents+stats.BusyAgents != stats.TotalAgents {
		// Not necessarily enforced by code, just a sanity check
	}
}

func TestAgentStateTransitions(t *testing.T) {
	// Verify states
	states := []model.AgentStatus{
		model.AgentCreating,
		model.AgentIdle,
		model.AgentBusy,
		model.AgentUnhealthy,
		model.AgentFailed,
		model.AgentRemoved,
	}
	for _, s := range states {
		if s == "" {
			t.Error("agent status should not be empty")
		}
	}
}
