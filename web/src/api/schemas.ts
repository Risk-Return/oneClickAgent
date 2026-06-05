import { z } from "zod";

export const UserTier = z.enum(["free", "pro", "enterprise"]);
export type UserTier = z.infer<typeof UserTier>;

export const UserRole = z.enum(["user", "admin"]);
export type UserRole = z.infer<typeof UserRole>;

export const UserStatus = z.enum(["active", "disabled"]);
export type UserStatus = z.infer<typeof UserStatus>;

export const UserSchema = z.object({
  id: z.string().uuid(),
  email: z.string().email(),
  username: z.string(),
  role: UserRole,
  tier: UserTier,
  status: UserStatus,
  org_id: z.string().uuid().nullable(),
  created_at: z.string().datetime(),
  updated_at: z.string().datetime(),
});
export type User = z.infer<typeof UserSchema>;

export const AuthResponseSchema = z.object({
  user: UserSchema,
  access_token: z.string(),
  refresh_token: z.string(),
  expires_in: z.number(),
});
export type AuthResponse = z.infer<typeof AuthResponseSchema>;

export const LoginRequestSchema = z.object({
  email: z.string().email(),
  password: z.string().min(12),
});
export type LoginRequest = z.infer<typeof LoginRequestSchema>;

export const RegisterRequestSchema = z.object({
  email: z.string().email(),
  username: z.string().min(3).max(32),
  password: z.string().min(12),
});
export type RegisterRequest = z.infer<typeof RegisterRequestSchema>;

export const RefreshRequestSchema = z.object({
  refresh: z.string(),
});
export type RefreshRequest = z.infer<typeof RefreshRequestSchema>;

export const DeviceStatus = z.enum(["enrolled", "online", "offline"]);
export type DeviceStatus = z.infer<typeof DeviceStatus>;

export const DeviceSchema = z.object({
  id: z.string().uuid(),
  operator_id: z.string().uuid(),
  name: z.string(),
  description: z.string().nullable(),
  platform: z.string().nullable(),
  status: DeviceStatus,
  last_seen_at: z.string().datetime().nullable(),
  resources: z.record(z.unknown()).nullable(),
  created_at: z.string().datetime(),
  updated_at: z.string().datetime(),
});
export type Device = z.infer<typeof DeviceSchema>;

export const AgentStatus = z.enum(["creating", "idle", "busy", "unhealthy", "failed", "removed"]);
export type AgentStatus = z.infer<typeof AgentStatus>;

export const AgentLimits = z.object({
  cpu: z.number(),
  mem_mb: z.number(),
  disk_mb: z.number(),
});

export const AgentSchema = z.object({
  id: z.string().uuid(),
  device_id: z.string().uuid(),
  user_id: z.string().uuid().nullable(),
  name: z.string(),
  description: z.string().nullable(),
  image: z.string(),
  port: z.number(),
  tags: z.array(z.string()),
  status: AgentStatus,
  job_id: z.string().uuid().nullable(),
  limits: AgentLimits,
  allocated_at: z.string().datetime().nullable(),
  created_at: z.string().datetime(),
  updated_at: z.string().datetime(),
});
export type Agent = z.infer<typeof AgentSchema>;

export const JobStatus = z.enum(["pending", "queued", "dispatched", "running", "succeeded", "failed", "cancelled"]);
export type JobStatus = z.infer<typeof JobStatus>;

export const JobSchema = z.object({
  id: z.string().uuid(),
  user_id: z.string().uuid(),
  user_tier: UserTier,
  agent_id: z.string().uuid().nullable(),
  device_id: z.string().uuid().nullable(),
  channel: z.string(),
  command: z.string(),
  params: z.record(z.unknown()).nullable(),
  skill_id: z.string().uuid().nullable(),
  status: JobStatus,
  percent: z.number(),
  progress_message: z.string().nullable(),
  result: z.record(z.unknown()).nullable(),
  error_code: z.string().nullable(),
  error_message: z.string().nullable(),
  queue_position: z.number().nullable(),
  estimated_wait_seconds: z.number().nullable(),
  submitted_at: z.string().datetime().nullable(),
  started_at: z.string().datetime().nullable(),
  finished_at: z.string().datetime().nullable(),
  created_at: z.string().datetime(),
  updated_at: z.string().datetime(),
});
export type Job = z.infer<typeof JobSchema>;

export const SubmitJobRequestSchema = z.object({
  command: z.string().min(1),
  params: z.record(z.unknown()).optional(),
  file_ids: z.array(z.string().uuid()).optional(),
  skill_id: z.string().uuid().optional(),
  credential_ids: z.array(z.string().uuid()).optional(),
});
export type SubmitJobRequest = z.infer<typeof SubmitJobRequestSchema>;

export const FileStatus = z.enum(["staged_cloud", "staged_device", "purged", "error"]);
export type FileStatus = z.infer<typeof FileStatus>;

export const FileSchema = z.object({
  id: z.string().uuid(),
  user_id: z.string().uuid(),
  name: z.string(),
  size: z.number(),
  mime: z.string().nullable(),
  sha256: z.string(),
  status: FileStatus,
  created_at: z.string().datetime(),
  purged_at: z.string().datetime().nullable(),
});
export type FileModel = z.infer<typeof FileSchema>;

