package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/store"
)

// Integration tests — requires a running PostgreSQL.
// Set ONE_CLICK_DSN to override the default DSN.
// Run: go test -tags=integration ./internal/store/

func dsn() string {
	if d := os.Getenv("ONE_CLICK_DSN"); d != "" {
		return d
	}
	return "postgresql://siyidong@localhost:5432/oneclickagent?sslmode=disable"
}

func setupDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	db, err := store.NewDB(ctx, dsn())
	if err != nil {
		t.Fatalf("connect to database: %v (hint: set ONE_CLICK_DSN env var)", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ─── Users ────────────────────────────────────────────────────

func TestUserCRUD(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)

	email := "test-crud-" + time.Now().Format("150405.000000") + "@example.com"
	user := &model.User{
		Email:        email,
		Username:     "test-crud-" + time.Now().Format("150405.000000"),
		PasswordHash: "hashed-password",
		Role:         model.RoleUser,
		Tier:         model.TierFree,
		Status:       model.UserActive,
	}

	// Create
	if err := us.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if user.ID.String() == "" {
		t.Fatal("user ID should be set after create")
	}

	// GetByID
	fetched, err := us.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user by ID: %v", err)
	}
	if fetched == nil {
		t.Fatal("user not found after create")
	}
	if fetched.Email != user.Email {
		t.Errorf("email = %s, want %s", fetched.Email, user.Email)
	}
	if fetched.Username != user.Username {
		t.Errorf("username = %s, want %s", fetched.Username, user.Username)
	}
	if fetched.Role != model.RoleUser {
		t.Errorf("role = %s, want user", fetched.Role)
	}
	if fetched.Tier != model.TierFree {
		t.Errorf("tier = %s, want free", fetched.Tier)
	}

	// GetByEmail
	fetchedByEmail, err := us.GetByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("get user by email: %v", err)
	}
	if fetchedByEmail == nil || fetchedByEmail.ID != user.ID {
		t.Fatal("user not found by email")
	}

	// Update
	newUsername := "updated-" + time.Now().Format("150405.000000")
	fetched.Username = newUsername
	if err := us.Update(ctx, fetched); err != nil {
		t.Fatalf("update user: %v", err)
	}
	updated, _ := us.GetByID(ctx, user.ID)
	if updated.Username != newUsername {
		t.Errorf("username = %s after update, want %s", updated.Username, newUsername)
	}

	// UpdateTier
	if err := us.UpdateTier(ctx, user.ID, model.TierPro); err != nil {
		t.Fatalf("update tier: %v", err)
	}
	tierUser, _ := us.GetByID(ctx, user.ID)
	if tierUser.Tier != model.TierPro {
		t.Errorf("tier = %s, want pro", tierUser.Tier)
	}

	// List
	list, _, err := us.List(ctx, nil, 10)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("list should return at least one user")
	}
}

func TestUserNotFound(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)

	u, err := us.GetByID(ctx, model.NewUUID())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != nil {
		t.Error("expected nil for non-existent user")
	}

	u, err = us.GetByEmail(ctx, "nonexistent@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != nil {
		t.Error("expected nil for non-existent email")
	}
}

func TestUserDuplicateEmail(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)

	email := "test-dup-" + time.Now().Format("150405") + "@example.com"

	u1 := &model.User{
		Email:        email,
		Username:     "dup-user-1-" + time.Now().Format("150405"),
		PasswordHash: "hash",
		Role:         model.RoleUser,
		Tier:         model.TierFree,
	}
	if err := us.Create(ctx, u1); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	u2 := &model.User{
		Email:        email,
		Username:     "dup-user-2-" + time.Now().Format("150405"),
		PasswordHash: "hash",
		Role:         model.RoleUser,
		Tier:         model.TierFree,
	}
	err := us.Create(ctx, u2)
	if err == nil {
		t.Error("expected error for duplicate email")
	}
}

// ─── Organizations ────────────────────────────────────────────

