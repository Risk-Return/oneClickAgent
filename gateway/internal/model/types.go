// Package model defines domain types, DTOs, enums, and shared value objects
// for the oneClickAgent Cloud Gateway.
// DB struct tags map to the PostgreSQL schema defined in gateway/migrations/.
package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type UUID = uuid.UUID

func NewUUID() UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic("failed to generate UUIDv7: " + err.Error())
	}
	return id
}

func ParseUUID(s string) (UUID, error) { return uuid.Parse(s) }

func MustParseUUID(s string) UUID { return uuid.MustParse(s) }

// ---------- Enums ----------

type UserRole string

const (
	RoleAdmin UserRole = "admin"
	RoleUser  UserRole = "user"
)

func (r UserRole) IsValid() bool { return r == RoleAdmin || r == RoleUser }

type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

type UserTier string

const (
	TierFree       UserTier = "free"
	TierPro        UserTier = "pro"
	TierEnterprise UserTier = "enterprise"
)

func (t UserTier) TierPriority() int {
	switch t {
	case TierEnterprise:
		return 0
	case TierPro:
		return 1
	default:
		return 2
	}
}

func ValidUserTiers() []UserTier {
	return []UserTier{TierFree, TierPro, TierEnterprise}
}

type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobQueued     JobStatus = "queued"
	JobDispatched JobStatus = "dispatched"
	JobRunning    JobStatus = "running"
	JobSucceeded  JobStatus = "succeeded"
	JobFailed     JobStatus = "failed"
	JobCancelled  JobStatus = "cancelled"
)

func (s JobStatus) IsTerminal() bool {
	return s == JobSucceeded || s == JobFailed || s == JobCancelled
}

func (s JobStatus) IsActive() bool {
	return s == JobPending || s == JobQueued || s == JobDispatched || s == JobRunning
}

type DeviceStatus string

const (
	DeviceEnrolled DeviceStatus = "enrolled"
	DeviceOnline   DeviceStatus = "online"
	DeviceOffline  DeviceStatus = "offline"
)

type AgentStatus string

const (
	AgentCreating  AgentStatus = "creating"
	AgentIdle      AgentStatus = "idle"
	AgentBusy      AgentStatus = "busy"
	AgentUnhealthy AgentStatus = "unhealthy"
	AgentFailed    AgentStatus = "failed"
	AgentRemoved   AgentStatus = "removed"
)

type SkillInstallStatus string

const (
	SkillInstalling SkillInstallStatus = "installing"
	SkillInstalled  SkillInstallStatus = "installed"
	SkillDisabled   SkillInstallStatus = "disabled"
	SkillUpdating   SkillInstallStatus = "updating"
	SkillDeleting   SkillInstallStatus = "deleting"
	SkillError      SkillInstallStatus = "error"
)

type AgentSkillStatus string

const (
	AgentSkillEnabled  AgentSkillStatus = "enabled"
	AgentSkillDisabled AgentSkillStatus = "disabled"
)

type FileStatus string

const (
	FileStagedCloud  FileStatus = "staged_cloud"
	FileStagedDevice FileStatus = "staged_device"
	FilePurged       FileStatus = "purged"
	FileStatusError  FileStatus = "error"
)

type SkillVisibility string

const (
	VisibilityPublic     SkillVisibility = "public"
	VisibilityRestricted SkillVisibility = "restricted"
)

type SkillCatalogStatus string

const (
	SkillActive      SkillCatalogStatus = "active"
	SkillDeprecated  SkillCatalogStatus = "deprecated"
)

type PrincipalType string

const (
	PrincipalUser PrincipalType = "user"
	PrincipalOrg  PrincipalType = "org"
)

type SkillScope string

const (
	SkillScopeDevice SkillScope = "device"
	SkillScopeAgent  SkillScope = "agent"
)

type SkillAction string

const (
	SkillActionInstall SkillAction = "install"
	SkillActionEnable  SkillAction = "enable"
	SkillActionDisable SkillAction = "disable"
	SkillActionUpdate  SkillAction = "update"
	SkillActionDelete  SkillAction = "delete"
)

type ErrorCode string

