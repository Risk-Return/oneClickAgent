package store

import (
	"context"
	"sync"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
)

// ─── MockUserStore ─────────────────────────────────────────

type MockUserStore struct {
	mu    sync.RWMutex
	users map[model.UUID]*model.User
}

func NewMockUserStore() *MockUserStore {
	return &MockUserStore{users: make(map[model.UUID]*model.User)}
}

func (m *MockUserStore) Create(ctx context.Context, u *model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.ID = model.NewUUID()
	u.CreatedAt = time.Now().UTC()
	u.UpdatedAt = u.CreatedAt
	if u.Status == "" {
		u.Status = model.UserActive
	}
	cp := *u
	m.users[u.ID] = &cp
	return nil
}

func (m *MockUserStore) GetByID(ctx context.Context, id model.UUID) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (m *MockUserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *MockUserStore) Update(ctx context.Context, u *model.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.UpdatedAt = time.Now().UTC()
	cp := *u
	m.users[u.ID] = &cp
	return nil
}

func (m *MockUserStore) UpdateTier(ctx context.Context, userID model.UUID, tier model.UserTier) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[userID]; ok {
		u.Tier = tier
		u.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MockUserStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.User, *model.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var users []model.User
	for _, u := range m.users {
		users = append(users, *u)
	}
	return users, nil, nil
}

// ─── MockJobStore ──────────────────────────────────────────

type MockJobStore struct {
	mu   sync.RWMutex
	jobs map[model.UUID]*model.Job
}

func NewMockJobStore() *MockJobStore {
	return &MockJobStore{jobs: make(map[model.UUID]*model.Job)}
}

func (m *MockJobStore) Create(ctx context.Context, j *model.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j.ID = model.NewUUID()
	j.CreatedAt = time.Now().UTC()
	j.UpdatedAt = j.CreatedAt
	j.SubmittedAt = j.CreatedAt
	cp := *j
	m.jobs[j.ID] = &cp
	return nil
}

func (m *MockJobStore) GetByID(ctx context.Context, id model.UUID) (*model.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, nil
	}
	cp := *j
	return &cp, nil
}

func (m *MockJobStore) UpdateStatus(ctx context.Context, id model.UUID, status model.JobStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		j.Status = status
		j.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MockJobStore) UpdateProgress(ctx context.Context, id model.UUID, percent int, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		p := percent
		j.Percent = &p
		j.ProgressMessage = &message
		j.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MockJobStore) UpdateResult(ctx context.Context, id model.UUID, status model.JobStatus, result *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		j.Status = status
		j.Result = result
		now := time.Now().UTC()
		j.FinishedAt = &now
		j.UpdatedAt = now
	}
	return nil
}

func (m *MockJobStore) SetAgent(ctx context.Context, jobID, agentID, deviceID model.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j.AgentID = &agentID
		j.DeviceID = &deviceID
		j.Status = model.JobDispatched
		j.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MockJobStore) Cancel(ctx context.Context, id, userID model.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		if j.UserID != userID {
			return nil
		}
		if j.Status.IsTerminal() {
			return nil
		}
		j.Status = model.JobCancelled
		now := time.Now().UTC()
		j.FinishedAt = &now
		j.UpdatedAt = now
	}
	return nil
}

func (m *MockJobStore) DequeueNext(ctx context.Context) (*model.Job, error) {
	return nil, nil
}

func (m *MockJobStore) ExpireQueued(ctx context.Context) (int64, error) {
	return 0, nil
}

func (m *MockJobStore) CountQueuedByUser(ctx context.Context, userID model.UUID) (int, error) {
	return 0, nil
}

func (m *MockJobStore) GetQueuePosition(ctx context.Context, jobID model.UUID) (int, error) {
	return 1, nil
}

