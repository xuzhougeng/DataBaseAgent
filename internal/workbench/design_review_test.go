package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
)

func assistant(t *testing.T, s *Service) Agent {
	t.Helper()
	a, err := agentByID(ptr(s.Store.Snapshot()), "assistant")
	if err != nil {
		t.Fatal(err)
	}
	return *a
}

func TestModelCredentialsStayWithTheirEndpoint(t *testing.T) {
	keyring.MockInit()
	s, pid := fixture(t)
	key := "secret-model-key"
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "m", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "x"}, APIKey: &key})
	m := s.Store.Snapshot().Settings.Models[0]
	if m.APIKeyRef == "" {
		t.Fatal("key not stored")
	}
	m.BaseURL = "https://model.example/v2" // same origin: the key may follow a path change
	action(t, s, "saveModel", Request{Model: m})

	moved := m
	moved.BaseURL = "https://collector.example/v1"
	dispatchError(t, s, "saveModel", Request{Model: moved})
	switched := m
	switched.Protocol = "anthropic"
	dispatchError(t, s, "saveModel", Request{Model: switched})
	a := assistant(t, s)
	if out, failed := s.runTool(context.Background(), pid, a, toolCall("save_model", map[string]any{"model": map[string]any{"id": m.ID, "baseUrl": "https://collector.example/v1"}})); !failed || !strings.Contains(out, "重新填写 API Key") {
		t.Fatalf("agent moved a stored key to another host: %s", out)
	}
	if got := s.Store.Snapshot().Settings.Models[0]; got.BaseURL != "https://model.example/v2" || got.APIKeyRef != m.APIKeyRef {
		t.Fatal("refused change was applied")
	}

	// Agents cannot pick an environment variable as a credential source.
	if out, failed := s.runTool(context.Background(), pid, a, toolCall("save_model", map[string]any{"model": map[string]any{"name": "n", "protocol": "openai", "baseUrl": "https://collector.example", "model": "x", "apiKeyEnv": "HOME"}})); !failed {
		t.Fatalf("agent set apiKeyEnv: %s", out)
	}
	agentCtx := context.WithValue(context.Background(), agentActorKey{}, "assistant")
	raw, _ := json.Marshal(Request{Model: ModelProfile{Name: "n", Protocol: "openai", BaseURL: "https://collector.example", Model: "x", APIKeyEnv: "HOME"}})
	if _, err := s.Dispatch(agentCtx, "saveModel", raw); err == nil {
		t.Fatal("agent actor set apiKeyEnv through Dispatch")
	}

	// The user can move a profile by entering a key for the new endpoint; the old key is removed.
	other := "second-key"
	action(t, s, "saveModel", Request{Model: moved, APIKey: &other})
	got := s.Store.Snapshot().Settings.Models[0]
	if got.BaseURL != moved.BaseURL || got.APIKeyRef == m.APIKeyRef {
		t.Fatal("explicit key change was not applied")
	}
	if _, err := keyring.Get(modelKeyService, s.Store.modelKeyAccount(m.APIKeyRef)); err == nil {
		t.Fatal("replaced key was left in the keychain")
	}
}

func TestProjectConversationToolsStayInTheirProject(t *testing.T) {
	s, pid := fixture(t)
	action(t, s, "createProject", Request{Name: "other"})
	other := s.Store.Snapshot().Projects[1].ID
	a := assistant(t, s)
	out, failed := s.runTool(context.Background(), pid, a, toolCall("update_project", map[string]any{"projectId": other, "name": "hijacked"}))
	if !failed || !strings.Contains(out, "只能操作该项目") || s.Store.Snapshot().Projects[1].Name != "other" {
		t.Fatalf("project conversation reached another project: %s", out)
	}
	callTool(t, s, pid, "assistant", "get_project", map[string]any{"projectId": pid})
	callTool(t, s, "", "assistant", "get_project", map[string]any{"projectId": other})
}