const (
	ErrCodeQueueTimeout     ErrorCode = "QUEUE_TIMEOUT"
	ErrCodeQueueFull        ErrorCode = "QUEUE_FULL"
	ErrCodeDeviceOffline    ErrorCode = "DEVICE_OFFLINE"
	ErrCodeAgentUnavailable ErrorCode = "AGENT_UNAVAILABLE"
	ErrCodeSkillNotEnabled  ErrorCode = "SKILL_NOT_ENABLED"
	ErrCodeLimitExceeded    ErrorCode = "LIMIT_EXCEEDED"
	ErrCodeValidationFailed ErrorCode = "VALIDATION_FAILED"
	ErrCodeNotFound         ErrorCode = "NOT_FOUND"
	ErrCodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden        ErrorCode = "FORBIDDEN"
	ErrCodeConflict         ErrorCode = "CONFLICT"
	ErrCodeInternalError    ErrorCode = "INTERNAL_ERROR"
)

// ---------- Domain Types (db tags match migration SQL columns) ----------

type User struct {
	ID           UUID       `json:"id" db:"id"`
	Email        string     `json:"email" db:"email"`
	Username     string     `json:"username" db:"username"`
	PasswordHash string     `json:"-" db:"password_hash"`
	Status       UserStatus `json:"status" db:"status"`
	Role         UserRole   `json:"role" db:"role"`
	Tier         UserTier   `json:"tier" db:"tier"`
	OrgID        *UUID      `json:"org_id,omitempty" db:"org_id"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

type Organization struct {
	ID          UUID      `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description,omitempty" db:"description"`
	CreatedBy   UUID      `json:"created_by" db:"created_by"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type Device struct {
	ID              UUID         `json:"id" db:"id"`
	OperatorID      UUID         `json:"operator_id" db:"operator_id"`
	Name            string       `json:"name" db:"name"`
	Description     string       `json:"description,omitempty" db:"description"`
	Platform        string       `json:"platform,omitempty" db:"platform"`
	Status          DeviceStatus `json:"status" db:"status"`
	TokenHash       string       `json:"-" db:"token_hash"`
	TokenRotatedAt  *time.Time   `json:"token_rotated_at,omitempty" db:"token_rotated_at"`
	LastSeenAt      *time.Time   `json:"last_seen_at,omitempty" db:"last_seen_at"`
	Resources       *DeviceResources `json:"resources,omitempty" db:"resources"`
	CreatedAt       time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at" db:"updated_at"`
}

type DeviceResources struct {
	CPUCount int   `json:"cpu_count"`
	MemoryMB int64 `json:"mem_mb"`
	DiskMB   int64 `json:"disk_mb"`
}

type Agent struct {
	ID          UUID        `json:"id" db:"id"`
	DeviceID    UUID        `json:"device_id" db:"device_id"`
	UserID      *UUID       `json:"user_id,omitempty" db:"user_id"`
	Name        string      `json:"name" db:"name"`
	Description string      `json:"description,omitempty" db:"description"`
	Image       string      `json:"image" db:"image"`
	Port        int         `json:"port" db:"port"`
	Tags        []string    `json:"tags,omitempty" db:"tags"`
	Status      AgentStatus `json:"status" db:"status"`
	JobID       *UUID       `json:"job_id,omitempty" db:"job_id"`
	Limits      *AgentLimits `json:"limits" db:"limits"`
	AllocatedAt *time.Time  `json:"allocated_at,omitempty" db:"allocated_at"`
	CreatedAt   time.Time   `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at" db:"updated_at"`
}

type AgentLimits struct {
	CPU     int   `json:"cpu"`
	MemMB   int64 `json:"mem_mb"`
	DiskMB  int64 `json:"disk_mb"`
}

