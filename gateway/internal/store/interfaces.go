package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
)

// ErrNotFound is returned when a store lookup yields no result.
var ErrNotFound = errors.New("store: not found")

// Store interfaces allow mocking for HTTP handler tests.

type UserStoreInterface interface {
	Create(ctx context.Context, user *model.User) error
	GetByID(ctx context.Context, id model.UUID) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	Update(ctx context.Context, user *model.User) error
	UpdateTier(ctx context.Context, userID model.UUID, tier model.UserTier) error
	List(ctx context.Context, cursor *model.UUID, limit int) ([]model.User, *model.UUID, error)
}

type DeviceStoreInterface interface {
	Create(ctx context.Context, d *model.Device) error
	GetByID(ctx context.Context, id model.UUID) (*model.Device, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*model.Device, error)
	Update(ctx context.Context, d *model.Device) error
	UpdateToken(ctx context.Context, id model.UUID, tokenHash string) error
	UpdateStatus(ctx context.Context, id model.UUID, status model.DeviceStatus) error
	List(ctx context.Context, cursor *model.UUID, limit int) ([]model.Device, *model.UUID, error)
	ListAll(ctx context.Context) ([]model.Device, error)
	Delete(ctx context.Context, id model.UUID) error
	GetEnrolledDevice(ctx context.Context, code string) (*model.Device, error)
}

type AgentStoreInterface interface {
	Create(ctx context.Context, a *model.Agent) error
	GetByID(ctx context.Context, id model.UUID) (*model.Agent, error)
	FindIdle(ctx context.Context) (*model.Agent, error)
	Allocate(ctx context.Context, agentID, userID, jobID model.UUID) error
	Release(ctx context.Context, agentID model.UUID) error
	UpdateStatus(ctx context.Context, agentID model.UUID, status model.AgentStatus) error
	ListByDevice(ctx context.Context, deviceID model.UUID) ([]model.Agent, error)
	ListAll(ctx context.Context, cursor *model.UUID, limit int) ([]model.Agent, *model.UUID, error)
	ListByUser(ctx context.Context, userID model.UUID) ([]model.Agent, error)
	IdleCount(ctx context.Context) (int, error)
	CountByDevice(ctx context.Context, deviceID model.UUID) (int, error)
	Delete(ctx context.Context, agentID model.UUID) error
}

type JobStoreInterface interface {
	Create(ctx context.Context, j *model.Job) error
	GetByID(ctx context.Context, id model.UUID) (*model.Job, error)
	UpdateStatus(ctx context.Context, id model.UUID, status model.JobStatus) error
	UpdateProgress(ctx context.Context, id model.UUID, percent int, message string, status model.JobStatus) error
	UpdateResult(ctx context.Context, id model.UUID, status model.JobStatus, result *json.RawMessage) error
	SetAgent(ctx context.Context, jobID, agentID, deviceID model.UUID) error
	ClearAgent(ctx context.Context, jobID model.UUID) error
	Cancel(ctx context.Context, id, userID model.UUID) error
	DequeueNext(ctx context.Context) (*model.Job, error)
	ExpireQueued(ctx context.Context) (int64, error)
	ExpireDispatched(ctx context.Context, timeout time.Duration) (int64, error)
	CountQueuedByUser(ctx context.Context, userID model.UUID) (int, error)
	GetQueuePosition(ctx context.Context, jobID model.UUID) (int, error)
	ListByUser(ctx context.Context, userID model.UUID, cursor *model.UUID, limit int) ([]model.Job, *model.UUID, error)
	ListByAgent(ctx context.Context, agentID model.UUID) ([]model.Job, error)
}

type FileStoreInterface interface {
	Create(ctx context.Context, f *model.File) error
	GetByID(ctx context.Context, id model.UUID) (*model.File, error)
	UpdateStatus(ctx context.Context, id model.UUID, status model.FileStatus) error
	MarkPurged(ctx context.Context, id model.UUID) error
	UpdateSHA256(ctx context.Context, id model.UUID, sha256 string) error
	ListByUser(ctx context.Context, userID model.UUID, cursor *model.UUID, limit int) ([]model.File, *model.UUID, error)
	ListStagedCloud(ctx context.Context, olderThan time.Time) ([]model.File, error)
	LinkToJob(ctx context.Context, fileID, jobID model.UUID) error
	ListByJob(ctx context.Context, jobID model.UUID) ([]model.File, error)
}