func TestOrgCRUD(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)
	os := store.NewOrgStore(db)

	// Need a user first (created_by FK)
	admin := &model.User{
		Email:    "org-admin-" + time.Now().Format("150405") + "@example.com",
		Username: "org-admin-" + time.Now().Format("150405"),
		Role:     model.RoleAdmin,
		Tier:     model.TierEnterprise,
	}
	us.Create(ctx, admin)

	org := &model.Organization{
		Name:        "Test Org " + time.Now().Format("150405"),
		Description: "test org",
		CreatedBy:   admin.ID,
	}

	if err := os.Create(ctx, org); err != nil {
		t.Fatalf("create org: %v", err)
	}

	fetched, err := os.GetByID(ctx, org.ID)
	if err != nil || fetched == nil {
		t.Fatal("org not found after create")
	}
	if fetched.Name != org.Name {
		t.Errorf("name = %s, want %s", fetched.Name, org.Name)
	}

	// Add member
	member := &model.User{
		Email:    "org-member-" + time.Now().Format("150405") + "@example.com",
		Username: "org-member-" + time.Now().Format("150405"),
		Role:     model.RoleUser,
		Tier:     model.TierFree,
	}
	us.Create(ctx, member)

	if err := os.AddMember(ctx, org.ID, member.ID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	members, err := os.ListMembers(ctx, org.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	found := false
	for _, m := range members {
		if m.ID == member.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("member not found in org member list")
	}

	// Remove member
	if err := os.RemoveMember(ctx, member.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	membersAfter, _ := os.ListMembers(ctx, org.ID)
	for _, m := range membersAfter {
		if m.ID == member.ID {
			t.Error("member should be removed")
		}
	}

	// Delete org
	if err := os.Delete(ctx, org.ID); err != nil {
		t.Fatalf("delete org: %v", err)
	}
	deleted, _ := os.GetByID(ctx, org.ID)
	if deleted != nil {
		t.Error("org should be deleted")
	}
}

// ─── Devices ──────────────────────────────────────────────────

func TestDeviceCRUD(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)
	ds := store.NewDeviceStore(db)

	admin := &model.User{
		Email:    "dev-admin-" + time.Now().Format("150405") + "@example.com",
		Username: "dev-admin-" + time.Now().Format("150405"),
		Role:     model.RoleAdmin,
		Tier:     model.TierEnterprise,
	}
	us.Create(ctx, admin)

	device := &model.Device{
		OperatorID:  admin.ID,
		Name:        "Test Device " + time.Now().Format("150405"),
		Description: "test device",
		Platform:    "macos",
		Status:      model.DeviceEnrolled,
		TokenHash:   "test-token-hash",
	}

	if err := ds.Create(ctx, device); err != nil {
		t.Fatalf("create device: %v", err)
	}

	fetched, err := ds.GetByID(ctx, device.ID)
	if err != nil || fetched == nil {
		t.Fatal("device not found")
	}
	if fetched.Name != device.Name {
		t.Errorf("name = %s, want %s", fetched.Name, device.Name)
	}
	if fetched.Platform != "macos" {
		t.Errorf("platform = %s, want macos", fetched.Platform)
	}

	// Update status
	if err := ds.UpdateStatus(ctx, device.ID, model.DeviceOnline); err != nil {
		t.Fatalf("update status: %v", err)
	}
	online, _ := ds.GetByID(ctx, device.ID)
	if online.Status != model.DeviceOnline {
		t.Errorf("status = %s, want online", online.Status)
	}

	// Update token
	if err := ds.UpdateToken(ctx, device.ID, "new-token-hash"); err != nil {
		t.Fatalf("update token: %v", err)
	}

	// List
	list, _, err := ds.List(ctx, nil, 10)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("list should return at least one device")
	}
}

// ─── Agents ───────────────────────────────────────────────────