type Job struct {
	ID              UUID      `json:"id" db:"id"`
	UserID          UUID      `json:"user_id" db:"user_id"`
	UserTier        UserTier  `json:"user_tier" db:"user_tier"`
	AgentID         *UUID     `json:"agent_id,omitempty" db:"agent_id"`
	DeviceID        *UUID     `json:"device_id,omitempty" db:"device_id"`
	Channel         string    `json:"channel" db:"channel"`
	Command         string    `json:"command" db:"command"`
	Params          *json.RawMessage   `json:"params,omitempty" db:"params"`
	SkillID         *UUID     `json:"skill_id,omitempty" db:"skill_id"`
	Status          JobStatus `json:"status" db:"status"`
	Percent         *int      `json:"percent,omitempty" db:"percent"`
	ProgressMessage *string   `json:"progress_message,omitempty" db:"progress_message"`
	Result          *json.RawMessage `json:"result,omitempty" db:"result"`
	ErrorCode       *string   `json:"error_code,omitempty" db:"error_code"`
	ErrorMessage    *string   `json:"error_message,omitempty" db:"error_message"`
	QueuedAt        *time.Time `json:"queued_at,omitempty" db:"queued_at"`
	QueueExpiresAt  *time.Time `json:"queue_expires_at,omitempty" db:"queue_expires_at"`
	SubmittedAt     time.Time  `json:"submitted_at" db:"submitted_at"`
	StartedAt       *time.Time `json:"started_at,omitempty" db:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty" db:"finished_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`

	// Computed fields (not from DB)
	QueuePosition        *int `json:"queue_position,omitempty" db:"-"`
	EstimatedWaitSeconds *int `json:"estimated_wait_seconds,omitempty" db:"-"`
}

type File struct {
	ID         UUID       `json:"id" db:"id"`
	UserID     UUID       `json:"user_id" db:"user_id"`
	Name       string     `json:"name" db:"name"`
	Size       int64      `json:"size" db:"size"`
	Mime       string     `json:"mime,omitempty" db:"mime"`
	SHA256     string     `json:"sha256" db:"sha256"`
	StorageURI string     `json:"-" db:"storage_uri"`
	Status     FileStatus `json:"status" db:"status"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	PurgedAt   *time.Time `json:"purged_at,omitempty" db:"purged_at"`
}

type Skill struct {
	ID            UUID               `json:"id" db:"id"`
	Key           string             `json:"key" db:"key"`
	Name          string             `json:"name" db:"name"`
	Description   string             `json:"description,omitempty" db:"description"`
	Visibility    SkillVisibility    `json:"visibility" db:"visibility"`
	LatestVersion string             `json:"latest_version,omitempty" db:"latest_version"`
	Status        SkillCatalogStatus `json:"status" db:"status"`
	CreatedAt     time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at" db:"updated_at"`
}

