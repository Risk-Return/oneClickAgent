// Package model defines domain types, DTOs, enums, and shared value objects
// for the IAgent Cloud Gateway.
package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// UUID is an alias for google/uuid UUID.
type UUID = uuid.UUID

// NewUUID generates a UUIDv7 (time-ordered) for use as primary keys.
func NewUUID() UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic("failed to generate UUIDv7: " + err.Error())
	}
	return id
}

// ParseUUID parses a UUID string.
func ParseUUID(s string) (UUID, error) {
	return uuid.Parse(s)
}

// MustParseUUID parses a UUID string, panicking on error.
func MustParseUUID(s string) UUID {
	return uuid.MustParse(s)
}

// ---------- Enums ----------

// UserRole represents the role of a user in the system.
type UserRole string

const (
	RoleAdmin UserRole = "admin"
	RoleUser  UserRole = "user"
)

// ValidUserRoles returns all valid user roles.
func ValidUserRoles() []UserRole {
	return []UserRole{RoleAdmin, RoleUser}
}

// IsValid checks if the role is valid.
func (r UserRole) IsValid() bool {
	return r == RoleAdmin || r == RoleUser
}

// UserTier represents the subscription tier of a customer.
type UserTier string

const (
	TierFree       UserTier = "free"
	TierPro        UserTier = "pro"
	TierEnterprise UserTier = "enterprise"
)

// TierPriority maps a tier to its queue priority (lower = higher priority).
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

// ValidUserTiers returns all valid user tiers.
func ValidUserTiers() []UserTier {
	return []UserTier{TierFree, TierPro, TierEnterprise}
}

// JobStatus represents the lifecycle status of a job.
type JobStatus string

const (
	JobPending    JobStatus = "PENDING"
	JobQueued     JobStatus = "QUEUED"
	JobDispatched JobStatus = "DISPATCHED"
	JobRunning    JobStatus = "RUNNING"
	JobSucceeded  JobStatus = "SUCCEEDED"
	JobFailed     JobStatus = "FAILED"
	JobCancelled  JobStatus = "CANCELLED"
)

// IsTerminal returns true if the job status is a terminal state.
func (s JobStatus) IsTerminal() bool {
	return s == JobSucceeded || s == JobFailed || s == JobCancelled
}

// IsActive returns true if the job is still in progress.
func (s JobStatus) IsActive() bool {
	return s == JobPending || s == JobQueued || s == JobDispatched || s == JobRunning
}

// DeviceStatus represents the connection status of a local device.
type DeviceStatus string

const (
	DeviceEnrolled DeviceStatus = "ENROLLED"
	DeviceOnline   DeviceStatus = "ONLINE"
	DeviceOffline  DeviceStatus = "OFFLINE"
)

// AgentStatus represents the pool/health status of an agent container.
type AgentStatus string

const (
	AgentCreating  AgentStatus = "CREATING"
	AgentIdle      AgentStatus = "IDLE"
	AgentBusy      AgentStatus = "BUSY"
	AgentUnhealthy AgentStatus = "UNHEALTHY"
	AgentFailed    AgentStatus = "FAILED"
	AgentRemoved   AgentStatus = "REMOVED"
)

// SkillStatus represents the installation status of a skill on a device or agent.
type SkillStatus string

const (
	SkillInstalling SkillStatus = "INSTALLING"
	SkillInstalled  SkillStatus = "INSTALLED"
	SkillDisabled   SkillStatus = "DISABLED"
	SkillUpdating   SkillStatus = "UPDATING"
	SkillDeleting   SkillStatus = "DELETING"
	SkillError      SkillStatus = "ERROR"
)

// FileStatus represents the staging lifecycle of a file.
type FileStatus string

const (
	FileStagedCloud  FileStatus = "STAGED_CLOUD"
	FileStagedDevice FileStatus = "STAGED_DEVICE"
	FilePurged       FileStatus = "PURGED"
)

// SkillVisibility controls who can see a skill.
type SkillVisibility string

