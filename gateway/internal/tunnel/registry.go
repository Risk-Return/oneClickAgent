package tunnel

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
)

// ─── InMemoryRegistry ───────────────────────────────────────

// InMemoryRegistry keeps device→node mappings in process memory.
// Used for single-instance deployments.
type InMemoryRegistry struct {
	mu       sync.RWMutex
	devices  map[model.UUID]*deviceEntry
}

type deviceEntry struct {
	NodeID   string
	LastSeen int64
}

// NewInMemoryRegistry creates a new in-memory device registry.
func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{
		devices: make(map[model.UUID]*deviceEntry),
	}
}

func (r *InMemoryRegistry) Register(ctx context.Context, deviceID model.UUID, nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[deviceID] = &deviceEntry{NodeID: nodeID}
	return nil
}

func (r *InMemoryRegistry) Unregister(ctx context.Context, deviceID model.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.devices, deviceID)
	return nil
}

func (r *InMemoryRegistry) GetNode(ctx context.Context, deviceID model.UUID) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.devices[deviceID]; ok {
		return e.NodeID, nil
	}
	return "", nil
}

func (r *InMemoryRegistry) IsOnline(ctx context.Context, deviceID model.UUID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.devices[deviceID]
	return ok
}

func (r *InMemoryRegistry) Count(ctx context.Context) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.devices)
}

func (r *InMemoryRegistry) List(ctx context.Context) []model.UUID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]model.UUID, 0, len(r.devices))
	for id := range r.devices {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})
	return ids
}

func (r *InMemoryRegistry) Touch(ctx context.Context, deviceID model.UUID, ts int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.devices[deviceID]; ok {
		e.LastSeen = ts
	}
}

func (r *InMemoryRegistry) GetStale(ctx context.Context, threshold int64) []model.UUID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var stale []model.UUID
	for id, e := range r.devices {
		if e.LastSeen > 0 && e.LastSeen < threshold {
			stale = append(stale, id)
		}
	}
	return stale
}

func (r *InMemoryRegistry) Close() error { return nil }

// ─── SimulatedRedisRegistry ─────────────────────────────────

// RedisRegistry uses a simulated Redis backend (in-process map) that
// mirrors a real Redis deployment with SET/GET/DEL/ZADD/ZRANGEBYSCORE.
// In production, this would use go-redis to talk to a real Redis instance.
//
// Simulated Redis commands used:
//
//	SET    iagent:device:{id}:node    <nodeID>       → register
//	GET    iagent:device:{id}:node                    → lookup node
//	DEL    iagent:device:{id}:node                    → unregister
//	ZADD   iagent:devices:heartbeat   <ts> <deviceID> → heartbeat
//	ZRANGEBYSCORE iagent:devices:heartbeat -inf <threshold> → stale
//	ZCARD  iagent:devices:heartbeat                   → count
//	ZRANGE iagent:devices:heartbeat 0 -1              → list
type RedisRegistry struct {
	mu       sync.RWMutex
	data     map[string]string         // key → value (SET/GET/DEL)
	zsets    map[string]map[string]float64 // zset key → (member → score) (ZADD/ZRANGEBYSCORE)
}

// NewRedisRegistry creates a simulated Redis-backed registry.
// In production, pass a go-redis client instead of using the simulation.
func NewRedisRegistry() *RedisRegistry {
	return &RedisRegistry{
		data:  make(map[string]string),
		zsets: make(map[string]map[string]float64),
	}
}

func (r *RedisRegistry) nodeKey(deviceID model.UUID) string {
	return "iagent:device:" + deviceID.String() + ":node"
}

const heartbeatZSet = "iagent:devices:heartbeat"

func (r *RedisRegistry) Register(ctx context.Context, deviceID model.UUID, nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// SET iagent:device:{id}:node <nodeID>
	r.data[r.nodeKey(deviceID)] = nodeID

	// ZADD iagent:devices:heartbeat <now> <deviceID>
	if r.zsets[heartbeatZSet] == nil {
		r.zsets[heartbeatZSet] = make(map[string]float64)
	}
	r.zsets[heartbeatZSet][deviceID.String()] = float64(timeNow())

	return nil
}

func (r *RedisRegistry) Unregister(ctx context.Context, deviceID model.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// DEL iagent:device:{id}:node
	delete(r.data, r.nodeKey(deviceID))

	// ZREM iagent:devices:heartbeat <deviceID>
	delete(r.zsets[heartbeatZSet], deviceID.String())

	return nil
}

func (r *RedisRegistry) GetNode(ctx context.Context, deviceID model.UUID) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data[r.nodeKey(deviceID)], nil
}

func (r *RedisRegistry) IsOnline(ctx context.Context, deviceID model.UUID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.data[r.nodeKey(deviceID)]
	return ok
}

func (r *RedisRegistry) Count(ctx context.Context) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.zsets[heartbeatZSet])
}

func (r *RedisRegistry) List(ctx context.Context) []model.UUID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := r.zsets[heartbeatZSet]
	ids := make([]model.UUID, 0, len(m))
	for s := range m {
		if id, err := model.ParseUUID(s); err == nil {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i].String() < ids[j].String()
	})
	return ids
}

func (r *RedisRegistry) Touch(ctx context.Context, deviceID model.UUID, ts int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.zsets[heartbeatZSet] == nil {
		r.zsets[heartbeatZSet] = make(map[string]float64)
	}
	r.zsets[heartbeatZSet][deviceID.String()] = float64(ts)
}

func (r *RedisRegistry) GetStale(ctx context.Context, threshold int64) []model.UUID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	m := r.zsets[heartbeatZSet]
	var stale []model.UUID
	for s, score := range m {
		if score > 0 && int64(score) < threshold {
			if id, err := model.ParseUUID(s); err == nil {
				stale = append(stale, id)
			}
		}
	}
	return stale
}

func (r *RedisRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = nil
	r.zsets = nil
	return nil
}

// timeNow is overridden in tests to control time.
var timeNow = func() int64 { return time.Now().Unix() }