func TestAgentPoolLifecycle(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)
	ds := store.NewDeviceStore(db)
	as := store.NewAgentStore(db)

	admin := &model.User{
		Email:    "pool-admin-" + time.Now().Format("150405") + "@example.com",
		Username: "pool-admin-" + time.Now().Format("150405"),
		Role:     model.RoleAdmin,
		Tier:     model.TierEnterprise,
	}
	us.Create(ctx, admin)

	device := &model.Device{
		OperatorID: admin.ID,
		Name:       "Pool Device " + time.Now().Format("150405"),
		TokenHash:  "pool-token",
		Status:     model.DeviceOnline,
	}
	ds.Create(ctx, device)

	// Create idle agent with unique port
	agentPort := 42001 + time.Now().Second() + time.Now().Nanosecond()%100
	agent := &model.Agent{
		DeviceID:    device.ID,
		Name:        "test-agent-" + time.Now().Format("150405.000000"),
		Image:       "iagent/agent:latest",
		Port:        agentPort,
		Status:      model.AgentIdle,
		Description: "test agent",
	}
	if err := as.Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Find idle — should find OUR agent (we know its ID and can verify)
	createdAgent, err := as.GetByID(ctx, agent.ID)
	if err != nil || createdAgent == nil {
		t.Fatal("agent not found after create")
	}
	if createdAgent.Status != model.AgentIdle {
		t.Errorf("status = %s, want idle", createdAgent.Status)
	}

	// Allocate with real user
	userID := admin.ID  // use real user ID
	jobID := model.NewUUID()
	if err := as.Allocate(ctx, agent.ID, userID, jobID); err != nil {
		t.Fatalf("allocate: %v", err)
	}

	// FindIdle should still work (there may be stale agents from other runs)
	idleAgain, _ := as.FindIdle(ctx)
	t.Logf("idle agents after alloc: %v", idleAgain != nil)

	// Release
	if err := as.Release(ctx, agent.ID); err != nil {
		t.Fatalf("release: %v", err)
	}
	released, _ := as.GetByID(ctx, agent.ID)
	if released.Status != model.AgentIdle {
		t.Errorf("status = %s, want idle after release", released.Status)
	}
	if released.UserID != nil {
		t.Error("user_id should be nil after release")
	}
	if released.JobID != nil {
		t.Error("job_id should be nil after release")
	}

	// Idle count (at least our agent should be idle)
	count, err := as.IdleCount(ctx)
	if err != nil {
		t.Fatalf("idle count: %v", err)
	}
	t.Logf("idle agent count: %d", count)
	if count < 1 {
		t.Errorf("idle count = %d, want >= 1", count)
	}
}

// ─── Jobs ─────────────────────────────────────────────────────