const (
	VisibilityPublic     SkillVisibility = "public"
	VisibilityRestricted SkillVisibility = "restricted"
)

// PrincipalType identifies the target of a skill grant.
type PrincipalType string

const (
	PrincipalUser PrincipalType = "user"
	PrincipalOrg  PrincipalType = "org"
)

// SkillActionScope defines the scope for a skill action frame.
type SkillActionScope string

const (
	SkillScopeDevice SkillActionScope = "device"
	SkillScopeAgent  SkillActionScope = "agent"
)

// SkillAction defines the type of skill operation.
type SkillAction string

const (
	SkillActionInstall SkillAction = "install"
	SkillActionEnable  SkillAction = "enable"
	SkillActionDisable SkillAction = "disable"
	SkillActionUpdate  SkillAction = "update"
	SkillActionDelete  SkillAction = "delete"
)

// ErrorCode represents machine-readable error codes.
type ErrorCode string

const (
	ErrCodeQueueTimeout      ErrorCode = "QUEUE_TIMEOUT"
	ErrCodeQueueFull         ErrorCode = "QUEUE_FULL"
	ErrCodeDeviceOffline     ErrorCode = "DEVICE_OFFLINE"
	ErrCodeAgentUnavailable  ErrorCode = "AGENT_UNAVAILABLE"
	ErrCodeLimitExceeded     ErrorCode = "LIMIT_EXCEEDED"
	ErrCodeValidationFailed  ErrorCode = "VALIDATION_FAILED"
	ErrCodeNotFound          ErrorCode = "NOT_FOUND"
	ErrCodeUnauthorized      ErrorCode = "UNAUTHORIZED"
	ErrCodeForbidden         ErrorCode = "FORBIDDEN"
	ErrCodeConflict          ErrorCode = "CONFLICT"
	ErrCodeInternalError     ErrorCode = "INTERNAL_ERROR"
)

// ---------- Domain Types ----------