type SkillStoreInterface interface {
	CreateSkill(ctx context.Context, sk *model.Skill) error
	GetSkill(ctx context.Context, id model.UUID) (*model.Skill, error)
	ListSkills(ctx context.Context, cursor *model.UUID, limit int) ([]model.Skill, *model.UUID, error)
	UpdateSkill(ctx context.Context, sk *model.Skill) error
	SetLatestVersion(ctx context.Context, skillID model.UUID, version string) error
	UpdateVisibility(ctx context.Context, id model.UUID, vis model.SkillVisibility) error
	DeleteSkill(ctx context.Context, id model.UUID) error
	CreateVersion(ctx context.Context, v *model.SkillVersion) error
	GetVersion(ctx context.Context, id model.UUID) (*model.SkillVersion, error)
	GetLatestVersion(ctx context.Context, skillID model.UUID) (*model.SkillVersion, error)
	GetVersionByTag(ctx context.Context, skillID model.UUID, version string) (*model.SkillVersion, error)
	SetDeviceSkill(ctx context.Context, ds *model.DeviceSkill) error
	UpdateDeviceSkillStatus(ctx context.Context, deviceID, skillID model.UUID, status model.SkillInstallStatus) error
	GetDeviceSkills(ctx context.Context, deviceID model.UUID) ([]model.DeviceSkill, error)
	DeleteDeviceSkill(ctx context.Context, deviceID, skillID model.UUID) error
	IsSkillInstalledOnDevice(ctx context.Context, deviceID, skillID model.UUID) (bool, error)
	SetAgentSkill(ctx context.Context, as *model.AgentSkill) error
	GetAgentSkills(ctx context.Context, agentID model.UUID) ([]model.AgentSkill, error)
	DeleteAgentSkill(ctx context.Context, agentID, skillID model.UUID) error
	CreateGrant(ctx context.Context, g *model.SkillGrant) error
	DeleteGrant(ctx context.Context, skillID model.UUID, pt model.PrincipalType, pid model.UUID) error
	ListGrants(ctx context.Context, skillID model.UUID) ([]model.SkillGrant, error)
	IsSkillVisibleToUser(ctx context.Context, skillID, userID model.UUID, orgID *model.UUID) (bool, error)
	ListVisibleSkills(ctx context.Context, userID model.UUID, orgID *model.UUID) ([]model.Skill, error)
	GetDeviceSkillsForSkill(ctx context.Context, skillID model.UUID) ([]model.DeviceSkill, error)
	GetAgentSkillsForSkill(ctx context.Context, skillID model.UUID) ([]model.SkillRolloutAgentEntry, error)
}

type OrgStoreInterface interface {
	Create(ctx context.Context, org *model.Organization) error
	GetByID(ctx context.Context, id model.UUID) (*model.Organization, error)
	List(ctx context.Context, cursor *model.UUID, limit int) ([]model.Organization, *model.UUID, error)
	Update(ctx context.Context, org *model.Organization) error
	Delete(ctx context.Context, id model.UUID) error
	AddMember(ctx context.Context, orgID, userID model.UUID) error
	RemoveMember(ctx context.Context, userID model.UUID) error
	ListMembers(ctx context.Context, orgID model.UUID) ([]model.User, error)
}

type TokenStoreInterface interface {
	Create(ctx context.Context, t *model.RefreshToken) error
	GetByHash(ctx context.Context, tokenHash string) (*model.RefreshToken, error)
	Revoke(ctx context.Context, id model.UUID) error
	RevokeFamily(ctx context.Context, family string) error
	RevokeAllForUser(ctx context.Context, userID model.UUID) error
	DeleteExpired(ctx context.Context) (int64, error)
}