func (m *MockJobStore) ListByUser(ctx context.Context, userID model.UUID, cursor *model.UUID, limit int) ([]model.Job, *model.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var jobs []model.Job
	for _, j := range m.jobs {
		if j.UserID == userID {
			jobs = append(jobs, *j)
		}
	}
	return jobs, nil, nil
}

// ─── MockAgentStore ────────────────────────────────────────

type MockAgentStore struct {
	mu     sync.RWMutex
	agents map[model.UUID]*model.Agent
}

func NewMockAgentStore() *MockAgentStore {
	return &MockAgentStore{agents: make(map[model.UUID]*model.Agent)}
}

func (m *MockAgentStore) Create(ctx context.Context, a *model.Agent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.ID = model.NewUUID()
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	if a.Limits == nil {
		a.Limits = &model.AgentLimits{CPU: 2, MemMB: 4096, DiskMB: 10240}
	}
	cp := *a
	m.agents[a.ID] = &cp
	return nil
}

func (m *MockAgentStore) GetByID(ctx context.Context, id model.UUID) (*model.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *MockAgentStore) FindIdle(ctx context.Context) (*model.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.agents {
		if a.Status == model.AgentIdle && a.UserID == nil {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *MockAgentStore) Allocate(ctx context.Context, agentID, userID, jobID model.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[agentID]; ok {
		a.Status = model.AgentBusy
		a.UserID = &userID
		a.JobID = &jobID
		now := time.Now().UTC()
		a.AllocatedAt = &now
		a.UpdatedAt = now
	}
	return nil
}

func (m *MockAgentStore) Release(ctx context.Context, agentID model.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[agentID]; ok {
		a.Status = model.AgentIdle
		a.UserID = nil
		a.JobID = nil
		a.AllocatedAt = nil
		a.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MockAgentStore) UpdateStatus(ctx context.Context, agentID model.UUID, status model.AgentStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[agentID]; ok {
		a.Status = status
		a.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MockAgentStore) ListByDevice(ctx context.Context, deviceID model.UUID) ([]model.Agent, error) {
	return nil, nil
}

func (m *MockAgentStore) ListAll(ctx context.Context, cursor *model.UUID, limit int) ([]model.Agent, *model.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var agents []model.Agent
	for _, a := range m.agents {
		agents = append(agents, *a)
	}
	return agents, nil, nil
}

func (m *MockAgentStore) ListByUser(ctx context.Context, userID model.UUID) ([]model.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var agents []model.Agent
	for _, a := range m.agents {
		if a.UserID != nil && *a.UserID == userID {
			agents = append(agents, *a)
		}
	}
	return agents, nil
}

func (m *MockAgentStore) IdleCount(ctx context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := 0
	for _, a := range m.agents {
		if a.Status == model.AgentIdle {
			c++
		}
	}
	return c, nil
}

func (m *MockAgentStore) CountByDevice(ctx context.Context, deviceID model.UUID) (int, error) {
	return 0, nil
}

func (m *MockAgentStore) Delete(ctx context.Context, agentID model.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agents, agentID)
	return nil
}

// ─── MockFileStore ─────────────────────────────────────────

type MockFileStore struct {
	mu    sync.RWMutex
	files map[model.UUID]*model.File
}

func NewMockFileStore() *MockFileStore {
	return &MockFileStore{files: make(map[model.UUID]*model.File)}
}

func (m *MockFileStore) Create(ctx context.Context, f *model.File) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f.ID = model.NewUUID()
	f.CreatedAt = time.Now().UTC()
	cp := *f
	m.files[f.ID] = &cp
	return nil
}

func (m *MockFileStore) GetByID(ctx context.Context, id model.UUID) (*model.File, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.files[id]
	if !ok {
		return nil, nil
	}
	cp := *f
	return &cp, nil
}

func (m *MockFileStore) UpdateStatus(ctx context.Context, id model.UUID, status model.FileStatus) error { return nil }
func (m *MockFileStore) MarkPurged(ctx context.Context, id model.UUID) error                       { return nil }
func (m *MockFileStore) UpdateSHA256(ctx context.Context, id model.UUID, sha256 string) error      { return nil }
func (m *MockFileStore) ListByUser(ctx context.Context, userID model.UUID, cursor *model.UUID, limit int) ([]model.File, *model.UUID, error) {
	return nil, nil, nil
}
func (m *MockFileStore) ListStagedCloud(ctx context.Context, olderThan time.Time) ([]model.File, error) {
	return nil, nil
}
func (m *MockFileStore) LinkToJob(ctx context.Context, fileID, jobID model.UUID) error { return nil }
func (m *MockFileStore) ListByJob(ctx context.Context, jobID model.UUID) ([]model.File, error) {
	return nil, nil
}

// ─── MockSkillStore ────────────────────────────────────────

type MockSkillStore struct {
	mu     sync.RWMutex
	skills map[model.UUID]*model.Skill
}

func NewMockSkillStore() *MockSkillStore {
	return &MockSkillStore{skills: make(map[model.UUID]*model.Skill)}
}

func (m *MockSkillStore) CreateSkill(ctx context.Context, sk *model.Skill) error {
	return nil
}
func (m *MockSkillStore) GetSkill(ctx context.Context, id model.UUID) (*model.Skill, error) {
	return nil, nil
}
func (m *MockSkillStore) ListSkills(ctx context.Context, cursor *model.UUID, limit int) ([]model.Skill, *model.UUID, error) {
	return nil, nil, nil
}
func (m *MockSkillStore) UpdateSkill(ctx context.Context, sk *model.Skill) error                    { return nil }
func (m *MockSkillStore) UpdateVisibility(ctx context.Context, id model.UUID, vis model.SkillVisibility) error {
	return nil
}
func (m *MockSkillStore) DeleteSkill(ctx context.Context, id model.UUID) error                    { return nil }
func (m *MockSkillStore) CreateVersion(ctx context.Context, v *model.SkillVersion) error          { return nil }
func (m *MockSkillStore) GetVersion(ctx context.Context, id model.UUID) (*model.SkillVersion, error) {
	return nil, nil
}
func (m *MockSkillStore) GetLatestVersion(ctx context.Context, skillID model.UUID) (*model.SkillVersion, error) {
	return nil, nil
}
func (m *MockSkillStore) GetVersionByTag(ctx context.Context, skillID model.UUID, version string) (*model.SkillVersion, error) {
	return nil, nil
}
func (m *MockSkillStore) SetDeviceSkill(ctx context.Context, ds *model.DeviceSkill) error                   { return nil }
func (m *MockSkillStore) UpdateDeviceSkillStatus(ctx context.Context, deviceID, skillID model.UUID, status model.SkillInstallStatus) error {
	return nil
}
func (m *MockSkillStore) GetDeviceSkills(ctx context.Context, deviceID model.UUID) ([]model.DeviceSkill, error) {
	return nil, nil
}
func (m *MockSkillStore) DeleteDeviceSkill(ctx context.Context, deviceID, skillID model.UUID) error       { return nil }
func (m *MockSkillStore) IsSkillInstalledOnDevice(ctx context.Context, deviceID, skillID model.UUID) (bool, error) {
	return true, nil
}
func (m *MockSkillStore) SetAgentSkill(ctx context.Context, as *model.AgentSkill) error                    { return nil }
func (m *MockSkillStore) GetAgentSkills(ctx context.Context, agentID model.UUID) ([]model.AgentSkill, error) {
	return nil, nil
}
func (m *MockSkillStore) DeleteAgentSkill(ctx context.Context, agentID, skillID model.UUID) error          { return nil }
func (m *MockSkillStore) CreateGrant(ctx context.Context, g *model.SkillGrant) error                       { return nil }
func (m *MockSkillStore) DeleteGrant(ctx context.Context, skillID model.UUID, pt model.PrincipalType, pid model.UUID) error {
	return nil
}
func (m *MockSkillStore) ListGrants(ctx context.Context, skillID model.UUID) ([]model.SkillGrant, error)   { return nil, nil }
func (m *MockSkillStore) IsSkillVisibleToUser(ctx context.Context, skillID, userID model.UUID, orgID *model.UUID) (bool, error) {
	return true, nil
}
func (m *MockSkillStore) ListVisibleSkills(ctx context.Context, userID model.UUID, orgID *model.UUID) ([]model.Skill, error) {
	return nil, nil
}

// ─── MockTokenStore ────────────────────────────────────────

type MockTokenStore struct{ mu sync.RWMutex }

func NewMockTokenStore() *MockTokenStore { return &MockTokenStore{} }

func (m *MockTokenStore) Create(ctx context.Context, t *model.RefreshToken) error { return nil }
func (m *MockTokenStore) GetByHash(ctx context.Context, hash string) (*model.RefreshToken, error) {
	return nil, nil
}
func (m *MockTokenStore) Revoke(ctx context.Context, id model.UUID) error            { return nil }
func (m *MockTokenStore) RevokeFamily(ctx context.Context, family string) error       { return nil }
func (m *MockTokenStore) RevokeAllForUser(ctx context.Context, userID model.UUID) error { return nil }
func (m *MockTokenStore) DeleteExpired(ctx context.Context) (int64, error)            { return 0, nil }

// ─── MockOrgStore ──────────────────────────────────────────

type MockOrgStore struct{ mu sync.RWMutex }

func NewMockOrgStore() *MockOrgStore { return &MockOrgStore{} }

func (m *MockOrgStore) Create(ctx context.Context, org *model.Organization) error                           { return nil }
func (m *MockOrgStore) GetByID(ctx context.Context, id model.UUID) (*model.Organization, error)              { return nil, nil }
func (m *MockOrgStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.Organization, *model.UUID, error) {
	return nil, nil, nil
}
func (m *MockOrgStore) Update(ctx context.Context, org *model.Organization) error                           { return nil }
func (m *MockOrgStore) Delete(ctx context.Context, id model.UUID) error                                     { return nil }
func (m *MockOrgStore) AddMember(ctx context.Context, orgID, userID model.UUID) error                       { return nil }
func (m *MockOrgStore) RemoveMember(ctx context.Context, userID model.UUID) error                           { return nil }
func (m *MockOrgStore) ListMembers(ctx context.Context, orgID model.UUID) ([]model.User, error)               { return nil, nil }

// ─── MockDeviceStore ───────────────────────────────────────

type MockDeviceStore struct{ mu sync.RWMutex }

func NewMockDeviceStore() *MockDeviceStore { return &MockDeviceStore{} }

func (m *MockDeviceStore) Create(ctx context.Context, d *model.Device) error                                   { return nil }
func (m *MockDeviceStore) GetByID(ctx context.Context, id model.UUID) (*model.Device, error)                    { return nil, nil }
func (m *MockDeviceStore) GetByTokenHash(ctx context.Context, tokenHash string) (*model.Device, error)          { return nil, ErrNotFound }
func (m *MockDeviceStore) UpdateToken(ctx context.Context, id model.UUID, hash string) error                    { return nil }
func (m *MockDeviceStore) UpdateStatus(ctx context.Context, id model.UUID, status model.DeviceStatus) error      { return nil }
func (m *MockDeviceStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.Device, *model.UUID, error) {
	return nil, nil, nil
}
func (m *MockDeviceStore) ListAll(ctx context.Context) ([]model.Device, error)                                   { return nil, nil }
func (m *MockDeviceStore) Delete(ctx context.Context, id model.UUID) error                                       { return nil }
func (m *MockDeviceStore) GetEnrolledDevice(ctx context.Context, code string) (*model.Device, error)              { return nil, nil }
