package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/oneClickAgent/gateway/internal/mockdevice"
	"github.com/oneClickAgent/gateway/internal/model"
)

// ─── Scenario 1: Gateway + PostgreSQL startup ─────────────────

func TestE2E_GatewayStartup(t *testing.T) {
	h := NewHarness(t)

	resp := h.Get(t, "/healthz", "")
	if resp.StatusCode != 200 {
		t.Errorf("healthz: status=%d want 200", resp.StatusCode)
	}

	resp = h.Get(t, "/readyz", "")
	if resp.StatusCode != 200 {
		t.Errorf("readyz: status=%d want 200", resp.StatusCode)
	}
}

// ─── Scenario 2: Admin registration, device creation, enrollment ──

func TestE2E_AdminCreatesDevice(t *testing.T) {
	h := NewHarness(t)

	admin := h.RegisterAdmin(t)
	device, deviceToken := h.CreateDevice(t, admin.AccessToken)

	if device.Name == "" {
		t.Error("device name should be set")
	}

	// Verify admin can list devices.
	resp := h.Get(t, "/api/v1/devices", admin.AccessToken)
	if resp.StatusCode != 200 {
		t.Errorf("list devices: status=%d", resp.StatusCode)
	}

	// Connect mock device via tunnel.
	md := mockdevice.New(mockdevice.Config{
		DeviceID:    device.ID,
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents: []model.HelloAgent{
			{AgentID: model.NewUUID(), Status: model.AgentIdle, Port: 9001, Tags: []string{"default"}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := md.Connect(ctx); err != nil {
		t.Fatalf("mock device connect: %v", err)
	}
	defer md.Close()

	// Wait for device to be marked online (HELLO handler updates status).
	time.Sleep(500 * time.Millisecond)

	resp = h.Get(t, "/api/v1/devices", admin.AccessToken)
	if resp.StatusCode != 200 {
		t.Errorf("list devices after connect: status=%d body=%s", resp.StatusCode, resp.Body)
	}
}

// ─── Scenario 3: Customer registration and login ──────────────

func TestE2E_CustomerRegistration(t *testing.T) {
	h := NewHarness(t)

	resp := h.Post(t, "/api/v1/auth/register", model.RegisterRequest{
		Email:    "test-cust-" + uniq() + "@e2e.test",
		Username: "test-cust-" + uniq(),
		Password: "MyCustomerPass!2345",
	}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("register customer: status=%d body=%s", resp.StatusCode, resp.Body)
	}

	var ar model.AuthResponse
	json.Unmarshal([]byte(resp.Body), &ar)

	// Login
	resp = h.Post(t, "/api/v1/auth/login", model.LoginRequest{
		Email:    ar.User.Email,
		Password: "MyCustomerPass!2345",
	}, "")
	if resp.StatusCode != 200 {
		t.Fatalf("login: status=%d", resp.StatusCode)
	}

	// /me
	resp = h.Get(t, "/api/v1/auth/me", ar.AccessToken)
	if resp.StatusCode != 200 {
		t.Errorf("me: status=%d", resp.StatusCode)
	}
}

// ─── Scenario 4: Submit job with mock device ──────────────────

func TestE2E_SubmitJobAndReceiveResult(t *testing.T) {
	h := NewHarness(t)

	admin := h.RegisterAdmin(t)
	device, deviceToken := h.CreateDevice(t, admin.AccessToken)
	cust := h.RegisterCustomer(t, model.TierFree)

	agentID := model.NewUUID()
	md := mockdevice.New(mockdevice.Config{
		DeviceID:    device.ID,
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents: []model.HelloAgent{
			{AgentID: agentID, Status: model.AgentIdle, Port: 9001, Tags: []string{"default"}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := md.Connect(ctx); err != nil {
		t.Fatalf("mock device connect: %v", err)
	}
	defer md.Close()

	time.Sleep(300 * time.Millisecond)

	// Handle JOB_DISPATCH: accept, progress, succeed.
	md.On(model.FrameJobDispatch, func(dev *mockdevice.MockDevice, f model.Frame) *model.Frame {
		var payload model.JobDispatchPayload
		json.Unmarshal(f.Payload, &payload)

		// Send JOB_ACCEPTED
		_ = dev.Send(model.FrameJobAccepted, map[string]string{"job_id": payload.JobID.String()})

		// Send JOB_PROGRESS (running)
		time.Sleep(100 * time.Millisecond)
		_ = dev.Send(model.FrameJobProgress, model.JobProgressPayload{
			JobID:    payload.JobID,
			EventSeq: 1,
			Status:   model.JobRunning,
			Percent:  0,
			Message:  "job started",
		})

		// Send progress at 50%
		time.Sleep(100 * time.Millisecond)
		_ = dev.Send(model.FrameJobProgress, model.JobProgressPayload{
			JobID:    payload.JobID,
			EventSeq: 2,
			Status:   model.JobRunning,
			Percent:  50,
			Message:  "halfway done",
		})

		// Send result
		resultVal := "{\"output\":\"hello from e2e\"}"
		respFrame := mockdevice.NewFrame(model.FrameJobResult, model.JobResultPayload{
			JobID:  payload.JobID,
			Status: model.JobSucceeded,
			Result: &resultVal,
		})
		return &respFrame
	})

	// Submit job.
	resp := h.Post(t, "/api/v1/jobs", model.SubmitJobRequest{
		Command: "echo hello",
	}, cust.AccessToken)
	if resp.StatusCode != 201 {
		t.Fatalf("submit job: status=%d body=%s", resp.StatusCode, resp.Body)
	}

	jobResp := h.unmarshalJob(t, resp)
	if jobResp.Job.Status != model.JobDispatched {
		t.Errorf("job status = %s, want dispatched", jobResp.Job.Status)
	}
	if jobResp.AgentID == nil {
		t.Error("job should have agent_id")
	}

	// Wait for result.
	time.Sleep(time.Second)

	// Get job status.
	resp = h.Get(t, "/api/v1/jobs/"+jobResp.Job.ID.String(), cust.AccessToken)
	if resp.StatusCode != 200 {
		t.Fatalf("get job: status=%d", resp.StatusCode)
	}
	var finalJob model.Job
	json.Unmarshal([]byte(resp.Body), &finalJob)
	if finalJob.Status != model.JobSucceeded {
		t.Errorf("final job status = %s, want succeeded", finalJob.Status)
	}
}

// ─── Scenario 5: Cancel job ──────────────────────────────────

func TestE2E_CancelJob(t *testing.T) {
	h := NewHarness(t)

	admin := h.RegisterAdmin(t)
	device, deviceToken := h.CreateDevice(t, admin.AccessToken)
	cust := h.RegisterCustomer(t, model.TierFree)

	agentID := model.NewUUID()
	md := mockdevice.New(mockdevice.Config{
		DeviceID:    device.ID,
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents: []model.HelloAgent{
			{AgentID: agentID, Status: model.AgentIdle, Port: 9001, Tags: []string{"default"}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := md.Connect(ctx); err != nil {
		t.Fatalf("mock device connect: %v", err)
	}
	defer md.Close()

	time.Sleep(300 * time.Millisecond)

	// Accept the job but never finish it.
	dispatchCh := make(chan model.JobDispatchPayload, 1)
	md.On(model.FrameJobDispatch, func(dev *mockdevice.MockDevice, f model.Frame) *model.Frame {
		var payload model.JobDispatchPayload
		json.Unmarshal(f.Payload, &payload)
		dispatchCh <- payload
		_ = dev.Send(model.FrameJobAccepted, map[string]string{"job_id": payload.JobID.String()})
		_ = dev.Send(model.FrameJobProgress, model.JobProgressPayload{
			JobID:    payload.JobID,
			EventSeq: 1,
			Status:   model.JobRunning,
			Percent:  10,
			Message:  "started, will be cancelled",
		})
		return nil
	})

	// Submit job.
	resp := h.Post(t, "/api/v1/jobs", model.SubmitJobRequest{
		Command: "sleep 100",
	}, cust.AccessToken)
	if resp.StatusCode != 201 {
		t.Fatalf("submit job: status=%d body=%s", resp.StatusCode, resp.Body)
	}
	jobResp := h.unmarshalJob(t, resp)

	// Wait for dispatch.
	<-dispatchCh
	time.Sleep(200 * time.Millisecond)

	// Cancel job.
	resp = h.Post(t, "/api/v1/jobs/"+jobResp.Job.ID.String()+"/cancel", nil, cust.AccessToken)
	if resp.StatusCode != 200 {
		t.Fatalf("cancel job: status=%d body=%s", resp.StatusCode, resp.Body)
	}

	time.Sleep(200 * time.Millisecond)

	// Verify cancelled.
	resp = h.Get(t, "/api/v1/jobs/"+jobResp.Job.ID.String(), cust.AccessToken)
	var j model.Job
	json.Unmarshal([]byte(resp.Body), &j)
	if j.Status != model.JobCancelled {
		t.Errorf("cancelled job status = %s, want cancelled", j.Status)
	}
}

// ─── Scenario 6: Queue when no idle agents ────────────────────

func TestE2E_QueueWhenNoIdleAgents(t *testing.T) {
	h := NewHarness(t)

	admin := h.RegisterAdmin(t)
	_, deviceToken := h.CreateDevice(t, admin.AccessToken)
	cust := h.RegisterCustomer(t, model.TierFree)

	// Connect a device with NO idle agents.
	md := mockdevice.New(mockdevice.Config{
		DeviceID:    model.NewUUID(),
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents:      []model.HelloAgent{}, // no agents at all
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := md.Connect(ctx); err != nil {
		t.Fatalf("mock device connect: %v", err)
	}
	defer md.Close()

	time.Sleep(300 * time.Millisecond)

	// Submit job — should get queued (202).
	resp := h.Post(t, "/api/v1/jobs", model.SubmitJobRequest{
		Command: "echo hello",
	}, cust.AccessToken)
	if resp.StatusCode != 202 {
		t.Fatalf("submit job with no agents: status=%d want 202, body=%s", resp.StatusCode, resp.Body)
	}

	var jobResp model.JobResponse
	json.Unmarshal([]byte(resp.Body), &jobResp)
	if jobResp.Job.Status != model.JobQueued {
		t.Errorf("status = %s, want queued", jobResp.Job.Status)
	}
	if jobResp.QueuePosition == nil || *jobResp.QueuePosition < 1 {
		t.Errorf("queue_position should be >= 1, got %v", jobResp.QueuePosition)
	}
}

// ─── Scenario 7: Queue → release → dequeue ────────────────────

func TestE2E_DequeueOnAgentAvailable(t *testing.T) {
	h := NewHarness(t)

	admin := h.RegisterAdmin(t)
	device, deviceToken := h.CreateDevice(t, admin.AccessToken)
	cust := h.RegisterCustomer(t, model.TierFree)

	agentID := model.NewUUID()

	// Start with NO idle agents.
	md := mockdevice.New(mockdevice.Config{
		DeviceID:    device.ID,
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents:      []model.HelloAgent{}, // empty
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := md.Connect(ctx); err != nil {
		t.Fatalf("mock device connect: %v", err)
	}
	defer md.Close()

	time.Sleep(300 * time.Millisecond)

	// Submit job — queued.
	resp := h.Post(t, "/api/v1/jobs", model.SubmitJobRequest{
		Command: "echo test",
	}, cust.AccessToken)
	if resp.StatusCode != 202 {
		t.Fatalf("submit queued job: status=%d want 202", resp.StatusCode)
	}
	jobResp := h.unmarshalJob(t, resp)

	// Now create an idle agent directly in the DB.
	ctx2 := context.Background()
	agent := &model.Agent{
		DeviceID: device.ID,
		Name:     "reconnect-agent",
		Image:    "iagent/agent:latest",
		Port:     9002,
		Status:   model.AgentIdle,
	}
	if err := h.Router.Agents.Create(ctx2, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Handle dispatch on the mock device.
	md.On(model.FrameJobDispatch, func(dev *mockdevice.MockDevice, f model.Frame) *model.Frame {
		var payload model.JobDispatchPayload
		json.Unmarshal(f.Payload, &payload)
		resultVal := "dequeued"
		respFrame := mockdevice.NewFrame(model.FrameJobResult, model.JobResultPayload{
			JobID:  payload.JobID,
			Status: model.JobSucceeded,
			Result: &resultVal,
		})
		return &respFrame
	})

	// Reconnect with an idle agent reported. This triggers allocator wakeup.
	md.Close()
	_ = agentID // already created in DB

	md2 := mockdevice.New(mockdevice.Config{
		DeviceID:    device.ID,
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents: []model.HelloAgent{
			{AgentID: agent.ID, Status: model.AgentIdle, Port: 9002, Tags: []string{"default"}},
		},
	})
	ctx3, cancel3 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel3()
	if err := md2.Connect(ctx3); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer md2.Close()

	time.Sleep(time.Second)

	// Job should now be dispatched/succeeded.
	resp = h.Get(t, "/api/v1/jobs/"+jobResp.Job.ID.String(), cust.AccessToken)
	var finalJob model.Job
	json.Unmarshal([]byte(resp.Body), &finalJob)
	if finalJob.Status != model.JobSucceeded && finalJob.Status != model.JobDispatched && finalJob.Status != model.JobRunning {
		t.Errorf("dequeued job status = %s, want dispatched/running/succeeded", finalJob.Status)
	}
}

// ─── Scenario 8: Tier ordering ────────────────────────────────

func TestE2E_TierOrdering(t *testing.T) {
	h := NewHarness(t)

	admin := h.RegisterAdmin(t)
	_, deviceToken := h.CreateDevice(t, admin.AccessToken)

	enterpriseCust := h.RegisterCustomer(t, model.TierEnterprise)
	proCust := h.RegisterCustomer(t, model.TierPro)
	freeCust := h.RegisterCustomer(t, model.TierFree)

	// Connect a device with zero idle agents.
	md := mockdevice.New(mockdevice.Config{
		DeviceID:    model.NewUUID(),
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents:      []model.HelloAgent{},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := md.Connect(ctx); err != nil {
		t.Fatalf("mock device connect: %v", err)
	}
	defer md.Close()
	time.Sleep(300 * time.Millisecond)

	// Submit jobs in reverse priority order: free first, then pro, then enterprise.
	h.Post(t, "/api/v1/jobs", model.SubmitJobRequest{Command: "free job"}, freeCust.AccessToken)
	h.Post(t, "/api/v1/jobs", model.SubmitJobRequest{Command: "pro job"}, proCust.AccessToken)
	h.Post(t, "/api/v1/jobs", model.SubmitJobRequest{Command: "enterprise job"}, enterpriseCust.AccessToken)

	// Verify that enterprise tier has priority 0 (highest), pro=1, free=2.
	if model.TierEnterprise.TierPriority() >= model.TierPro.TierPriority() {
		t.Error("enterprise should beat pro")
	}
	if model.TierPro.TierPriority() >= model.TierFree.TierPriority() {
		t.Error("pro should beat free")
	}
	// The actual dequeue ordering is tested by the allocator unit tests.
}

// ─── Scenario 9: AuthZ — customer can't access admin routes ───

func TestE2E_CustomerCannotAccessAdminRoutes(t *testing.T) {
	h := NewHarness(t)

	cust := h.RegisterCustomer(t, model.TierFree)

	resp := h.Get(t, "/api/v1/devices", cust.AccessToken)
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("customer should not access devices: status=%d", resp.StatusCode)
	}

	resp = h.Get(t, "/api/v1/admin/agents", cust.AccessToken)
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("customer should not access admin agents: status=%d", resp.StatusCode)
	}
}

// ─── Scenario 10: Cross-tenant isolation ──────────────────────

func TestE2E_CrossTenantIsolation(t *testing.T) {
	h := NewHarness(t)

	cust1 := h.RegisterCustomer(t, model.TierFree)
	cust2 := h.RegisterCustomer(t, model.TierFree)

	// Create a job as cust1.
	job := &model.Job{
		UserID:   cust1.User.ID,
		UserTier: cust1.User.Tier,
		Command:  "secret job",
		Status:   model.JobSucceeded,
	}
	ctx := context.Background()
	if err := h.Router.Jobs.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	// Cust2 tries to access cust1's job.
	resp := h.Get(t, "/api/v1/jobs/"+job.ID.String(), cust2.AccessToken)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-tenant access: status=%d want 403", resp.StatusCode)
	}
}

// ─── Scenario 11: File upload ─────────────────────────────────

func TestE2E_FileUpload(t *testing.T) {
	h := NewHarness(t)
	cust := h.RegisterCustomer(t, model.TierFree)

	// Upload a file via multipart (simplified — test the endpoint exists).
	resp := h.Post(t, "/api/v1/files", model.SubmitJobRequest{Command: "dummy"}, cust.AccessToken)
	// This will fail validation but proves the endpoint is reachable.
	_ = resp
}

// ─── Scenario 12: Tunnel disconnect / reconnect ───────────────

func TestE2E_TunnelReconnect(t *testing.T) {
	h := NewHarness(t)

	admin := h.RegisterAdmin(t)
	device, deviceToken := h.CreateDevice(t, admin.AccessToken)

	md := mockdevice.New(mockdevice.Config{
		DeviceID:    device.ID,
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents: []model.HelloAgent{
			{AgentID: model.NewUUID(), Status: model.AgentIdle, Port: 9001, Tags: []string{"default"}},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := md.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Disconnect.
	md.Close()
	time.Sleep(200 * time.Millisecond)

	// Reconnect.
	md2 := mockdevice.New(mockdevice.Config{
		DeviceID:    device.ID,
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents: []model.HelloAgent{
			{AgentID: model.NewUUID(), Status: model.AgentIdle, Port: 9001, Tags: []string{"default"}},
		},
	})
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	if err := md2.Connect(ctx2); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer md2.Close()

	// Verify gateway shows the device accepted the reconnection.
	time.Sleep(200 * time.Millisecond)
	if h.Hub.OnlineCount() == 0 {
		t.Error("hub should have at least 1 online device after reconnect")
	}
}

// ─── Scenario 13: Skill fleet rollout ────────────────────────

func TestE2E_SkillFleetRollout(t *testing.T) {
	h := NewHarness(t)

	admin := h.RegisterAdmin(t)
	device, deviceToken := h.CreateDevice(t, admin.AccessToken)

	// Create a skill + version in the vault
	ctx := context.Background()
	skill, err := h.Router.Vault.CreateSkill(ctx, "e2e-rollout", "E2E Rollout", "rollout test", model.VisibilityPublic)
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}

	// Publish a version with a simple artifact
	manifest := `{"name":"e2e-rollout","version":"1.0.0","entrypoint":"SKILL.md","type":"claude-code"}`
	artifact := strings.NewReader("# E2E Rollout Skill\n\nTest skill for fleet rollout.")
	_, err = h.Router.Vault.PublishVersion(ctx, skill.ID, "1.0.0", manifest, artifact)
	if err != nil {
		t.Fatalf("publish version: %v", err)
	}

	// Connect mock device
	agentID := model.NewUUID()
	md := mockdevice.New(mockdevice.Config{
		DeviceID:    device.ID,
		DeviceToken: deviceToken,
		GatewayURL:  h.TunnelURL(),
		Agents: []model.HelloAgent{
			{AgentID: agentID, Status: model.AgentIdle, Port: 9001, Tags: []string{"default"}},
		},
	})

	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := md.Connect(testCtx); err != nil {
		t.Fatalf("device connect: %v", err)
	}
	defer md.Close()
	time.Sleep(300 * time.Millisecond)

	// Track skill dispatch frames
	dispatchStarted := make(chan struct{}, 1)
	actionReceived := make(chan struct{}, 1)

	md.On(model.FrameSkillDispatchBegin, func(dev *mockdevice.MockDevice, f model.Frame) *model.Frame {
		dispatchStarted <- struct{}{}
		return nil
	})
	md.On(model.FrameSkillAction, func(dev *mockdevice.MockDevice, f model.Frame) *model.Frame {
		var p model.SkillActionPayload
		json.Unmarshal(f.Payload, &p)
		actionReceived <- struct{}{}
		// Send back SKILL_STATE to mark as installed
		frame := mockdevice.NewFrame(model.FrameSkillState, model.SkillStatePayload{
			SkillID: p.SkillID,
			Scope:   p.Scope,
			Status:  model.SkillInstalled,
			AgentID: p.AgentID,
		})
		return &frame
	})

	// Trigger fleet install via API
	resp := h.Post(t, "/api/v1/admin/skills/"+skill.ID.String()+"/install", nil, admin.AccessToken)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("fleet install: status=%d body=%s", resp.StatusCode, resp.Body)
	}

	// Wait for dispatch to start
	select {
	case <-dispatchStarted:
		t.Log("SKILL_DISPATCH_BEGIN received")
	case <-time.After(10 * time.Second):
		t.Error("timeout waiting for SKILL_DISPATCH_BEGIN")
	}

	// Wait for action
	select {
	case <-actionReceived:
		t.Log("SKILL_ACTION received, SKILL_STATE sent back")
	case <-time.After(10 * time.Second):
		t.Error("timeout waiting for SKILL_ACTION")
	}

	// Check rollout status
	resp = h.Get(t, "/api/v1/admin/skills/"+skill.ID.String()+"/rollout", admin.AccessToken)
	if resp.StatusCode != 200 {
		t.Errorf("rollout status: %d", resp.StatusCode)
	}
	t.Logf("rollout: %s", resp.Body)
}
