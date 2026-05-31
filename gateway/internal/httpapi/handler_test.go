package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/oneClickAgent/gateway/internal/auth"
	"github.com/oneClickAgent/gateway/internal/config"
	"github.com/oneClickAgent/gateway/internal/httpapi"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/pool"
	"github.com/oneClickAgent/gateway/internal/pubsub"
	"github.com/oneClickAgent/gateway/internal/store"
)

// setupTestDeps creates mock dependencies for handler tests.
func setupTestDeps() *httpapi.Dependencies {
	cfg := config.Config{
		AccessTTL:          15 * time.Minute,
		RefreshTTL:         720 * time.Hour,
		MaxUploadBytes:     100 * 1024 * 1024,
		RateLimitAPIPerSec: 10000, // high limit for tests
	}

	broker := pubsub.NewBroker()
	mockAgents := store.NewMockAgentStore()
	mockJobs := store.NewMockJobStore()

	return &httpapi.Dependencies{
		Config:    cfg,
		Broker:    broker,
		Allocator: pool.NewAllocator(mockAgents, mockJobs, nil, broker, time.Hour, 10),
		JWT:       auth.NewJWTManager("test-secret-key-for-handler-tests!!", 15*time.Minute),
		Hasher:    auth.NewPasswordHasher(),
		Users:     store.NewMockUserStore(),
		Tokens:    store.NewMockTokenStore(),
		Jobs:      mockJobs,
		Agents:    mockAgents,
		Files:     store.NewMockFileStore(),
		Skills:    store.NewMockSkillStore(),
		Devices:   store.NewMockDeviceStore(),
		Orgs:      store.NewMockOrgStore(),
	}
}