func TestJobLifecycle(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)
	ds := store.NewDeviceStore(db)
	as := store.NewAgentStore(db)
	js := store.NewJobStore(db)

	// Setup: user + device + agent
	admin := &model.User{
		Email:    "job-user-" + time.Now().Format("150405") + "@example.com",
		Username: "job-user-" + time.Now().Format("150405"),
		Role:     model.RoleUser,
		Tier:     model.TierEnterprise,
	}
	us.Create(ctx, admin)

	device := &model.Device{
		OperatorID: admin.ID,
		Name:       "Job Device " + time.Now().Format("150405"),
		TokenHash:  "job-token",
	}
	ds.Create(ctx, device)

	// Create job
	job := &model.Job{
		UserID:   admin.ID,
		UserTier: admin.Tier,
		Command:  "echo hello world",
		Channel:  "web",
		Status:   model.JobPending,
	}
	if err := js.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	// Get job
	fetched, err := js.GetByID(ctx, job.ID)
	if err != nil || fetched == nil {
		t.Fatal("job not found after create")
	}
	if fetched.Status != model.JobPending {
		t.Errorf("status = %s, want pending", fetched.Status)
	}
	if fetched.Command != "echo hello world" {
		t.Errorf("command = %s", fetched.Command)
	}

	// Update status
	if err := js.UpdateStatus(ctx, job.ID, model.JobRunning); err != nil {
		t.Fatalf("update status: %v", err)
	}
	running, _ := js.GetByID(ctx, job.ID)
	if running.Status != model.JobRunning {
		t.Errorf("status = %s, want running", running.Status)
	}

	// Update progress
	if err := js.UpdateProgress(ctx, job.ID, 50, "halfway done", model.JobRunning); err != nil {
		t.Fatalf("update progress: %v", err)
	}
	progress, _ := js.GetByID(ctx, job.ID)
	if progress.Percent == nil || *progress.Percent != 50 {
		t.Errorf("percent = %v, want 50", progress.Percent)
	}
	if progress.ProgressMessage == nil || *progress.ProgressMessage != "halfway done" {
		t.Error("progress message mismatch")
	}

	// Set agent
	agent := &model.Agent{
		DeviceID: device.ID,
		Name:     "job-agent-" + time.Now().Format("150405"),
		Image:    "iagent/agent:latest",
		Port:     42999,
		Status:   model.AgentBusy,
	}
	as.Create(ctx, agent)

	if err := js.SetAgent(ctx, job.ID, agent.ID, device.ID); err != nil {
		t.Fatalf("set agent: %v", err)
	}
	withAgent, _ := js.GetByID(ctx, job.ID)
	if withAgent.Status != model.JobDispatched {
		t.Errorf("status = %s, want dispatched", withAgent.Status)
	}

	// Update result (jsonb column — use json.RawMessage)
	result := json.RawMessage(`"hello world"`)
	if err := js.UpdateResult(ctx, job.ID, model.JobSucceeded, &result); err != nil {
		t.Fatalf("update result: %v", err)
	}
	done, _ := js.GetByID(ctx, job.ID)
	if done.Status != model.JobSucceeded {
		t.Errorf("status = %s, want succeeded", done.Status)
	}
	if done.Result == nil || string(*done.Result) != string(result) {
		t.Error("result mismatch")
	}

	// List by user
	jobs, _, err := js.ListByUser(ctx, admin.ID, nil, 10)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) == 0 {
		t.Fatal("list should return at least one job")
	}
}

// ─── Job Queue ────────────────────────────────────────────────

func TestJobQueue(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)
	js := store.NewJobStore(db)

	// Create users with different tiers
	enterpriseUser := &model.User{
		Email:    "queue-ent-" + time.Now().Format("150405") + "@example.com",
		Username: "queue-ent-" + time.Now().Format("150405"),
		Role:     model.RoleUser,
		Tier:     model.TierEnterprise,
	}
	us.Create(ctx, enterpriseUser)

	freeUser := &model.User{
		Email:    "queue-free-" + time.Now().Format("150405") + "@example.com",
		Username: "queue-free-" + time.Now().Format("150405"),
		Role:     model.RoleUser,
		Tier:     model.TierFree,
	}
	us.Create(ctx, freeUser)

	now := time.Now().UTC()
	expires := now.Add(1 * time.Hour)

	// Create queued job for free user (created first)
	freeJob := &model.Job{
		UserID:         freeUser.ID,
		UserTier:       freeUser.Tier,
		Command:        "free job",
		Status:         model.JobQueued,
		QueuedAt:       &now,
		QueueExpiresAt: &expires,
	}
	js.Create(ctx, freeJob)

	// Create queued job for enterprise user (created second, but higher tier)
	entJob := &model.Job{
		UserID:         enterpriseUser.ID,
		UserTier:       enterpriseUser.Tier,
		Command:        "enterprise job",
		Status:         model.JobQueued,
		QueuedAt:       &now,
		QueueExpiresAt: &expires,
	}
	js.Create(ctx, entJob)

	// DequeueNext should return enterprise job first (higher tier)
	next, err := js.DequeueNext(ctx)
	if err != nil {
		t.Fatalf("dequeue next: %v", err)
	}
	if next == nil {
		t.Fatal("expected a queued job")
	}
	if next.UserTier != model.TierEnterprise {
		t.Errorf("dequeued tier = %s, want enterprise (higher priority)", next.UserTier)
	}

	// CountQueuedByUser
	freeCount, err := js.CountQueuedByUser(ctx, freeUser.ID)
	if err != nil {
		t.Fatalf("count queued: %v", err)
	}
	if freeCount != 1 {
		t.Errorf("free user queued count = %d, want 1", freeCount)
	}

	// Expire queued
	expiredCount, err := js.ExpireQueued(ctx)
	if err != nil {
		t.Fatalf("expire queued: %v", err)
	}
	// freeJob should still be there (not expired)
	t.Logf("expired %d queued jobs", expiredCount)

	// Cancel job
	if err := js.Cancel(ctx, freeJob.ID, freeUser.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	cancelled, _ := js.GetByID(ctx, freeJob.ID)
	if cancelled.Status != model.JobCancelled {
		t.Errorf("status = %s, want cancelled", cancelled.Status)
	}
}