func waitApproval(t *testing.T, s *Service) Approval {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pending := s.Approvals(); len(pending) == 1 {
			return pending[0]
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no approval request")
	return Approval{}
}

type toolResult struct {
	out    string
	failed bool
}

func TestHighImpactToolsWaitForApproval(t *testing.T) {
	s, pid := fixture(t)
	s.autoApprove = false
	a := assistant(t, s)
	run := func(ctx context.Context, name string, args map[string]any) <-chan toolResult {
		done := make(chan toolResult, 1)
		go func() {
			out, failed := s.runTool(ctx, pid, a, toolCall(name, args))
			done <- toolResult{out, failed}
		}()
		return done
	}

	done := run(context.Background(), "launch_jobs", map[string]any{})
	ap := waitApproval(t, s)
	if ap.Tool != "launch_jobs" || !strings.Contains(ap.Detail, "将创建 1 个任务") || !strings.Contains(ap.Detail, "expression.tsv") || ap.Reason == "" {
		t.Fatalf("approval lacks the commands to run: %+v", ap)
	}
	if len(s.Store.Snapshot().Jobs) != 0 {
		t.Fatal("jobs created before approval")
	}
	if body, _ := s.stateResponse(""); !strings.Contains(string(body), ap.ID) {
		t.Fatal("pending approval missing from state polling")
	}
	action(t, s, "decideApproval", Request{ApprovalID: ap.ID, Approve: true})
	if r := <-done; r.failed || len(s.Store.Snapshot().Jobs) != 1 {
		t.Fatalf("approved launch failed: %s", r.out)
	}
	dispatchError(t, s, "decideApproval", Request{ApprovalID: ap.ID, Approve: true})

	// Nothing new to launch: the dry run fails without asking.
	if out, failed := s.runTool(context.Background(), pid, a, toolCall("launch_jobs", map[string]any{})); !failed || len(s.Approvals()) != 0 {
		t.Fatalf("invalid launch asked for approval: %s", out)
	}

	servers := len(s.Store.Snapshot().Servers)
	done = run(context.Background(), "add_server", map[string]any{"server": map[string]any{"name": "x", "host": "collector.example", "user": "u", "workDir": "/srv/dba", "cpuQuota": 4, "maxJobs": 1}})
	ap = waitApproval(t, s)
	action(t, s, "decideApproval", Request{ApprovalID: ap.ID, Approve: false})
	if r := <-done; !r.failed || !strings.Contains(r.out, "拒绝") || len(s.Store.Snapshot().Servers) != servers {
		t.Fatalf("denied tool ran: %s", r.out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done = run(ctx, "save_agent", map[string]any{"agent": map[string]any{"id": "scout", "maxSteps": 3}})
	waitApproval(t, s)
	cancel()
	if r := <-done; !r.failed || len(s.Approvals()) != 0 {
		t.Fatal("cancelled run left its approval pending")
	}
	if a, _ := agentByID(ptr(s.Store.Snapshot()), "scout"); a.MaxSteps == 3 {
		t.Fatal("cancelled tool ran")
	}
}

func TestReadOnlyMCPToolsSkipApproval(t *testing.T) {
	s, _ := fixture(t)
	s.autoApprove = false
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	reply := func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	}
	server.AddTool(&mcp.Tool{Name: "lookup", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, InputSchema: map[string]any{"type": "object"}}, reply)
	server.AddTool(&mcp.Tool{Name: "publish", InputSchema: map[string]any{"type": "object"}}, reply)
	remote := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	defer remote.Close()
	v := saveTestMCP(t, s, MCPServer{Name: "fixture", Transport: "http", URL: remote.URL, Enabled: true})
	a := assistant(t, s)
	runCtx := &mcpRun{connections: map[string]*mcpConnection{}}
	defer runCtx.close()
	ctx := context.WithValue(context.Background(), mcpRunKey{}, runCtx)
	call := func(tool string) <-chan toolResult {
		done := make(chan toolResult, 1)
		go func() {
			out, failed := s.runTool(ctx, "", a, toolCall("call_mcp_tool", map[string]any{"serverId": v.ID, "tool": tool, "arguments": map[string]any{}}))
			done <- toolResult{out, failed}
		}()
		return done
	}
	select {
	case r := <-call("lookup"):
		if r.failed {
			t.Fatalf("read-only call failed: %s", r.out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read-only MCP tool waited for approval")
	}
	done := call("publish")
	ap := waitApproval(t, s)
	if ap.Tool != "call_mcp_tool" || !strings.Contains(ap.Detail, "publish") {
		t.Fatalf("unexpected approval: %+v", ap)
	}
	action(t, s, "decideApproval", Request{ApprovalID: ap.ID, Approve: true})
	if r := <-done; r.failed {
		t.Fatalf("approved MCP call failed: %s", r.out)
	}
}

func TestPreviewServerRejectsForeignHosts(t *testing.T) {
	s, _ := fixture(t)
	h := LoopbackOnly(Handler(s, fstest.MapFS{}))
	for host, want := range map[string]int{"127.0.0.1:3415": 200, "localhost:3415": 200, "LOCALHOST": 200, "[::1]:3415": 200, "rebind.example:3415": 403, "127.0.0.1.nip.io:3415": 403} {
		req := httptest.NewRequest("GET", "/api/state", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: got %d, want %d", host, rec.Code, want)
		}
	}
	// A rebinding page sends an Origin that matches its Host; it must still be refused.
	req := httptest.NewRequest("POST", "/api/launch", strings.NewReader(`{}`))
	req.Host = "rebind.example:3415"
	req.Header.Set("Origin", "http://rebind.example:3415")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("rebinding POST got %d", rec.Code)
	}
}

func TestStatePollingUsesETagAndRedactsCredentials(t *testing.T) {
	keyring.MockInit()
	s, _ := fixture(t)
	key := "k"
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "m", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "x"}, APIKey: &key})
	ref := s.Store.Snapshot().Settings.Models[0].APIKeyRef
	h := Handler(s, fstest.MapFS{})
	get := func(tag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/state", nil)
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	first := get("")
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag == "" || strings.Contains(first.Body.String(), ref) || !strings.Contains(first.Body.String(), `"keySaved"`) {
		t.Fatal("state response missing tag or leaked a credential reference")
	}
	if s.Store.Snapshot().Settings.Models[0].APIKeyRef != ref {
		t.Fatal("redaction modified the live state")
	}
	if again := get(tag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("unchanged state was sent again: %d", again.Code)
	}
	cfg := s.Store.Snapshot().Settings
	cfg.IntervalSeconds = 30
	action(t, s, "settings", Request{Settings: cfg})
	if changed := get(tag); changed.Code != 200 || changed.Header().Get("ETag") == tag {
		t.Fatal("changed state answered 304")
	}
}

func TestStoreSkipsUnchangedWritesAndTrimsAgentEventsFirst(t *testing.T) {
	s, _ := fixture(t)
	file := filepath.Join(s.Store.Root(), "workbench.json")
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(file, past, past); err != nil {
		t.Fatal(err)
	}
	version := s.Store.Version()
	if err := s.Store.Update(func(*State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(file); !info.ModTime().Equal(past) || s.Store.Version() != version {
		t.Fatal("unchanged transaction rewrote the workspace")
	}
	if err := s.Store.Update(func(st *State) error { st.LastTick = now(); return nil }); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(file); info.ModTime().Equal(past) || s.Store.Version() != version+1 {
		t.Fatal("change was not written")
	}

	if err := s.Store.Update(func(st *State) error {
		for i := 0; i < maxEvents+100; i++ {
			kind := "review"
			if i%2 == 0 {
				kind = "agent"
			}
			event(st, "", "", kind, "trim-"+strconv.Itoa(i), "")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	agent, kept := 0, 0
	for _, e := range s.Store.Snapshot().Events {
		if e.Kind == "agent" {
			agent++
		} else if strings.HasPrefix(e.Title, "trim-") {
			kept++
		}
	}
	if agent > maxAgentEvents || kept != (maxEvents+100)/2 {
		t.Fatalf("agent events crowded out the audit trail: %d agent, %d others kept", agent, kept)
	}
}

type gateExecutor struct {
	fakeExecutor
	gate chan struct{}
}

func (g *gateExecutor) Start(ctx context.Context, _ Job) error {
	select {
	case <-g.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestTickReservesQueuedJobsInOneWrite(t *testing.T) {
	s, pid := fixture(t)
	gate := &gateExecutor{gate: make(chan struct{})}
	s.Runner = gate
	t.Cleanup(func() { close(gate.gate) })
	for i := 0; i < 4; i++ {
		action(t, s, "importCandidates", Request{ProjectID: pid, Candidates: []Candidate{{Title: "Dataset " + strconv.Itoa(i+2), Accession: "GSE" + strconv.Itoa(i+2)}}})
	}
	for _, c := range s.Store.Snapshot().Projects[0].Candidates {
		if c.Decision != "accepted" {
			action(t, s, "review", Request{ProjectID: pid, CandidateID: c.ID, Decision: "accepted", Note: "ok", Accession: c.Accession})
		}
	}
	action(t, s, "launch", Request{ProjectID: pid})
	version := s.Store.Version()
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	starting := 0
	for _, j := range s.Store.Snapshot().Jobs {
		if j.Status == "starting" {
			starting++
		}
	}
	if s.Store.Version() != version+1 || starting != 1 {
		t.Fatalf("tick wrote %d times and started %d jobs within a one-job quota", s.Store.Version()-version, starting)
	}
}

type progressExecutor struct {
	fakeExecutor
	progress bool
}

func (p *progressExecutor) Sync(ctx context.Context, j Job, root string) ([]Artifact, error) {
	if p.progress {
		return nil, &TransferProgress{Err: errors.New("connection reset"), Bytes: 4096}
	}
	return p.fakeExecutor.Sync(ctx, j, root)
}

func TestSyncThatKeepsProgressingIsNotEscalated(t *testing.T) {
	s, pid := fixture(t)
	f := &progressExecutor{fakeExecutor: fakeExecutor{observation: Observation{Status: "succeeded"}}, progress: true}
	s.Runner = f
	cfg := s.Store.Snapshot().Settings
	cfg.MaxRetries = 1
	action(t, s, "settings", Request{Settings: cfg})
	action(t, s, "launch", Request{ProjectID: pid})
	tickWait(t, s) // start
	tickWait(t, s) // poll: succeeded → syncing
	for i := 0; i < 5; i++ {
		tickWait(t, s)
		_ = s.Store.Update(func(st *State) error { st.Jobs[0].RetryAt = ""; return nil })
	}
	if j := s.Store.Snapshot().Jobs[0]; j.Status != "syncing" || j.Failures != 1 {
		t.Fatalf("progressing transfer escalated: %s, %d failures", j.Status, j.Failures)
	}
	f.progress = false
	tickWait(t, s)
	if j := s.Store.Snapshot().Jobs[0]; j.Status != "succeeded" || f.starts != 1 {
		t.Fatal("resumed transfer did not finish without rerun")
	}
}

func TestRemoteScriptsReclaimStaleLocksAndRejectReusedPIDs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote scripts target POSIX sh")
	}
	dir := filepath.Join(t.TempDir(), "attempt-1")
	j := Job{ID: "job_script", Attempt: 1, RemoteDir: dir, Accession: "GSE1", Pipeline: Pipeline{CPUs: 1, Steps: []Step{{Name: "noop", Tool: "sh", Command: "true"}}}}
	run := func(script string) (string, int) {
		out, err := exec.Command("sh", "-c", script).CombinedOutput()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return string(out), exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), 0
	}
	lock := filepath.Join(dir, ".submit-lock")
	if err := os.MkdirAll(lock, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(lock, "owner"), []byte(strconv.Itoa(os.Getpid())), 0600)
	if _, code := run(startScript(j)); code != 75 {
		t.Fatalf("live lock owner was overridden: exit %d", code)
	}
	dead := exec.Command("sh", "-c", "exit 0")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(lock, "owner"), []byte(strconv.Itoa(dead.ProcessState.Pid())), 0600)
	if out, code := run(startScript(j)); code != 0 || !strings.Contains(out, "Submitted") {
		t.Fatalf("stale lock blocked submission: %d %s", code, out)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("submission lock not released")
	}
	if out, _ := run(startScript(j)); !strings.Contains(out, "Already submitted") {
		t.Fatal("second submission was not idempotent")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "exitcode")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	poll := func() string {
		out, _ := run(pollScript(j))
		obs, err := parsePoll(out)
		if err != nil {
			t.Fatal(err)
		}
		return obs.Status
	}
	if got := poll(); got != "succeeded" {
		t.Fatalf("finished job polled as %s", got)
	}
	// A live but unrelated process now holds the recorded PID.
	_ = os.Remove(filepath.Join(dir, "exitcode"))
	_ = os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
	_ = os.WriteFile(filepath.Join(dir, "pid.start"), []byte("1"), 0600)
	if got := poll(); got != "unknown" {
		t.Fatalf("reused PID counted as the job: %s", got)
	}
	_ = os.Remove(filepath.Join(dir, "pid.start")) // jobs from before start times were recorded
	if got := poll(); got != "running" {
		t.Fatalf("legacy job without start time polled as %s", got)
	}
}