type SkillVersion struct {
	ID          UUID      `json:"id" db:"id"`
	SkillID     UUID      `json:"skill_id" db:"skill_id"`
	Version     string    `json:"version" db:"version"`
	Manifest    json.RawMessage `json:"manifest" db:"manifest"`
	ArtifactURI string    `json:"-" db:"artifact_uri"`
	SHA256      string    `json:"sha256" db:"sha256"`
	Size        *int64    `json:"size,omitempty" db:"size"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type DeviceSkill struct {
	DeviceID     UUID               `json:"device_id" db:"device_id"`
	SkillID      UUID               `json:"skill_id" db:"skill_id"`
	Version      string             `json:"version" db:"version"`
	Status       SkillInstallStatus `json:"status" db:"status"`
	InstalledBy  *UUID              `json:"installed_by,omitempty" db:"installed_by"`
	ErrorMessage *string            `json:"error_message,omitempty" db:"error_message"`
	UpdatedAt    time.Time          `json:"updated_at" db:"updated_at"`
}

type AgentSkill struct {
	AgentID    UUID             `json:"agent_id" db:"agent_id"`
	SkillID    UUID             `json:"skill_id" db:"skill_id"`
	Status     AgentSkillStatus `json:"status" db:"status"`
	SelectedBy *UUID            `json:"selected_by,omitempty" db:"selected_by"`
	UpdatedAt  time.Time        `json:"updated_at" db:"updated_at"`
}

type SkillGrant struct {
	SkillID       UUID          `json:"skill_id" db:"skill_id"`
	PrincipalType PrincipalType `json:"principal_type" db:"principal_type"`
	PrincipalID   UUID          `json:"principal_id" db:"principal_id"`
	GrantedBy     UUID          `json:"granted_by" db:"granted_by"`
	CreatedAt     time.Time     `json:"created_at" db:"created_at"`
}

type RefreshToken struct {
	ID         UUID       `json:"id" db:"id"`
	UserID     UUID       `json:"user_id" db:"user_id"`
	TokenHash  string     `json:"-" db:"token_hash"`
	ExpiresAt  time.Time  `json:"expires_at" db:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	UserAgent  string     `json:"-" db:"user_agent"`
	IP         string     `json:"-" db:"ip"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	Family     string     `json:"-" db:"family"`  // theft detection (refresh token rotation family)
}

type AuditLog struct {
	ID         UUID      `json:"id" db:"id"`
	UserID     *UUID     `json:"user_id,omitempty" db:"user_id"`
	Actor      string    `json:"actor,omitempty" db:"actor"`
	Action     string    `json:"action" db:"action"`
	TargetType string    `json:"target_type,omitempty" db:"target_type"`
	TargetID   *UUID     `json:"target_id,omitempty" db:"target_id"`
	Meta       *json.RawMessage `json:"meta,omitempty" db:"meta"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// ---------- DTOs ----------

type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required,min=12"`
	Name     string `json:"-"` // deprecated, use Username
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type AuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type SubmitJobRequest struct {
	Command       string `json:"command" validate:"required"`
	SkillID       *UUID  `json:"skill_id,omitempty"`
	FileIDs       []UUID `json:"file_ids,omitempty"`
	CredentialIDs []UUID `json:"credential_ids,omitempty"`
}

type JobResponse struct {
	Job                  Job   `json:"job"`
	QueuePosition        *int  `json:"queue_position,omitempty"`
	EstimatedWaitSeconds *int  `json:"estimated_wait_seconds,omitempty"`
	AgentID              *UUID `json:"agent_id,omitempty"`
}

type CreateSkillRequest struct {
	Key         string          `json:"key" validate:"required"`
	Name        string          `json:"name" validate:"required"`
	Description string          `json:"description"`
	Visibility  SkillVisibility `json:"visibility" validate:"required,oneof=public restricted"`
}

type PublishSkillVersionRequest struct {
	Version  string `json:"version" validate:"required"`
	Manifest string `json:"manifest" validate:"required"`
}

type FleetSkillRequest struct {
	Version string `json:"version" validate:"required"`
}

type SkillVisibilityUpdate struct {
	Visibility SkillVisibility `json:"visibility" validate:"required,oneof=public restricted"`
}

type SkillGrantRequest struct {
	PrincipalType PrincipalType `json:"principal_type" validate:"required,oneof=user org"`
	PrincipalID   UUID          `json:"principal_id" validate:"required"`
}

type CreateOrgRequest struct {
	Name        string `json:"name" validate:"required"`
	Description string `json:"description"`
}

type UpdateOrgMemberRequest struct {
	UserID UUID `json:"user_id" validate:"required"`
}

type UpdateUserTierRequest struct {
	Tier UserTier `json:"tier" validate:"required,oneof=free pro enterprise"`
}

type CreateDeviceRequest struct {
	Name        string `json:"name" validate:"required"`
	Description string `json:"description,omitempty"`
}

type UpdateDeviceRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type CreateDeviceResponse struct {
	Device         Device `json:"device"`
	EnrollmentCode string `json:"enrollment_code"`
}

type SetPoolSizeRequest struct {
	Size int `json:"size" validate:"required,min=1"`
}

type PaginationParams struct {
	Cursor *UUID `json:"cursor,omitempty"`
	Limit  int   `json:"limit"`
}

type PaginatedResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor *UUID  `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

type APIError struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message"`
	Details   *string   `json:"details,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
}

type APIErrorResponse struct {
	Error APIError `json:"error"`
}

type EnrollmentRequest struct {
	EnrollmentCode string `json:"enrollment_code" validate:"required"`
}

type EnrollmentResponse struct {
	DeviceID    UUID   `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

type EnableSkillRequest struct {
	SkillID UUID `json:"skill_id" validate:"required"`
}

type DrainAgentResponse struct {
	AgentID UUID        `json:"agent_id"`
	Status  AgentStatus `json:"status"`
}

type SkillWithLatestVersion struct {
	Skill
	LatestVersion *SkillVersion `json:"latest_version,omitempty"`
}

type SkillVisible struct {
	Skill
	LatestVersion *SkillVersion `json:"latest_version,omitempty"`
	Enabled       *bool         `json:"enabled,omitempty"`
}

type SkillRolloutEntry struct {
	DeviceID    UUID               `json:"device_id"`
	DeviceName  string             `json:"device_name"`
	Version     string             `json:"version,omitempty"`
	Status      SkillInstallStatus `json:"status"`
	Error       *string            `json:"error,omitempty"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

type VNCStatusResponse struct {
	SessionID *UUID             `json:"session_id,omitempty"`
	Status    VNCSessionStatus `json:"status"`
}

// ---------- Tunnel Frame Types ----------

const FrameVersion = 1

const (
	SubprotocolTunnel = "iagent.tunnel.v1"
	SubprotocolSession = "iagent.session.v1"
	SubprotocolWeb     = "iagent.web.v1"
)

const HelloTimeout = 10 * time.Second
const AckRetransmitBase = 1 * time.Second
const AckRetransmitMaxRetries = 3
const ChunkSizeBytes = 256 * 1024

type Frame struct {
	Version int             `json:"v"`
	Type    FrameType       `json:"type"`
	MsgID   string          `json:"msg_id"`
	AckID   *string         `json:"ack_id,omitempty"`
	TS      int64           `json:"ts"`
	Payload json.RawMessage `json:"payload"`
}

type FrameType string

const (
	FrameHello     FrameType = "HELLO"
	FrameHelloAck  FrameType = "HELLO_ACK"
	FramePing      FrameType = "PING"
	FramePong      FrameType = "PONG"
	FrameAck       FrameType = "ACK"
	FrameError     FrameType = "ERROR"
	FrameStateSync FrameType = "STATE_SYNC"

	FrameJobDispatch FrameType = "JOB_DISPATCH"
	FrameJobCancel   FrameType = "JOB_CANCEL"
	FrameJobQuery    FrameType = "JOB_QUERY"

	FrameJobAccepted FrameType = "JOB_ACCEPTED"
	FrameJobProgress FrameType = "JOB_PROGRESS"
	FrameJobResult   FrameType = "JOB_RESULT"
	FrameJobRejected FrameType = "JOB_REJECTED"

	FrameAgentCreate    FrameType = "AGENT_CREATE"
	FrameAgentAction    FrameType = "AGENT_ACTION"
	FrameAgentStatusReq FrameType = "AGENT_STATUS_REQ"
	FrameAgentStatus    FrameType = "AGENT_STATUS"

	FrameSkillDispatchBegin FrameType = "SKILL_DISPATCH_BEGIN"
	FrameSkillChunk         FrameType = "SKILL_CHUNK"
	FrameSkillDispatchEnd   FrameType = "SKILL_DISPATCH_END"
	FrameSkillAction        FrameType = "SKILL_ACTION"
	FrameSkillState         FrameType = "SKILL_STATE"
	FrameSkillSync          FrameType = "SKILL_SYNC"

	FrameFilePushBegin FrameType = "FILE_PUSH_BEGIN"
	FrameFileChunk     FrameType = "FILE_CHUNK"
	FrameFilePushEnd   FrameType = "FILE_PUSH_END"
	FrameFileAck       FrameType = "FILE_ACK"

	FrameSkillDispatchAck FrameType = "SKILL_DISPATCH_ACK"
	FrameFilePurged       FrameType = "FILE_PURGED"
)

const FrameMaxSize = 1 << 20

// ---------- Frame Payloads ----------

type HelloPayload struct {
	DeviceID     UUID           `json:"device_id"`
	AgentVersion string         `json:"agent_version,omitempty"`
	Platform     string         `json:"platform,omitempty"`
	AgentCount   int            `json:"agent_count"`
	Agents       []HelloAgent   `json:"agents"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Resources    HelloResources `json:"resources,omitempty"`
}

type HelloAgent struct {
	AgentID     UUID        `json:"agent_id"`
	ContainerID string      `json:"container_id,omitempty"`
	Status      AgentStatus `json:"status"`
	Port        int         `json:"port,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
}

type HelloResources struct {
	CPUCount int   `json:"cpu_count"`
	MemoryMB int64 `json:"memory_mb"`
	DiskMB   int64 `json:"disk_mb"`
}

type HelloAckConfig struct {
	HeartbeatS    int `json:"heartbeat_s"`
	MaxFrameBytes int `json:"max_frame_bytes"`
}

type HelloAckPayload struct {
	ServerTime int64           `json:"server_time"`
	SessionID  string          `json:"session_id"`
	Config     HelloAckConfig  `json:"config"`
}

type JobDispatchPayload struct {
	JobID         UUID              `json:"job_id"`
	UserID        UUID              `json:"user_id"`
	AgentID       UUID              `json:"agent_id"`
	Command       string            `json:"command"`
	Params        json.RawMessage   `json:"params,omitempty"`
	SkillID       *UUID             `json:"skill_id,omitempty"`
	FileIDs       []UUID            `json:"file_ids,omitempty"`
	CredentialIDs []UUID            `json:"credential_ids,omitempty"`
	SubmittedAt   int64             `json:"submitted_at"`
}

type JobProgressPayload struct {
	JobID     UUID      `json:"job_id"`
	EventSeq  int       `json:"event_seq"`
	Status    JobStatus `json:"status"`
	Percent   int       `json:"percent"`
	Message   string    `json:"message"`
}

type JobResultPayload struct {
	JobID    UUID      `json:"job_id"`
	Status   JobStatus `json:"status"`
	Result   *string   `json:"result,omitempty"`
	ErrorMsg *string   `json:"error_msg,omitempty"`
}

type JobRejectedPayload struct {
	JobID  UUID   `json:"job_id"`
	Reason string `json:"reason"`
}

type AgentCreatePayload struct {
	AgentID UUID        `json:"agent_id"`
	Image   string      `json:"image"`
	Tags    []string    `json:"tags,omitempty"`
	Limits  AgentLimits `json:"limits"`
	Env     []string    `json:"env,omitempty"`
}

type AgentStatusPayload struct {
	AgentID     UUID        `json:"agent_id"`
	Status      AgentStatus `json:"status"`
	ContainerID *string     `json:"container_id,omitempty"`
	CPUPercent  *float64    `json:"cpu_percent,omitempty"`
	MemoryMB    *int64      `json:"memory_mb,omitempty"`
}

type SkillDispatchBeginPayload struct {
	SkillID        UUID   `json:"skill_id"`
	SkillVersionID UUID   `json:"skill_version_id"`
	Version        string `json:"version"`
	TotalChunks    int    `json:"total_chunks"`
	TotalBytes     int64  `json:"total_bytes"`
	SHA256         string `json:"sha256"`
}

type SkillChunkPayload struct {
	SkillID        UUID   `json:"skill_id"`
	SkillVersionID UUID   `json:"skill_version_id"`
	ChunkIndex     int    `json:"chunk_index"`
	Data           string `json:"data"`
}

type SkillDispatchEndPayload struct {
	SkillID        UUID `json:"skill_id"`
	SkillVersionID UUID `json:"skill_version_id"`
}

type SkillActionPayload struct {
	Scope   SkillScope  `json:"scope"`
	Action  SkillAction `json:"action"`
	SkillID UUID        `json:"skill_id"`
	Version *string     `json:"version,omitempty"`
	AgentID *UUID       `json:"agent_id,omitempty"`
}

type SkillStatePayload struct {
	SkillID        UUID        `json:"skill_id"`
	SkillVersionID UUID        `json:"skill_version_id"`
	Scope          SkillScope  `json:"scope"`
	Status         SkillInstallStatus `json:"status"`
	AgentID        *UUID       `json:"agent_id,omitempty"`
	Error          *string     `json:"error,omitempty"`
}

type SkillSyncPayload struct {
	DeviceSkills []DeviceSkill `json:"device_skills"`
	AgentSkills  []AgentSkill  `json:"agent_skills"`
}

type FilePushBeginPayload struct {
	FileID      UUID   `json:"file_id"`
	FileName    string `json:"file_name"`
	SizeBytes   int64  `json:"size_bytes"`
	TotalChunks int    `json:"total_chunks"`
	SHA256      string `json:"sha256"`
}

type FileChunkPayload struct {
	FileID     UUID   `json:"file_id"`
	ChunkIndex int    `json:"chunk_index"`
	Data       string `json:"data"`
}

type FilePushEndPayload struct {
	FileID UUID `json:"file_id"`
}

type FileAckPayload struct {
	FileID UUID       `json:"file_id"`
	Status FileStatus `json:"status"`
	Error  *string    `json:"error,omitempty"`
}

type StateSyncPayload struct {
	Jobs   []StateSyncJob   `json:"jobs"`
	Agents []StateSyncAgent `json:"agents"`
}

type StateSyncJob struct {
	JobID   UUID      `json:"job_id"`
	AgentID UUID      `json:"agent_id"`
	Status  JobStatus `json:"status"`
}

type StateSyncAgent struct {
	AgentID     UUID        `json:"agent_id"`
	ContainerID string      `json:"container_id"`
	Status      AgentStatus `json:"status"`
	JobID       *UUID       `json:"job_id,omitempty"`
}

// ---------- WebSocket Events ----------

type WSEvent struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

const (
	WSEventJobProgress  = "job.progress"
	WSEventJobStatus    = "job.status"
	WSEventJobResult    = "job.result"
	WSEventAgentStatus  = "agent.status"
	WSEventDeviceStatus = "device.status"
	WSEventSkillRollout = "skill.rollout"
)

// ---------- Pool Types ----------

type PoolAgentInfo struct {
	Agent
	DeviceName string `json:"device_name"`
}

type PoolStats struct {
	TotalAgents   int `json:"total_agents"`
	IdleAgents    int `json:"idle_agents"`
	BusyAgents    int `json:"busy_agents"`
	OnlineDevices int `json:"online_devices"`
}

// ─── VNC / Docker Types ─────────────────────────────────────

// VNCSessionStatus represents the lifecycle of a VNC relay session.
type VNCSessionStatus string

const (
	VNCSessionPending VNCSessionStatus = "pending"
	VNCSessionReady   VNCSessionStatus = "ready"
	VNCSessionActive  VNCSessionStatus = "active"
	VNCSessionClosed  VNCSessionStatus = "closed"
	VNCSessionError   VNCSessionStatus = "error"
)

// VNCSession represents an interactive browser relay session.
type VNCSession struct {
	ID               UUID             `json:"id" db:"id"`
	JobID            UUID             `json:"job_id" db:"job_id"`
	UserID           UUID             `json:"user_id" db:"user_id"`
	DeviceID         UUID             `json:"device_id" db:"device_id"`
	AgentID          UUID             `json:"agent_id" db:"agent_id"`
	SessionTokenHash string           `json:"-" db:"session_token_hash"`
	RFBPassword      *string          `json:"rfb_password,omitempty" db:"rfb_password"`
	Status           VNCSessionStatus `json:"status" db:"status"`
	GatewayNode      *string          `json:"gateway_node,omitempty" db:"gateway_node"`
	IdleTTLSecs      int              `json:"idle_ttl_secs" db:"idle_ttl_secs"`
	MaxTTLSecs       int              `json:"max_ttl_secs" db:"max_ttl_secs"`
	LastActiveAt     *time.Time       `json:"last_active_at,omitempty" db:"last_active_at"`
	TokenExpiresAt   *time.Time       `json:"token_expires_at,omitempty" db:"token_expires_at"`
	StartedAt        *time.Time       `json:"started_at,omitempty" db:"started_at"`
	CloseReason      *string          `json:"close_reason,omitempty" db:"close_reason"`
	CreatedAt        time.Time        `json:"created_at" db:"created_at"`
	EndedAt          *time.Time       `json:"ended_at,omitempty" db:"ended_at"`
}

// BrowserCredential stores an encrypted browser login cookie.
type BrowserCredential struct {
	ID              UUID       `json:"id" db:"id"`
	UserID          UUID       `json:"user_id" db:"user_id"`
	Label           string     `json:"label" db:"label"`
	Origin          string     `json:"origin" db:"origin"`
	StorageStateEnc []byte     `json:"-" db:"storage_state_enc"`
	Nonce           []byte     `json:"-" db:"nonce"`
	AuthTag         []byte     `json:"-" db:"auth_tag"`
	KeyID           string     `json:"key_id" db:"key_id"`
	SHA256          string     `json:"sha256" db:"sha256"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// JobCredential links a job to injected credentials.
type JobCredential struct {
	JobID        UUID `json:"job_id" db:"job_id"`
	CredentialID UUID `json:"credential_id" db:"credential_id"`
}

// ─── New Frame Types ────────────────────────────────────────

const (
	// VNC frames
	FrameVNCOpen    FrameType = "VNC_OPEN"
	FrameVNCOpened  FrameType = "VNC_OPENED"
	FrameVNCClose   FrameType = "VNC_CLOSE"

	// Credential frames
	FrameCredPush    FrameType = "CRED_PUSH"
	FrameCredPushAck FrameType = "CRED_PUSH_ACK"
	FrameCredCapture FrameType = "CRED_CAPTURE"
	FrameCredCaptureAck FrameType = "CRED_CAPTURE_ACK"
)

// VNCOpenPayload is the payload for VNC_OPEN frame (gateway → device).
type VNCOpenPayload struct {
	SessionID    UUID   `json:"session_id"`
	AgentID      UUID   `json:"agent_id"`
	JobID        UUID   `json:"job_id"`
	RelayURL     string `json:"relay_url"`
	SessionToken string `json:"session_token"`
	TTLSecs      int    `json:"ttl_secs"`
}

// VNCOpenedPayload is the payload for VNC_OPENED frame (device → gateway).
type VNCOpenedPayload struct {
	SessionID   UUID   `json:"session_id"`
	Status      string `json:"status"` // ready | error
	RFBPassword string `json:"rfb_password,omitempty"`
	Error       string `json:"error,omitempty"`
}

// VNCClosePayload is the payload for VNC_CLOSE frame (gateway → device).
type VNCClosePayload struct {
	SessionID UUID   `json:"session_id"`
	Reason    string `json:"reason"`
}

// CredPushPayload is the payload for CRED_PUSH frame (gateway → device).
type CredPushPayload struct {
	JobID        UUID   `json:"job_id"`
	CredentialID UUID   `json:"credential_id"`
	Origin       string `json:"origin"`
	StorageState string `json:"storage_state"`
	SHA256       string `json:"sha256"`
}

// CredPushAckPayload is the payload for CRED_PUSH_ACK frame (device → gateway).
type CredPushAckPayload struct {
	JobID        UUID   `json:"job_id"`
	CredentialID UUID   `json:"credential_id"`
	Status       string `json:"status"` // ok | error
	Error        string `json:"error,omitempty"`
}

// CredCapturePayload is the payload for CRED_CAPTURE frame (device → gateway).
type CredCapturePayload struct {
	SessionID UUID   `json:"session_id"`
	JobID     UUID   `json:"job_id"`
	AgentID   UUID   `json:"agent_id"`
	Origin    string `json:"origin"`
	Data      string `json:"data"`  // base64-encoded browser storage state
	SHA256    string `json:"sha256"`
	Label     string `json:"label,omitempty"`
}

// CredCaptureAckPayload is the payload for CRED_CAPTURE_ACK frame (gateway → device).
type CredCaptureAckPayload struct {
	CredentialID UUID   `json:"credential_id,omitempty"`
	SessionID    UUID   `json:"session_id"`
	Status       string `json:"status"` // STORED | error
	Error        string `json:"error,omitempty"`
}

// ─── VNC + Credential DTOs ──────────────────────────────────

// VNCOpenRequest is the payload for POST /jobs/{id}/vnc.
type VNCOpenRequest struct{}

// VNCOpenResponse is returned after opening a VNC session.
type VNCOpenResponse struct {
	SessionID   UUID   `json:"session_id"`
	WSUrl       string `json:"ws_url"`       // /ws/vnc/{id}
	RFBPassword string `json:"rfb_password,omitempty"`
	TTLSecs     int    `json:"ttl_secs"`
}

// SaveLoginRequest is the payload for POST /vnc/{id}/save-login.
type SaveLoginRequest struct {
	Label string `json:"label"`
}

// CreateCredentialRequest is the payload for POST /credentials (admin push).
type CreateCredentialRequest struct {
	Label  string `json:"label" validate:"required"`
	Origin string `json:"origin" validate:"required"`
}

// UpdateCredentialRequest is the payload for PATCH /credentials/{id}.
type UpdateCredentialRequest struct {
	Label string `json:"label" validate:"required"`
}
type CredentialResponse struct {
	ID         UUID       `json:"id"`
	Label      string     `json:"label"`
	Origin     string     `json:"origin"`
	SHA256     string     `json:"sha256"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func CredentialResponseFrom(bc *BrowserCredential) CredentialResponse {
	return CredentialResponse{
		ID:         bc.ID,
		Label:      bc.Label,
		Origin:     bc.Origin,
		SHA256:     bc.SHA256,
		LastUsedAt: bc.LastUsedAt,
		CreatedAt:  bc.CreatedAt,
		UpdatedAt:  bc.UpdatedAt,
	}
}