// ─── Files ────────────────────────────────────────────────────

func TestFileLifecycle(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)
	fs := store.NewFileStore(db)

	user := &model.User{
		Email:    "file-user-" + time.Now().Format("150405") + "@example.com",
		Username: "file-user-" + time.Now().Format("150405"),
		Role:     model.RoleUser,
		Tier:     model.TierFree,
	}
	us.Create(ctx, user)

	file := &model.File{
		UserID:     user.ID,
		Name:       "test-file.txt",
		Size:       1024,
		Mime:       "text/plain",
		SHA256:     "abc123def456",
		StorageURI: "/tmp/test-file.txt",
		Status:     model.FileStagedCloud,
	}

	if err := fs.Create(ctx, file); err != nil {
		t.Fatalf("create file: %v", err)
	}

	fetched, err := fs.GetByID(ctx, file.ID)
	if err != nil || fetched == nil {
		t.Fatal("file not found")
	}
	if fetched.Name != "test-file.txt" {
		t.Errorf("name = %s", fetched.Name)
	}

	// Update status
	if err := fs.UpdateStatus(ctx, file.ID, model.FileStagedDevice); err != nil {
		t.Fatalf("update status: %v", err)
	}
	updated, _ := fs.GetByID(ctx, file.ID)
	if updated.Status != model.FileStagedDevice {
		t.Errorf("status = %s, want staged_device", updated.Status)
	}

	// Mark purged
	if err := fs.MarkPurged(ctx, file.ID); err != nil {
		t.Fatalf("mark purged: %v", err)
	}
	purged, _ := fs.GetByID(ctx, file.ID)
	if purged.Status != model.FilePurged {
		t.Errorf("status = %s, want purged", purged.Status)
	}

	// ListByUser
	files, _, err := fs.ListByUser(ctx, user.ID, nil, 10)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	// Purged files are excluded
	if len(files) != 0 {
		t.Errorf("expected 0 files after purge, got %d", len(files))
	}

	// LinkToJob + ListByJob
	js := store.NewJobStore(db)
	job := &model.Job{
		UserID:   user.ID,
		UserTier: user.Tier,
		Command:  "test",
	}
	js.Create(ctx, job)

	file2 := &model.File{
		UserID:     user.ID,
		Name:       "linked-file.txt",
		Size:       512,
		SHA256:     "xyz789",
		StorageURI: "/tmp/linked.txt",
		Status:     model.FileStagedCloud,
	}
	fs.Create(ctx, file2)

	if err := fs.LinkToJob(ctx, file2.ID, job.ID); err != nil {
		t.Fatalf("link file: %v", err)
	}
	linked, err := fs.ListByJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("list by job: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != file2.ID {
		t.Errorf("expected 1 linked file, got %d", len(linked))
	}
}

// ─── Skills ───────────────────────────────────────────────────