export const SkillVisibility = z.enum(["public", "restricted"]);
export type SkillVisibility = z.infer<typeof SkillVisibility>;

export const SkillCatalogStatus = z.enum(["active", "deprecated"]);
export type SkillCatalogStatus = z.infer<typeof SkillCatalogStatus>;

export const SkillSchema = z.object({
  id: z.string().uuid(),
  key: z.string(),
  name: z.string(),
  description: z.string().nullable(),
  visibility: SkillVisibility,
  latest_version: z.string().nullable(),
  status: SkillCatalogStatus,
  created_at: z.string().datetime(),
  updated_at: z.string().datetime(),
});
export type Skill = z.infer<typeof SkillSchema>;

export const SkillVersionSchema = z.object({
  id: z.string().uuid(),
  skill_id: z.string().uuid(),
  version: z.string(),
  manifest: z.record(z.unknown()),
  sha256: z.string(),
  size: z.number().nullable(),
  created_at: z.string().datetime(),
});
export type SkillVersion = z.infer<typeof SkillVersionSchema>;

export const SkillInstallStatus = z.enum(["installing", "installed", "disabled", "updating", "deleting", "error", "pending"]);
export type SkillInstallStatus = z.infer<typeof SkillInstallStatus>;

export const DeviceSkillSchema = z.object({
  device_id: z.string().uuid(),
  skill_id: z.string().uuid(),
  version: z.string(),
  status: SkillInstallStatus,
  error_message: z.string().nullable(),
  updated_at: z.string().datetime(),
});
export type DeviceSkill = z.infer<typeof DeviceSkillSchema>;

export const PrincipalType = z.enum(["user", "org"]);
export type PrincipalType = z.infer<typeof PrincipalType>;

export const SkillGrantSchema = z.object({
  skill_id: z.string().uuid(),
  principal_type: PrincipalType,
  principal_id: z.string().uuid(),
  granted_by: z.string().uuid(),
  created_at: z.string().datetime(),
});
export type SkillGrant = z.infer<typeof SkillGrantSchema>;

export const SkillRolloutAgentEntrySchema = z.object({
  agent_id: z.string().uuid(),
  agent_name: z.string(),
  device_id: z.string().uuid(),
  status: SkillInstallStatus,
  error: z.string().nullable().optional(),
});
export type SkillRolloutAgentEntry = z.infer<typeof SkillRolloutAgentEntrySchema>;

export const SkillRolloutEntrySchema = z.object({
  device_id: z.string().uuid(),
  device_name: z.string(),
  version: z.string().optional(),
  status: SkillInstallStatus,
  error: z.string().nullable().optional(),
  updated_at: z.string().datetime(),
  agents: z.array(SkillRolloutAgentEntrySchema).optional(),
});
export type SkillRolloutEntry = z.infer<typeof SkillRolloutEntrySchema>;

export const OrganizationSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  description: z.string().nullable(),
  created_by: z.string().uuid(),
  created_at: z.string().datetime(),
  updated_at: z.string().datetime(),
});
export type Organization = z.infer<typeof OrganizationSchema>;

export const VNCSessionStatus = z.enum(["pending", "ready", "active", "closed", "error"]);
export type VNCSessionStatus = z.infer<typeof VNCSessionStatus>;

export const VNCOpenResponseSchema = z.object({
  session_id: z.string().uuid(),
  ws_url: z.string(),
  rfb_password: z.string(),
  ttl_s: z.number(),
});
export type VNCOpenResponse = z.infer<typeof VNCOpenResponseSchema>;

export const VNCSessionSchema = z.object({
  session_id: z.string().uuid(),
  status: VNCSessionStatus,
});
export type VNCSession = z.infer<typeof VNCSessionSchema>;

export const BrowserCredentialSchema = z.object({
  id: z.string().uuid(),
  label: z.string(),
  origin: z.string(),
  last_used_at: z.string().datetime().nullable(),
  created_at: z.string().datetime(),
  updated_at: z.string().datetime(),
});
export type BrowserCredential = z.infer<typeof BrowserCredentialSchema>;

export const PaginatedResponseSchema = <T extends z.ZodType>(itemSchema: T) =>
  z.object({
    items: z.array(itemSchema),
    next_cursor: z.string().nullable(),
  });

export const APIErrorSchema = z.object({
  error: z.object({
    code: z.string(),
    message: z.string(),
    request_id: z.string().optional(),
  }),
});
export type APIError = z.infer<typeof APIErrorSchema>;

export const WSMessage = z.discriminatedUnion("type", [
  z.object({ type: z.literal("subscribe"), topics: z.array(z.string()) }),
  z.object({ type: z.literal("unsubscribe"), topics: z.array(z.string()) }),
  z.object({ type: z.literal("ping") }),
]);

export const WSEvent = z.object({
  type: z.string(),
  payload: z.record(z.unknown()),
});
export type WSEvent = z.infer<typeof WSEvent>;