// User represents a registered user (admin or customer).
type User struct {
	ID           UUID      `json:"id" db:"id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Name         string    `json:"name" db:"name"`
	Role         UserRole  `json:"role" db:"role"`
	Tier         UserTier  `json:"tier" db:"tier"`
	OrgID        *UUID     `json:"org_id,omitempty" db:"org_id"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// Device represents a local device registered with the gateway.
type Device struct {
	ID               UUID         `json:"id" db:"id"`
	DeviceName       string       `json:"device_name" db:"device_name"`
	DeviceTokenHash  string       `json:"-" db:"device_token_hash"`
	Status           DeviceStatus `json:"status" db:"status"`
	HostInfo         string       `json:"host_info,omitempty" db:"host_info"`
	AgentPoolSize    int          `json:"agent_pool_size" db:"agent_pool_size"`
	EnrollmentCode   string       `json:"-" db:"enrollment_code"`
	EnrollmentCodeAt time.Time    `json:"-" db:"enrollment_code_at"`
	CreatedAt        time.Time    `json:"created_at" db:"created_at"`
	LastSeenAt       *time.Time   `json:"last_seen_at,omitempty" db:"last_seen_at"`
}

// Agent represents a pooled agent container on a device.
type Agent struct {
	ID          UUID        `json:"id" db:"id"`
	DeviceID    UUID        `json:"device_id" db:"device_id"`
	ContainerID string      `json:"container_id,omitempty" db:"container_id"`
	Status      AgentStatus `json:"status" db:"status"`
	UserID      *UUID       `json:"user_id,omitempty" db:"user_id"`
	JobID       *UUID       `json:"job_id,omitempty" db:"job_id"`
	AgentName   string      `json:"agent_name" db:"agent_name"`
	CreatedAt   time.Time   `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at" db:"updated_at"`
}

// Job represents a unit of work submitted by a customer.
type Job struct {
	ID              UUID      `json:"id" db:"id"`
	UserID          UUID      `json:"user_id" db:"user_id"`
	AgentID         *UUID     `json:"agent_id,omitempty" db:"agent_id"`
	DeviceID        *UUID     `json:"device_id,omitempty" db:"device_id"`
	Command         string    `json:"command" db:"command"`
	SkillID         *UUID     `json:"skill_id,omitempty" db:"skill_id"`
	Status          JobStatus `json:"status" db:"status"`
	ErrorCode       *string   `json:"error_code,omitempty" db:"error_code"`
	QueuePosition   *int      `json:"queue_position,omitempty" db:"-"`
	QueuedAt        *time.Time `json:"queued_at,omitempty" db:"queued_at"`
	QueueExpiresAt  *time.Time `json:"queue_expires_at,omitempty" db:"queue_expires_at"`
	Result          *string    `json:"result,omitempty" db:"result"`
	ProgressPercent *int       `json:"progress_percent,omitempty" db:"progress_percent"`
	ProgressMessage *string    `json:"progress_message,omitempty" db:"progress_message"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty" db:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty" db:"completed_at"`
}

// File represents an uploaded file tracked by the gateway.
type File struct {
	ID          UUID       `json:"id" db:"id"`
	UserID      UUID       `json:"user_id" db:"user_id"`
	FileName    string     `json:"file_name" db:"file_name"`
	SizeBytes   int64      `json:"size_bytes" db:"size_bytes"`
	MimeType    string     `json:"mime_type" db:"mime_type"`
	SHA256      string     `json:"sha256,omitempty" db:"sha256"`
	Status      FileStatus `json:"status" db:"status"`
	StoragePath string     `json:"-" db:"storage_path"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	PurgedAt    *time.Time `json:"purged_at,omitempty" db:"purged_at"`
}

// Skill represents a skill in the cloud vault catalog.
type Skill struct {
	ID          UUID            `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Description string          `json:"description" db:"description"`
	Visibility  SkillVisibility `json:"visibility" db:"visibility"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}

// SkillVersion represents a published version of a skill.
type SkillVersion struct {
	ID           UUID      `json:"id" db:"id"`
	SkillID      UUID      `json:"skill_id" db:"skill_id"`
	Version      string    `json:"version" db:"version"`
	Manifest     string    `json:"manifest" db:"manifest"`
	ArtifactPath string    `json:"-" db:"artifact_path"`
	SHA256       string    `json:"sha256" db:"sha256"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// DeviceSkill tracks the fleet installation state of a skill on a device.
type DeviceSkill struct {
	DeviceID       UUID        `json:"device_id" db:"device_id"`
	SkillID        UUID        `json:"skill_id" db:"skill_id"`
	SkillVersionID UUID        `json:"skill_version_id" db:"skill_version_id"`
	Status         SkillStatus `json:"status" db:"status"`
	InstalledAt    *time.Time  `json:"installed_at,omitempty" db:"installed_at"`
}

// AgentSkill tracks per-agent enable/disable state for a skill.
type AgentSkill struct {
	AgentID        UUID        `json:"agent_id" db:"agent_id"`
	SkillID        UUID        `json:"skill_id" db:"skill_id"`
	SkillVersionID UUID        `json:"skill_version_id" db:"skill_version_id"`
	Status         SkillStatus `json:"status" db:"status"`
	EnabledAt      *time.Time  `json:"enabled_at,omitempty" db:"enabled_at"`
}

// Organization represents a group of customers.
type Organization struct {
	ID        UUID      `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// SkillGrant represents a visibility grant for a skill.
type SkillGrant struct {
	SkillID       UUID          `json:"skill_id" db:"skill_id"`
	PrincipalType PrincipalType `json:"principal_type" db:"principal_type"`
	PrincipalID   UUID          `json:"principal_id" db:"principal_id"`
	GrantedAt     time.Time     `json:"granted_at" db:"granted_at"`
}

// RefreshToken represents a stored refresh token.
type RefreshToken struct {
	ID        UUID       `json:"id" db:"id"`
	UserID    UUID       `json:"user_id" db:"user_id"`
	TokenHash string     `json:"-" db:"token_hash"`
	Family    string     `json:"-" db:"family"`
	ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
}

// AuditLog represents an audit entry for sensitive admin actions.
type AuditLog struct {
	ID           UUID      `json:"id" db:"id"`
	ActorID      UUID      `json:"actor_id" db:"actor_id"`
	Action       string    `json:"action" db:"action"`
	ResourceType string    `json:"resource_type" db:"resource_type"`
	ResourceID   *UUID     `json:"resource_id,omitempty" db:"resource_id"`
	Detail       string    `json:"detail,omitempty" db:"detail"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// ---------- DTOs ----------

// RegisterRequest is the payload for POST /auth/register.
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=12"`
	Name     string `json:"name" validate:"required"`
}

// LoginRequest is the payload for POST /auth/login.
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// AuthResponse is returned after login/register/refresh.
type AuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
}

// RefreshRequest is the payload for POST /auth/refresh.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// SubmitJobRequest is the payload for POST /jobs.
type SubmitJobRequest struct {
	Command  string  `json:"command" validate:"required"`
	SkillID  *UUID   `json:"skill_id,omitempty"`
	FileIDs  []UUID  `json:"file_ids,omitempty"`
}

// JobResponse is the response for job operations.
type JobResponse struct {
	Job                  Job    `json:"job"`
	QueuePosition        *int   `json:"queue_position,omitempty"`
	EstimatedWaitSeconds *int   `json:"estimated_wait_seconds,omitempty"`
	AgentID              *UUID  `json:"agent_id,omitempty"`
}

// CreateSkillRequest is the payload for admin skill creation.
type CreateSkillRequest struct {
	Name        string          `json:"name" validate:"required"`
	Description string          `json:"description"`
	Visibility  SkillVisibility `json:"visibility" validate:"required,oneof=public restricted"`
}

// PublishSkillVersionRequest is the payload for admin skill version publishing.
type PublishSkillVersionRequest struct {
	Version  string `json:"version" validate:"required"`
	Manifest string `json:"manifest" validate:"required"`
}

// FleetSkillRequest is the payload for admin fleet skill management.
type FleetSkillRequest struct {
	Version string `json:"version" validate:"required"`
}

// SkillVisibilityUpdate is the payload for updating skill visibility.
type SkillVisibilityUpdate struct {
	Visibility SkillVisibility `json:"visibility" validate:"required,oneof=public restricted"`
}

// SkillGrantRequest is the payload for creating/removing skill grants.
type SkillGrantRequest struct {
	PrincipalType PrincipalType `json:"principal_type" validate:"required,oneof=user org"`
	PrincipalID   UUID          `json:"principal_id" validate:"required"`
}

// CreateOrgRequest is the payload for creating an organization.
type CreateOrgRequest struct {
	Name string `json:"name" validate:"required"`
}

// UpdateOrgMemberRequest is the payload for adding/removing org members.
type UpdateOrgMemberRequest struct {
	UserID UUID `json:"user_id" validate:"required"`
}

// UpdateUserTierRequest is the payload for changing a user's tier.
type UpdateUserTierRequest struct {
	Tier UserTier `json:"tier" validate:"required,oneof=free pro enterprise"`
}

// SetPoolSizeRequest is the payload for setting agent pool size on a device.
type SetPoolSizeRequest struct {
	Size int `json:"size" validate:"required,min=1"`
}

// UpdateUserRequest is the payload for updating user profile.
type UpdateUserRequest struct {
	Name *string `json:"name,omitempty"`
}

// PaginationParams holds cursor-based pagination parameters.
type PaginationParams struct {
	Cursor *UUID `json:"cursor,omitempty"`
	Limit  int   `json:"limit"`
}

// PaginatedResponse wraps a paginated list response.
type PaginatedResponse[T any] struct {
	Data       []T    `json:"data"`
	NextCursor *UUID  `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// APIError represents a structured API error response.
type APIError struct {
	Code    ErrorCode  `json:"code"`
	Message string     `json:"message"`
	Details *string    `json:"details,omitempty"`
}

// APIErrorResponse wraps an API error for JSON responses.
type APIErrorResponse struct {
	Error APIError `json:"error"`
}

// EnrollmentRequest is the payload for POST /devices/enroll.
type EnrollmentRequest struct {
	EnrollmentCode string `json:"enrollment_code" validate:"required"`
}

// EnrollmentResponse is returned after successful device enrollment.
type EnrollmentResponse struct {
	DeviceID    UUID   `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

// ---------- Tunnel Frame Types ----------

// FrameVersion is the current tunnel protocol version.
const FrameVersion = 1

// Frame represents a single tunnel message between gateway and device.
type Frame struct {
	Version int             `json:"v"`
	Type    FrameType       `json:"type"`
	MsgID   string          `json:"msg_id"`
	AckID   *string         `json:"ack_id,omitempty"`
	TS      int64           `json:"ts"`
	Payload json.RawMessage `json:"payload"`
}

// FrameType enumerates all tunnel message types.
type FrameType string

const (
	// Control
	FrameHello     FrameType = "HELLO"
	FrameHelloAck  FrameType = "HELLO_ACK"
	FramePing      FrameType = "PING"
	FramePong      FrameType = "PONG"
	FrameAck       FrameType = "ACK"
	FrameError     FrameType = "ERROR"
	FrameStateSync FrameType = "STATE_SYNC"

	// Job control
	FrameJobDispatch FrameType = "JOB_DISPATCH"
	FrameJobCancel   FrameType = "JOB_CANCEL"
	FrameJobQuery    FrameType = "JOB_QUERY"

	// Job events
	FrameJobAccepted FrameType = "JOB_ACCEPTED"
	FrameJobProgress FrameType = "JOB_PROGRESS"
	FrameJobResult   FrameType = "JOB_RESULT"
	FrameJobRejected FrameType = "JOB_REJECTED"

	// Agent management
	FrameAgentCreate    FrameType = "AGENT_CREATE"
	FrameAgentAction    FrameType = "AGENT_ACTION"
	FrameAgentStatusReq FrameType = "AGENT_STATUS_REQ"
	FrameAgentStatus    FrameType = "AGENT_STATUS"

	// Skill dispatch
	FrameSkillDispatchBegin FrameType = "SKILL_DISPATCH_BEGIN"
	FrameSkillChunk         FrameType = "SKILL_CHUNK"
	FrameSkillDispatchEnd   FrameType = "SKILL_DISPATCH_END"
	FrameSkillAction        FrameType = "SKILL_ACTION"
	FrameSkillState         FrameType = "SKILL_STATE"
	FrameSkillSync          FrameType = "SKILL_SYNC"

	// File transfer
	FrameFilePushBegin FrameType = "FILE_PUSH_BEGIN"
	FrameFileChunk     FrameType = "FILE_CHUNK"
	FrameFilePushEnd   FrameType = "FILE_PUSH_END"
	FrameFileAck       FrameType = "FILE_ACK"
)

// FrameMaxSize is the maximum allowed frame size in bytes.
const FrameMaxSize = 1 << 20 // 1 MiB

// ---------- Hello Payload ----------

// HelloPayload is the payload for a HELLO frame sent by a device on connect.
type HelloPayload struct {
	DeviceID    UUID     `json:"device_id"`
	AgentCount  int      `json:"agent_count"`
	Agents      []HelloAgent `json:"agents"`
	Capabilities []string `json:"capabilities,omitempty"`
	Resources   HelloResources `json:"resources,omitempty"`
}

// HelloAgent describes an agent known to the device on connect.
type HelloAgent struct {
	AgentID     UUID        `json:"agent_id"`
	ContainerID string      `json:"container_id,omitempty"`
	Status      AgentStatus `json:"status"`
}

// HelloResources describes device resource info on connect.
type HelloResources struct {
	CPUCount   int   `json:"cpu_count"`
	MemoryMB   int64 `json:"memory_mb"`
	DiskMB     int64 `json:"disk_mb"`
}

// HelloAckPayload is the payload for a HELLO_ACK frame from the gateway.
type HelloAckPayload struct {
	SessionID string `json:"session_id"`
}

// ---------- Job Frame Payloads ----------

// JobDispatchPayload is the payload for JOB_DISPATCH frame.
type JobDispatchPayload struct {
	JobID    UUID   `json:"job_id"`
	UserID   UUID   `json:"user_id"`
	AgentID  UUID   `json:"agent_id"`
	Command  string `json:"command"`
	SkillID  *UUID  `json:"skill_id,omitempty"`
	FileIDs  []UUID `json:"file_ids,omitempty"`
}

// JobProgressPayload is the payload for JOB_PROGRESS frame.
type JobProgressPayload struct {
	JobID          UUID   `json:"job_id"`
	Status         JobStatus `json:"status"`
	Percent        int    `json:"percent"`
	Message        string `json:"message"`
}

// JobResultPayload is the payload for JOB_RESULT frame.
type JobResultPayload struct {
	JobID    UUID      `json:"job_id"`
	Status   JobStatus `json:"status"`
	Result   *string   `json:"result,omitempty"`
	ErrorMsg *string   `json:"error_msg,omitempty"`
}

// JobRejectedPayload is the payload for JOB_REJECTED frame.
type JobRejectedPayload struct {
	JobID  UUID   `json:"job_id"`
	Reason string `json:"reason"`
}

// ---------- Agent Frame Payloads ----------

// AgentCreatePayload is the payload for AGENT_CREATE frame.
type AgentCreatePayload struct {
	AgentID   UUID   `json:"agent_id"`
	AgentName string `json:"agent_name"`
}

// AgentStatusPayload is the payload for AGENT_STATUS frame.
type AgentStatusPayload struct {
	AgentID     UUID        `json:"agent_id"`
	Status      AgentStatus `json:"status"`
	ContainerID *string     `json:"container_id,omitempty"`
	CPUPercent  *float64    `json:"cpu_percent,omitempty"`
	MemoryMB    *int64      `json:"memory_mb,omitempty"`
}

// ---------- Skill Frame Payloads ----------

// SkillDispatchBeginPayload is the payload for SKILL_DISPATCH_BEGIN frame.
type SkillDispatchBeginPayload struct {
	SkillID        UUID   `json:"skill_id"`
	SkillVersionID UUID   `json:"skill_version_id"`
	Version        string `json:"version"`
	TotalChunks    int    `json:"total_chunks"`
	TotalBytes     int64  `json:"total_bytes"`
	SHA256         string `json:"sha256"`
}

// SkillChunkPayload is the payload for SKILL_CHUNK frame.
type SkillChunkPayload struct {
	SkillID        UUID   `json:"skill_id"`
	SkillVersionID UUID   `json:"skill_version_id"`
	ChunkIndex     int    `json:"chunk_index"`
	Data           string `json:"data"` // base64-encoded
}

// SkillDispatchEndPayload is the payload for SKILL_DISPATCH_END frame.
type SkillDispatchEndPayload struct {
	SkillID        UUID `json:"skill_id"`
	SkillVersionID UUID `json:"skill_version_id"`
}

// SkillActionPayload is the payload for SKILL_ACTION frame.
type SkillActionPayload struct {
	Scope    SkillActionScope `json:"scope"`
	Action   SkillAction      `json:"action"`
	SkillID  UUID             `json:"skill_id"`
	Version  *string          `json:"version,omitempty"`
	AgentID  *UUID            `json:"agent_id,omitempty"`
}

// SkillStatePayload is the payload for SKILL_STATE frame from device.
type SkillStatePayload struct {
	SkillID        UUID        `json:"skill_id"`
	SkillVersionID UUID        `json:"skill_version_id"`
	Scope          SkillActionScope `json:"scope"`
	Status         SkillStatus `json:"status"`
	AgentID        *UUID       `json:"agent_id,omitempty"`
	Error          *string     `json:"error,omitempty"`
}

// SkillSyncPayload is the payload for SKILL_SYNC frame from gateway.
type SkillSyncPayload struct {
	DeviceSkills []DeviceSkill `json:"device_skills"`
	AgentSkills  []AgentSkill  `json:"agent_skills"`
}

// ---------- File Frame Payloads ----------

// FilePushBeginPayload is the payload for FILE_PUSH_BEGIN frame.
type FilePushBeginPayload struct {
	FileID      UUID   `json:"file_id"`
	FileName    string `json:"file_name"`
	SizeBytes   int64  `json:"size_bytes"`
	TotalChunks int    `json:"total_chunks"`
	SHA256      string `json:"sha256"`
}

// FileChunkPayload is the payload for FILE_CHUNK frame.
type FileChunkPayload struct {
	FileID     UUID   `json:"file_id"`
	ChunkIndex int    `json:"chunk_index"`
	Data       string `json:"data"` // base64-encoded, max 256KiB raw
}

// FilePushEndPayload is the payload for FILE_PUSH_END frame.
type FilePushEndPayload struct {
	FileID UUID `json:"file_id"`
}

// FileAckPayload is the payload for FILE_ACK frame from device.
type FileAckPayload struct {
	FileID UUID       `json:"file_id"`
	Status FileStatus `json:"status"`
	Error  *string    `json:"error,omitempty"`
}

// ---------- State Sync Payload ----------

// StateSyncPayload is the payload for STATE_SYNC frame from device.
type StateSyncPayload struct {
	Jobs     []StateSyncJob   `json:"jobs"`
	Agents   []StateSyncAgent `json:"agents"`
}

// StateSyncJob describes an in-flight job from the device's perspective.
type StateSyncJob struct {
	JobID    UUID      `json:"job_id"`
	AgentID  UUID      `json:"agent_id"`
	Status   JobStatus `json:"status"`
}

// StateSyncAgent describes an agent from the device's perspective.
type StateSyncAgent struct {
	AgentID     UUID        `json:"agent_id"`
	ContainerID string      `json:"container_id"`
	Status      AgentStatus `json:"status"`
	JobID       *UUID       `json:"job_id,omitempty"`
}

// ---------- WebSocket Event Types ----------

// WSEvent represents a real-time event pushed to web WS clients.
type WSEvent struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

const (
	WSEventJobProgress   = "job.progress"
	WSEventJobStatus     = "job.status"
	WSEventJobResult     = "job.result"
	WSEventAgentStatus   = "agent.status"
	WSEventDeviceStatus  = "device.status"
	WSEventSkillRollout  = "skill.rollout"
)

// ---------- Skill Vault Types ----------

// SkillWithLatestVersion combines skill metadata with its latest version.
type SkillWithLatestVersion struct {
	Skill
	LatestVersion *SkillVersion `json:"latest_version,omitempty"`
}

// SkillVisible represents a skill visible to a customer, including enable state.
type SkillVisible struct {
	Skill
	LatestVersion *SkillVersion `json:"latest_version,omitempty"`
	Enabled       *bool         `json:"enabled,omitempty"`
}

// EnableSkillRequest is the payload for enabling a skill on an agent.
type EnableSkillRequest struct {
	SkillID UUID `json:"skill_id" validate:"required"`
}

// ---------- Agent Pool Types ----------

// PoolAgentInfo represents an agent in the pool view (admin).
type PoolAgentInfo struct {
	Agent
	DeviceName string `json:"device_name"`
}

// DrainAgentResponse is returned after draining an agent.
type DrainAgentResponse struct {
	AgentID UUID        `json:"agent_id"`
	Status  AgentStatus `json:"status"`
}

// ReleaseAgentRequest is the payload for force-releasing an agent.
type ReleaseAgentRequest struct {
	Reason string `json:"reason"`
}

// ---------- Utility ----------