func TestSkillVaultCRUD(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)
	sk := store.NewSkillStore(db)

	admin := &model.User{
		Email:    "skill-admin-" + time.Now().Format("150405") + "@example.com",
		Username: "skill-admin-" + time.Now().Format("150405"),
		Role:     model.RoleAdmin,
		Tier:     model.TierEnterprise,
	}
	us.Create(ctx, admin)

	// Create skill
	skill := &model.Skill{
		Key:         "test-skill-" + time.Now().Format("150405"),
		Name:        "Test Skill",
		Description: "a test skill",
		Visibility:  model.VisibilityPublic,
		Status:      model.SkillActive,
	}
	if err := sk.CreateSkill(ctx, skill); err != nil {
		t.Fatalf("create skill: %v", err)
	}

	// Get
	fetched, err := sk.GetSkill(ctx, skill.ID)
	if err != nil || fetched == nil {
		t.Fatal("skill not found")
	}
	if fetched.Key != skill.Key {
		t.Errorf("key = %s, want %s", fetched.Key, skill.Key)
	}

	// Create version
	ver := &model.SkillVersion{
		SkillID:     skill.ID,
		Version:     "1.0.0",
		Manifest:    json.RawMessage(`{"entrypoint":"main.py"}`),
		ArtifactURI: "/tmp/test-skill.tar.gz",
		SHA256:      "sha256-test",
	}
	if err := sk.CreateVersion(ctx, ver); err != nil {
		t.Fatalf("create version: %v", err)
	}

	// Get version
	fetchedVer, err := sk.GetVersion(ctx, ver.ID)
	if err != nil || fetchedVer == nil {
		t.Fatal("version not found")
	}
	if fetchedVer.Version != "1.0.0" {
		t.Errorf("version = %s, want 1.0.0", fetchedVer.Version)
	}

	// Get latest version
	latest, err := sk.GetLatestVersion(ctx, skill.ID)
	if err != nil || latest == nil {
		t.Fatal("latest version not found")
	}
	if latest.Version != "1.0.0" {
		t.Errorf("latest version = %s", latest.Version)
	}

	// Get by tag
	tagged, err := sk.GetVersionByTag(ctx, skill.ID, "1.0.0")
	if err != nil || tagged == nil {
		t.Fatal("version not found by tag")
	}
	if tagged.ID != ver.ID {
		t.Error("version ID mismatch")
	}

	// List versions
	versions, err := sk.ListVersions(ctx, skill.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("expected 1 version, got %d", len(versions))
	}

	// Update visibility
	if err := sk.UpdateVisibility(ctx, skill.ID, model.VisibilityRestricted); err != nil {
		t.Fatalf("update visibility: %v", err)
	}
	restricted, _ := sk.GetSkill(ctx, skill.ID)
	if restricted.Visibility != model.VisibilityRestricted {
		t.Errorf("visibility = %s, want restricted", restricted.Visibility)
	}

	// Grant visibility
	grant := &model.SkillGrant{
		SkillID:       skill.ID,
		PrincipalType: model.PrincipalUser,
		PrincipalID:   admin.ID,
		GrantedBy:     admin.ID,
	}
	if err := sk.CreateGrant(ctx, grant); err != nil {
		t.Fatalf("create grant: %v", err)
	}

	// Check visibility
	visible, err := sk.IsSkillVisibleToUser(ctx, skill.ID, admin.ID, nil)
	if err != nil {
		t.Fatalf("check visibility: %v", err)
	}
	if !visible {
		t.Error("skill should be visible to granted user (even though restricted)")
	}

	// List grants
	grants, err := sk.ListGrants(ctx, skill.ID)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 {
		t.Errorf("expected 1 grant, got %d", len(grants))
	}

	// Delete grant
	if err := sk.DeleteGrant(ctx, skill.ID, model.PrincipalUser, admin.ID); err != nil {
		t.Fatalf("delete grant: %v", err)
	}

	// List visible skills
	visibleSkills, err := sk.ListVisibleSkills(ctx, admin.ID, nil)
	if err != nil {
		t.Fatalf("list visible skills: %v", err)
	}
	// Skill is restricted now and grant was deleted, so it may not be visible
	t.Logf("visible skills after restrict + grant delete: %d", len(visibleSkills))

	// List skills
	allSkills, _, err := sk.ListSkills(ctx, nil, 10)
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(allSkills) == 0 {
		t.Fatal("list should return at least one skill")
	}

	// Delete skill
	if err := sk.DeleteSkill(ctx, skill.ID); err != nil {
		t.Fatalf("delete skill: %v", err)
	}
	deleted, _ := sk.GetSkill(ctx, skill.ID)
	if deleted != nil {
		t.Error("skill should be deleted")
	}
}

