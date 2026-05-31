package tunnel

import (
	"context"
	"testing"

	"github.com/oneClickAgent/gateway/internal/model"
)

// registryTestSuite runs a standard set of tests against any Registry implementation.
func registryTestSuite(t *testing.T, reg Registry, name string) {
	t.Helper()
	ctx := context.Background()

	t.Run(name+"/RegisterAndGetNode", func(t *testing.T) {
		deviceID := model.NewUUID()
		err := reg.Register(ctx, deviceID, "node-1")
		if err != nil {
			t.Fatalf("register: %v", err)
		}

		node, err := reg.GetNode(ctx, deviceID)
		if err != nil {
			t.Fatalf("get node: %v", err)
		}
		if node != "node-1" {
			t.Errorf("node = %q, want node-1", node)
		}
	})

	t.Run(name+"/IsOnline", func(t *testing.T) {
		deviceID := model.NewUUID()
		if reg.IsOnline(ctx, deviceID) {
			t.Error("should not be online before register")
		}
		reg.Register(ctx, deviceID, "node-1")
		if !reg.IsOnline(ctx, deviceID) {
			t.Error("should be online after register")
		}
	})

	t.Run(name+"/Unregister", func(t *testing.T) {
		deviceID := model.NewUUID()
		reg.Register(ctx, deviceID, "node-1")
		reg.Unregister(ctx, deviceID)

		node, _ := reg.GetNode(ctx, deviceID)
		if node != "" {
			t.Errorf("node should be empty after unregister, got %q", node)
		}
		if reg.IsOnline(ctx, deviceID) {
			t.Error("should be offline after unregister")
		}
	})

	t.Run(name+"/Count", func(t *testing.T) {
		// Clear state from previous sub-tests
		current := reg.Count(ctx)
		t.Logf("current count: %d", current)

		a := model.NewUUID()
		b := model.NewUUID()
		reg.Register(ctx, a, "node-1")
		reg.Register(ctx, b, "node-2")

		if reg.Count(ctx) != current+2 {
			t.Errorf("count = %d, want %d", reg.Count(ctx), current+2)
		}
	})

	t.Run(name+"/List", func(t *testing.T) {
		a := model.NewUUID()
		reg.Register(ctx, a, "node-1")

		list := reg.List(ctx)
		found := false
		for _, id := range list {
			if id == a {
				found = true
				break
			}
		}
		if !found {
			t.Error("registered device not found in List()")
		}
	})

	t.Run(name+"/TouchAndGetStale", func(t *testing.T) {
		deviceID := model.NewUUID()
		reg.Register(ctx, deviceID, "node-1")

		now := timeNow()
		reg.Touch(ctx, deviceID, now)

		// Should NOT be stale when threshold is before the touch time
		stale := reg.GetStale(ctx, now-1)
		for _, id := range stale {
			if id == deviceID {
				t.Error("just-touched device should not be stale with past threshold")
			}
		}

		// Should BE stale when threshold is after the touch time
		stale2 := reg.GetStale(ctx, now+1000)
		foundFresh := false
		for _, id := range stale2 {
			if id == deviceID {
				foundFresh = true
				break
			}
		}
		if !foundFresh {
			t.Error("device should be stale with future threshold")
		}

		// Old device should be stale
		oldID := model.NewUUID()
		reg.Register(ctx, oldID, "node-1")
		reg.Touch(ctx, oldID, 100)

		stale3 := reg.GetStale(ctx, now+1000)
		foundOld := false
		for _, id := range stale3 {
			if id == oldID {
				foundOld = true
				break
			}
		}
		if !foundOld {
			t.Error("old device should be stale")
		}
	})

	t.Run(name+"/Close", func(t *testing.T) {
		if err := reg.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
}

func TestInMemoryRegistry(t *testing.T) {
	reg := NewInMemoryRegistry()
	registryTestSuite(t, reg, "InMemory")
}

func TestRedisRegistry(t *testing.T) {
	reg := NewRedisRegistry()
	registryTestSuite(t, reg, "Redis")
}

func TestRegistrySupersede(t *testing.T) {
	t.Run("InMemory/Supersede", func(t *testing.T) {
		reg := NewInMemoryRegistry()
		ctx := context.Background()
		deviceID := model.NewUUID()

		reg.Register(ctx, deviceID, "node-1")
		reg.Register(ctx, deviceID, "node-2") // supersede

		node, _ := reg.GetNode(ctx, deviceID)
		if node != "node-2" {
			t.Errorf("should be superseded by node-2, got %q", node)
		}
	})

	t.Run("Redis/Supersede", func(t *testing.T) {
		reg := NewRedisRegistry()
		ctx := context.Background()
		deviceID := model.NewUUID()

		reg.Register(ctx, deviceID, "node-1")
		reg.Register(ctx, deviceID, "node-2")

		node, _ := reg.GetNode(ctx, deviceID)
		if node != "node-2" {
			t.Errorf("should be superseded by node-2, got %q", node)
		}
	})
}

func TestRegistryMultiNode(t *testing.T) {
	t.Run("InMemory/MultiNode", func(t *testing.T) {
		reg := NewInMemoryRegistry()
		ctx := context.Background()

		d1 := model.NewUUID()
		d2 := model.NewUUID()
		d3 := model.NewUUID()

		reg.Register(ctx, d1, "node-a")
		reg.Register(ctx, d2, "node-b")
		reg.Register(ctx, d3, "node-a")

		n1, _ := reg.GetNode(ctx, d1)
		n2, _ := reg.GetNode(ctx, d2)
		n3, _ := reg.GetNode(ctx, d3)

		if n1 != "node-a" || n2 != "node-b" || n3 != "node-a" {
			t.Errorf("node mapping wrong: %s/%s/%s", n1, n2, n3)
		}

		if reg.Count(ctx) != 3 {
			t.Errorf("count = %d, want 3", reg.Count(ctx))
		}
	})

	t.Run("Redis/MultiNode", func(t *testing.T) {
		reg := NewRedisRegistry()
		ctx := context.Background()

		d1 := model.NewUUID()
		d2 := model.NewUUID()

		reg.Register(ctx, d1, "node-a")
		reg.Register(ctx, d2, "node-b")

		if !reg.IsOnline(ctx, d1) || !reg.IsOnline(ctx, d2) {
			t.Error("both devices should be online")
		}

		reg.Unregister(ctx, d1)
		if reg.IsOnline(ctx, d1) {
			t.Error("d1 should be offline after unregister")
		}
		if !reg.IsOnline(ctx, d2) {
			t.Error("d2 should still be online")
		}
	})
}