// execHandler creates a test server with the router and executes a request.
func execHandler(deps *httpapi.Dependencies, method, path, body string, token string) *httptest.ResponseRecorder {
	router := httpapi.NewRouter(deps)
	ts := httptest.NewServer(router)
	defer ts.Close()

	var req *http.Request
	if body != "" {
		req, _ = http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	} else {
		req, _ = http.NewRequest(method, ts.URL+path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ─── Auth Handlers ──────────────────────────────────────────

func TestRegister(t *testing.T) {
	deps := setupTestDeps()
	body := `{"email":"test@example.com","username":"testuser","password":"password12345"}`
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register", body, "")

	if w.Code != http.StatusCreated {
		t.Errorf("register: status = %d, want %d, body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp model.AuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("access_token should not be empty")
	}
	if resp.RefreshToken == "" {
		t.Error("refresh_token should not be empty")
	}
	if resp.User.Email != "test@example.com" {
		t.Errorf("email = %s, want test@example.com", resp.User.Email)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	deps := setupTestDeps()
	body := `{"email":"dup@example.com","username":"dupuser","password":"password12345"}`
	// First register
	execHandler(deps, http.MethodPost, "/api/v1/auth/register", body, "")
	// Second register with same email
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register", body, "")

	if w.Code != http.StatusConflict {
		t.Errorf("duplicate register: status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestRegisterMissingFields(t *testing.T) {
	deps := setupTestDeps()

	tests := []struct {
		name string
		body string
	}{
		{"empty", `{}`},
		{"no password", `{"email":"a@b.com","username":"a"}`},
		{"no email", `{"username":"a","password":"password12345"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := execHandler(deps, http.MethodPost, "/api/v1/auth/register", tt.body, "")
			if w.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, want %d", tt.name, w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	deps := setupTestDeps()
	// Register first
	execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"login@example.com","username":"loginuser","password":"password12345"}`, "")

	// Login
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/login",
		`{"email":"login@example.com","password":"password12345"}`, "")

	if w.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp model.AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.AccessToken == "" {
		t.Error("login should return access_token")
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	deps := setupTestDeps()
	execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"bad@example.com","username":"baduser","password":"password12345"}`, "")

	w := execHandler(deps, http.MethodPost, "/api/v1/auth/login",
		`{"email":"bad@example.com","password":"wrongpassword"}`, "")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("invalid login: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMeEndpoint(t *testing.T) {
	deps := setupTestDeps()
	// Register and get token
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"me@example.com","username":"meuser","password":"password12345"}`, "")

	var resp model.AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Call /me
	w2 := execHandler(deps, http.MethodGet, "/api/v1/auth/me", "", resp.AccessToken)
	if w2.Code != http.StatusOK {
		t.Fatalf("me: status = %d, want %d", w2.Code, http.StatusOK)
	}

	var user model.User
	json.Unmarshal(w2.Body.Bytes(), &user)
	if user.Email != "me@example.com" {
		t.Errorf("me email = %s", user.Email)
	}
}

func TestMeWithoutToken(t *testing.T) {
	deps := setupTestDeps()
	w := execHandler(deps, http.MethodGet, "/api/v1/auth/me", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("me without token: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ─── Job Handlers ───────────────────────────────────────────

func TestSubmitJob(t *testing.T) {
	deps := setupTestDeps()
	// Register and login to get token
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"jobuser@example.com","username":"jobuser","password":"password12345"}`, "")
	var resp model.AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	token := resp.AccessToken

	// Create an idle agent
	agent := &model.Agent{
		Name:   "test-agent",
		Image:  "iagent/agent:latest",
		Port:   42001,
		Status: model.AgentIdle,
	}
	deps.Agents.Create(nil, agent)

	// Submit job
	body := `{"command":"echo hello"}`
	w2 := execHandler(deps, http.MethodPost, "/api/v1/jobs", body, token)

	if w2.Code != http.StatusCreated {
		t.Fatalf("submit job: status = %d, want %d, body = %s", w2.Code, http.StatusCreated, w2.Body.String())
	}

	var jobResp model.JobResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &jobResp); err != nil {
		t.Fatalf("decode job response: %v", err)
	}
	if jobResp.Job.Status != model.JobDispatched {
		t.Errorf("job status = %s, want dispatched", jobResp.Job.Status)
	}
	if jobResp.AgentID == nil {
		t.Error("job should have agent_id after allocation")
	}
}

func TestSubmitJobNoAgent(t *testing.T) {
	deps := setupTestDeps()
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"noagent@example.com","username":"noagent","password":"password12345"}`, "")
	var resp model.AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Submit job without any idle agent
	w2 := execHandler(deps, http.MethodPost, "/api/v1/jobs",
		`{"command":"echo hello"}`, resp.AccessToken)

	// Should get queued (202)
	if w2.Code != http.StatusAccepted {
		t.Errorf("no agent: status = %d, want %d", w2.Code, http.StatusAccepted)
	}
}

func TestSubmitJobWithoutToken(t *testing.T) {
	deps := setupTestDeps()
	w := execHandler(deps, http.MethodPost, "/api/v1/jobs", `{"command":"test"}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestListJobs(t *testing.T) {
	deps := setupTestDeps()
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"listjobs@example.com","username":"listjobs","password":"password12345"}`, "")
	var resp model.AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Create a few jobs for this user
	for i := 0; i < 3; i++ {
		job := &model.Job{
			UserID:   resp.User.ID,
			UserTier: resp.User.Tier,
			Command:  "test",
			Status:   model.JobSucceeded,
		}
		deps.Jobs.Create(nil, job)
	}

	w2 := execHandler(deps, http.MethodGet, "/api/v1/jobs", "", resp.AccessToken)
	if w2.Code != http.StatusOK {
		t.Fatalf("list jobs: status = %d", w2.Code)
	}

	var page model.PaginatedResponse[model.Job]
	json.Unmarshal(w2.Body.Bytes(), &page)
	if len(page.Data) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(page.Data))
	}
}

func TestGetJob(t *testing.T) {
	deps := setupTestDeps()
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"getjob@example.com","username":"getjob","password":"password12345"}`, "")
	var resp model.AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	job := &model.Job{
		UserID:   resp.User.ID,
		UserTier: resp.User.Tier,
		Command:  "test-get",
	}
	deps.Jobs.Create(nil, job)

	w2 := execHandler(deps, http.MethodGet, "/api/v1/jobs/"+job.ID.String(), "", resp.AccessToken)
	if w2.Code != http.StatusOK {
		t.Fatalf("get job: status = %d", w2.Code)
	}

	var fetched model.Job
	json.Unmarshal(w2.Body.Bytes(), &fetched)
	if fetched.Command != "test-get" {
		t.Errorf("command = %s, want test-get", fetched.Command)
	}
}

func TestGetJobForbidden(t *testing.T) {
	deps := setupTestDeps()
	// User 1
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"user1@example.com","username":"user1get","password":"password12345"}`, "")
	var resp1 model.AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp1)

	job := &model.Job{
		UserID:   resp1.User.ID,
		UserTier: resp1.User.Tier,
		Command:  "test",
	}
	deps.Jobs.Create(nil, job)

	// User 2
	w2 := execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"user2@example.com","username":"user2get","password":"password12345"}`, "")
	var resp2 model.AuthResponse
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	// User 2 tries to access User 1's job
	w3 := execHandler(deps, http.MethodGet, "/api/v1/jobs/"+job.ID.String(), "", resp2.AccessToken)
	if w3.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 Forbidden", w3.Code)
	}
}

func TestCancelJob(t *testing.T) {
	deps := setupTestDeps()
	w := execHandler(deps, http.MethodPost, "/api/v1/auth/register",
		`{"email":"cancel@example.com","username":"canceluser","password":"password12345"}`, "")
	var resp model.AuthResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	job := &model.Job{
		UserID:   resp.User.ID,
		UserTier: resp.User.Tier,
		Command:  "test-cancel",
	}
	deps.Jobs.Create(nil, job)

	w2 := execHandler(deps, http.MethodPost, "/api/v1/jobs/"+job.ID.String()+"/cancel", "", resp.AccessToken)
	if w2.Code != http.StatusOK {
		t.Errorf("cancel: status = %d, want %d", w2.Code, http.StatusOK)
	}

	// Verify it's cancelled
	j, _ := deps.Jobs.GetByID(nil, job.ID)
	if j.Status != model.JobCancelled {
		t.Errorf("job status = %s, want cancelled", j.Status)
	}
}

// ─── Health Endpoints ───────────────────────────────────────

func TestHealthz(t *testing.T) {
	deps := setupTestDeps()
	w := execHandler(deps, http.MethodGet, "/healthz", "", "")
	if w.Code != http.StatusOK {
		t.Errorf("healthz: status = %d, want %d", w.Code, http.StatusOK)
	}
}