// ─── Device + Agent Skills ────────────────────────────────────

func TestDeviceAndAgentSkills(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	us := store.NewUserStore(db)
	ds := store.NewDeviceStore(db)
	as := store.NewAgentStore(db)
	sk := store.NewSkillStore(db)

	admin := &model.User{
		Email:    "daskill-admin-" + time.Now().Format("150405") + "@example.com",
		Username: "daskill-admin-" + time.Now().Format("150405"),
		Role:     model.RoleAdmin,
		Tier:     model.TierEnterprise,
	}
	us.Create(ctx, admin)

	device := &model.Device{
		OperatorID: admin.ID,
		Name:       "Skill Device " + time.Now().Format("150405"),
		TokenHash:  "skill-token",
	}
	ds.Create(ctx, device)

	agent := &model.Agent{
		DeviceID: device.ID,
		Name:     "skill-agent-" + time.Now().Format("150405"),
		Image:    "iagent/agent:latest",
		Port:     42010,
		Status:   model.AgentIdle,
	}
	as.Create(ctx, agent)

	skill := &model.Skill{
		Key:         "da-skill-" + time.Now().Format("150405"),
		Name:        "DA Skill",
		Visibility:  model.VisibilityPublic,
		Status:      model.SkillActive,
	}
	sk.CreateSkill(ctx, skill)

	// Set device skill
	dsRec := &model.DeviceSkill{
		DeviceID: device.ID,
		SkillID:  skill.ID,
		Version:  "1.0.0",
		Status:   model.SkillInstalled,
	}
	if err := sk.SetDeviceSkill(ctx, dsRec); err != nil {
		t.Fatalf("set device skill: %v", err)
	}

	// Get device skills
	dSkills, err := sk.GetDeviceSkills(ctx, device.ID)
	if err != nil {
		t.Fatalf("get device skills: %v", err)
	}
	if len(dSkills) != 1 {
		t.Errorf("expected 1 device skill, got %d", len(dSkills))
	}
	if dSkills[0].Status != model.SkillInstalled {
		t.Errorf("device skill status = %s, want installed", dSkills[0].Status)
	}

	// Is installed
	installed, err := sk.IsSkillInstalledOnDevice(ctx, device.ID, skill.ID)
	if err != nil {
		t.Fatalf("is installed: %v", err)
	}
	if !installed {
		t.Error("skill should be marked as installed on device")
	}

	// Set agent skill
	asRec := &model.AgentSkill{
		AgentID: agent.ID,
		SkillID: skill.ID,
		Status:  model.AgentSkillEnabled,
	}
	if err := sk.SetAgentSkill(ctx, asRec); err != nil {
		t.Fatalf("set agent skill: %v", err)
	}

	// Get agent skills
	aSkills, err := sk.GetAgentSkills(ctx, agent.ID)
	if err != nil {
		t.Fatalf("get agent skills: %v", err)
	}
	if len(aSkills) != 1 {
		t.Errorf("expected 1 agent skill, got %d", len(aSkills))
	}
	if aSkills[0].Status != model.AgentSkillEnabled {
		t.Errorf("agent skill status = %s, want enabled", aSkills[0].Status)
	}

	// Delete agent skill
	if err := sk.DeleteAgentSkill(ctx, agent.ID, skill.ID); err != nil {
		t.Fatalf("delete agent skill: %v", err)
	}
	aSkillsAfter, _ := sk.GetAgentSkills(ctx, agent.ID)
	if len(aSkillsAfter) != 0 {
		t.Error("agent skills should be empty after delete")
	}

	// Delete device skill
	if err := sk.DeleteDeviceSkill(ctx, device.ID, skill.ID); err != nil {
		t.Fatalf("delete device skill: %v", err)
	}
	dSkillsAfter, _ := sk.GetDeviceSkills(ctx, device.ID)
	if len(dSkillsAfter) != 0 {
		t.Error("device skills should be empty after delete")
	}
}
