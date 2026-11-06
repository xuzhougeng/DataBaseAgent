package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func action(t *testing.T, s *Service, name string, r Request) {
	t.Helper()
	b, _ := json.Marshal(r)
	if _, e := s.Dispatch(context.Background(), name, b); e != nil {
		t.Fatalf("%s: %v", name, e)
	}
}
func fixture(t *testing.T) (*Service, string) {
	t.Helper()
	store, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	s := NewService(store)
	s.autoApprove = true
	t.Cleanup(s.Stop)
	action(t, s, "createProject", Request{Name: "数据库一", Description: "研究目标"})
	pid := s.Store.Snapshot().Projects[0].ID
	action(t, s, "addServer", Request{Server: Server{Name: "test", Host: "example.org", User: "research", Port: 22, WorkDir: "/home/research/dba", CPUQuota: 8, MaxJobs: 1}})
	action(t, s, "importCandidates", Request{ProjectID: pid, Candidates: []Candidate{{Title: "Dataset one", Accession: "GSE1", Abstract: "A real source sentence."}}})
	st := s.Store.Snapshot()
	cid := st.Projects[0].Candidates[0].ID
	action(t, s, "review", Request{ProjectID: pid, CandidateID: cid, Decision: "accepted", Note: "已核对原始数据与实验设计", Accession: "GSE1"})
	v := Pipeline{Name: "user workflow", ServerID: st.Servers[0].ID, CPUs: 4, Concurrency: 2, Steps: []Step{{Name: "quantify", Tool: "tool@1", Command: "printf 'gene\\tcount\\nA\\t1\\n' > expression.tsv"}}, Outputs: []string{"expression.tsv"}}
	action(t, s, "lockPipeline", Request{ProjectID: pid, Pipeline: v})
	return s, pid
}

type fakeExecutor struct {
	mu          sync.Mutex
	starts      int
	polls       int
	syncs       int
	failPoll    bool
	failSync    bool
	observation Observation
}

func (*fakeExecutor) Probe(context.Context, Server) (string, error) { return "OK", nil }
func (f *fakeExecutor) Start(context.Context, Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return nil
}
func (f *fakeExecutor) Poll(context.Context, Job) (Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	if f.failPoll {
		return Observation{}, errors.New("temporary SSH interruption")
	}
	return f.observation, nil
}
func (f *fakeExecutor) Sync(_ context.Context, j Job, root string) ([]Artifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs++
	if f.failSync {
		return nil, errors.New("checksum mismatch")
	}
	name := filepath.Join(root, "projects", j.ProjectID, "results", j.ID, "expression.tsv")
	if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
		return nil, e
	}
	if e := os.WriteFile(name, []byte("gene\tcount\nA\t1\n"), 0600); e != nil {
		return nil, e
	}
	sha, _ := hashFile(name)
	return []Artifact{{ID: j.ID + ":expression.tsv", JobID: j.ID, ProjectID: j.ProjectID, Name: "expression.tsv", Path: name, SHA256: sha, Size: 15}}, nil
}
func tickWait(t *testing.T, s *Service) {
	t.Helper()
	if e := s.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.opMu.Lock()
		n := len(s.inFlight)
		s.opMu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("loop did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestProjectIsolationImmutableRunsAndPersistence(t *testing.T) {
	s, pid := fixture(t)
	f := &fakeExecutor{observation: Observation{Status: "succeeded", Log: "done"}}
	s.Runner = f
	action(t, s, "createProject", Request{Name: "数据库二"})
	action(t, s, "launch", Request{ProjectID: pid})
	st := s.Store.Snapshot()
	if len(st.Jobs) != 1 || st.Jobs[0].Pipeline.Version != 1 {
		t.Fatal("invalid queue")
	}
	// Later edits cannot alter the approved execution or the previous locked version.
	v := st.Projects[0].Draft
	v.Steps[0].Command = "different command"
	action(t, s, "savePipeline", Request{ProjectID: pid, Pipeline: v})
	st = s.Store.Snapshot()
	if st.Jobs[0].Pipeline.Steps[0].Command == "different command" || st.Projects[0].Versions[0].Steps[0].Command == "different command" {
		t.Fatal("locked snapshots mutated")
	}
	if len(st.Projects[1].Candidates) != 0 {
		t.Fatal("cross-project candidate leak")
	}
	tickWait(t, s)
	tickWait(t, s)
	tickWait(t, s)
	st = s.Store.Snapshot()
	if st.Jobs[0].Status != "succeeded" || len(st.Artifacts) != 1 {
		t.Fatalf("not archived: %#v", st.Jobs[0])
	}
	if len(st.Artifacts[0].SHA256) != 64 {
		t.Fatal("missing checksum")
	}
	if _, e := os.Stat(filepath.Join(s.Store.Root(), "projects", pid, "results", st.Jobs[0].ID, "run_manifest.json")); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(s.Store.Root())
	if e != nil {
		t.Fatal(e)
	}
	if reopened.Snapshot().Jobs[0].Status != "succeeded" {
		t.Fatal("lost durable job state")
	}
	// Candidate and immutable pipeline prevent accidental duplicate submissions.
	b, _ := json.Marshal(Request{ProjectID: pid})
	if _, e = s.Dispatch(context.Background(), "launch", b); e == nil {
		t.Fatal("unlocked draft submitted")
	}
}

func TestBoundedRecoveryNeverResubmitsOnPollFailure(t *testing.T) {
	s, pid := fixture(t)
	f := &fakeExecutor{failPoll: true}
	s.Runner = f
	cfg := s.Store.Snapshot().Settings
	cfg.MaxRetries = 1
	action(t, s, "settings", Request{Settings: cfg})
	action(t, s, "launch", Request{ProjectID: pid})
	tickWait(t, s)
	tickWait(t, s)
	_ = s.Store.Update(func(st *State) error { st.Jobs[0].RetryAt = ""; return nil })
	tickWait(t, s)
	st := s.Store.Snapshot()
	if st.Jobs[0].Status != "needs_help" || st.Jobs[0].ResumeStatus != "running" {
		t.Fatal("did not request help")
	}
	if f.starts != 1 {
		t.Fatal("poll failure duplicated remote execution")
	}
	action(t, s, "resolveJob", Request{JobID: st.Jobs[0].ID, Note: "连接已恢复"})
	f.failPoll = false
	f.observation = Observation{Status: "succeeded"}
	tickWait(t, s)
	tickWait(t, s)
	if s.Store.Snapshot().Jobs[0].Status != "succeeded" || f.starts != 1 {
		t.Fatal("resume did not preserve original process")
	}
}

func TestRestartResumesObservationWithoutResubmission(t *testing.T) {
	s, pid := fixture(t)
	f := &fakeExecutor{observation: Observation{Status: "running"}}
	s.Runner = f
	action(t, s, "launch", Request{ProjectID: pid})
	tickWait(t, s)
	s.Stop()
	store, e := Open(s.Store.Root())
	if e != nil {
		t.Fatal(e)
	}
	resumed := NewService(store)
	resumed.autoApprove = true
	t.Cleanup(resumed.Stop)
	resumed.Runner = f
	f.observation = Observation{Status: "succeeded", Log: "finished while app was closed"}
	tickWait(t, resumed)
	tickWait(t, resumed)
	if f.starts != 1 || resumed.Store.Snapshot().Jobs[0].Status != "succeeded" {
		t.Fatal("restart resubmitted instead of observing existing process")
	}
}

func TestSyncExhaustionResumesTransferWithoutRerun(t *testing.T) {
	s, pid := fixture(t)
	f := &fakeExecutor{observation: Observation{Status: "succeeded"}, failSync: true}
	s.Runner = f
	cfg := s.Store.Snapshot().Settings
	cfg.MaxRetries = 1
	action(t, s, "settings", Request{Settings: cfg})
	action(t, s, "launch", Request{ProjectID: pid})
	tickWait(t, s)
	tickWait(t, s)
	tickWait(t, s)
	_ = s.Store.Update(func(st *State) error { st.Jobs[0].RetryAt = ""; return nil })
	tickWait(t, s)
	j := s.Store.Snapshot().Jobs[0]
	if j.Status != "needs_help" || j.ResumeStatus != "syncing" {
		t.Fatal("sync failures did not escalate with preserved phase")
	}
	f.failSync = false
	action(t, s, "resolveJob", Request{JobID: j.ID, Note: "已修复产物读取权限"})
	tickWait(t, s)
	if f.starts != 1 || s.Store.Snapshot().Jobs[0].Status != "succeeded" {
		t.Fatal("transfer recovery reran the pipeline")
	}
}

func TestServerQuotaAcrossProjectsAndPendingGate(t *testing.T) {
	s, pid := fixture(t)
	f := &fakeExecutor{observation: Observation{Status: "running"}}
	s.Runner = f
	action(t, s, "importCandidates", Request{ProjectID: pid, Candidates: []Candidate{{Title: "unreviewed dataset", Accession: "GSE2", Decision: "accepted"}}})
	action(t, s, "launch", Request{ProjectID: pid})
	if len(s.Store.Snapshot().Jobs) != 1 {
		t.Fatal("unreviewed input scheduled")
	}
	st := s.Store.Snapshot()
	action(t, s, "createProject", Request{Name: "Second"})
	pid2 := s.Store.Snapshot().Projects[1].ID
	action(t, s, "importCandidates", Request{ProjectID: pid2, Candidates: []Candidate{{Title: "other dataset", Accession: "GSE3"}}})
	cid2 := s.Store.Snapshot().Projects[1].Candidates[0].ID
	action(t, s, "review", Request{ProjectID: pid2, CandidateID: cid2, Decision: "accepted", Note: "checked"})
	action(t, s, "lockPipeline", Request{ProjectID: pid2, Pipeline: st.Projects[0].Draft})
	action(t, s, "launch", Request{ProjectID: pid2})
	tickWait(t, s)
	st = s.Store.Snapshot()
	if st.Jobs[0].Status != "running" || st.Jobs[1].Status != "queued" || f.starts != 1 {
		t.Fatal("server concurrency quota exceeded")
	}
}

func TestPersistenceRollbackAndCorruption(t *testing.T) {
	store, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	e = store.Update(func(st *State) error {
		st.Projects = append(st.Projects, Project{Name: "must rollback"})
		return errors.New("abort")
	})
	if e == nil || len(store.Snapshot().Projects) != 0 {
		t.Fatal("transaction rollback failed")
	}
	if e = os.WriteFile(filepath.Join(store.Root(), "workbench.json"), []byte("{broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Open(store.Root()); e == nil {
		t.Fatal("corruption silently overwritten")
	}
}

func TestPathAndHTTPBoundaries(t *testing.T) {
	for _, name := range []string{"../secret", "/etc/passwd", "x/../../secret", "x\\secret", "file\nname", "run/../secret", "C:/secret"} {
		if safeRelative(name) {
			t.Errorf("unsafe path accepted: %s", name)
		}
	}
	if !safeRelative("qc/report.html") {
		t.Fatal("safe output rejected")
	}
	s, _ := fixture(t)
	handler := Handler(s, fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}})
	req := httptest.NewRequest("POST", "http://127.0.0.1:3415/api/createProject", strings.NewReader(`{"name":"evil"}`))
	req.Header.Set("Origin", "https://external.example")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal("cross-origin mutation allowed")
	}
	req = httptest.NewRequest("GET", "http://127.0.0.1:3415/api/state", nil)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "数据库一") {
		t.Fatal("state API not reachable")
	}
}

func TestExportManifestAndLinkBoundaries(t *testing.T) {
	s, pid := fixture(t)
	out, e := s.ExportManifest(pid)
	if e != nil || out != filepath.Join(s.Store.Root(), "projects", pid, "results", "dba_manifest.json") {
		t.Fatalf("manifest path %q: %v", out, e)
	}
	if b, e := os.ReadFile(out); e != nil || !strings.Contains(string(b), "数据库一") {
		t.Fatal("manifest missing project")
	}
	if _, e = s.ExportManifest("../../etc"); e == nil {
		t.Fatal("unknown project exported")
	}
	for _, link := range []string{"file:///etc/passwd", "javascript:alert(1)", "https://", "-a Calculator"} {
		if openURL(link) == nil {
			t.Errorf("unsafe link accepted: %s", link)
		}
	}
}

func TestAgentReviewSuggestionsRequireVerifiedEvidence(t *testing.T) {
	s, pid := fixture(t)
	_ = s.Store.Update(func(st *State) error { st.Projects[0].Candidates[0].Decision = "pending"; return nil })
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("invalid model request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"suggestion\":\"include\",\"confidence\":\"high\",\"reason\":\"yes\",\"evidence\":\"invented sentence\"}"}}]}`))}, nil
	})}
	t.Setenv("DBA_TEST_KEY", "test-token")
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "test", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "test-model", APIKeyEnv: "DBA_TEST_KEY"}})
	if e := s.AgentReview(context.Background(), pid, ""); e != nil {
		t.Fatal(e)
	}
	c := s.Store.Snapshot().Projects[0].Candidates[0]
	if c.Decision != "pending" || c.Suggestion != "uncertain" || c.Evidence != "" {
		t.Fatal("unverifiable batch suggestion changed a decision")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDiscoveryDeduplicationAndRecordedLimits(t *testing.T) {
	s, pid := fixture(t)
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("pageSize") != "2" {
			t.Error("fetch limit not applied")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"hitCount":25,"resultList":{"result":[{"id":"1","source":"MED","title":"Dataset one","abstractText":"abstract"},{"id":"2","source":"MED","title":"New candidate","abstractText":"abstract","doi":"10.1/example"}]}}`)), Header: make(http.Header)}, nil
	})}
	result, e := s.Search(context.Background(), Request{ProjectID: pid, Query: "broad query", Source: "Europe PMC", Limit: 2})
	if e != nil {
		t.Fatal(e)
	}
	r := result.(SearchRecord)
	if r.Total != 25 || r.Fetched != 2 || r.Added != 1 {
		t.Fatalf("incorrect counts: %#v", r)
	}
	if len(s.Store.Snapshot().Projects[0].Searches) != 1 {
		t.Fatal("missing search provenance")
	}
}

// fakeModel answers with a tool call on the first request and a final text on the second, recording request bodies.
func fakeModel(t *testing.T, firstBody, secondBody string, finalBody ...string) *[]map[string]any {
	t.Helper()
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	seen := &[]map[string]any{}
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["_path"], body["_auth"], body["_key"], body["_goog"] = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("x-api-key"), r.Header.Get("x-goog-api-key")
		*seen = append(*seen, body)
		reply := secondBody
		if len(*seen) == 1 {
			reply = firstBody
		} else if len(*seen) > 2 && len(finalBody) > 0 {
			reply = finalBody[0]
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(reply))}, nil
	})}
	return seen
}

func TestAgentLoopScopesToolsAndRecordsTrace(t *testing.T) {
	s, pid := fixture(t)
	t.Setenv("DBA_TEST_KEY", "k")
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "m", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "x", APIKeyEnv: "DBA_TEST_KEY"}})
	mid := s.Store.Snapshot().Settings.Models[0].ID
	action(t, s, "saveAgent", Request{Agent: Agent{ID: "scout", System: "custom instructions", ModelID: mid, MaxSteps: 3}})
	seen := fakeModel(t, `{"choices":[{"finish_reason":"tool_calls","message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"list_candidates","arguments":"{\"decision\":\"all\"}"}},{"id":"c2","type":"function","function":{"name":"get_job","arguments":"{\"id\":\"GSE1\"}"}}]}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"建议收窄检索式。"}}]}`)
	action(t, s, "message", Request{ProjectID: pid, AgentID: "scout", Message: "评估检索式"})
	msgs := s.Store.Snapshot().Projects[0].Messages
	if len(msgs) != 3 || msgs[0].Agent != "assistant" || msgs[1].Kind != "delegation" || msgs[1].Agent != "scout" || msgs[1].Content != "建议收窄检索式。" || len(msgs[1].Trace) != 2 || msgs[2].Agent != "assistant" {
		t.Fatalf("loop result not persisted: %#v", msgs)
	}
	if msgs[1].Trace[0].Error || !strings.Contains(msgs[1].Trace[0].Result, "Dataset one") || !msgs[1].Trace[1].Error {
		t.Fatalf("tool scope not enforced: %#v", msgs[1].Trace)
	}
	first := (*seen)[0]
	if first["_path"] != "/v1/chat/completions" || first["_auth"] != "Bearer k" || !strings.HasPrefix(first["messages"].([]any)[0].(map[string]any)["content"].(string), "custom instructions") {
		t.Fatalf("invalid first request: %v", first)
	}
	second := (*seen)[1]["messages"].([]any)
	if len(second) != 6 || second[len(second)-2].(map[string]any)["role"] != "tool" || second[len(second)-1].(map[string]any)["tool_call_id"] != "c2" {
		t.Fatalf("tool results not fed back: %v", second)
	}
	// Reset restores preset instructions but keeps the chosen model.
	action(t, s, "resetAgent", Request{AgentID: "scout"})
	a, _ := agentByID(ptr(s.Store.Snapshot()), "scout")
	if a.System == "custom instructions" || a.ModelID != mid {
		t.Fatal("reset mishandled")
	}
	action(t, s, "deleteModel", Request{ModelID: mid})
	if a, _ = agentByID(ptr(s.Store.Snapshot()), "scout"); a.ModelID != "" {
		t.Fatal("dangling model reference")
	}
}

func ptr(st State) *State { return &st }

func TestAnthropicAndGeminiProtocolRoundTrip(t *testing.T) {
	s, pid := fixture(t)
	t.Setenv("DBA_TEST_KEY", "k")
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "claude", Protocol: "anthropic", BaseURL: "https://api.anthropic.com/v1", Model: "claude-opus-5-5", APIKeyEnv: "DBA_TEST_KEY"}})
	seen := fakeModel(t, `{"stop_reason":"tool_use","content":[{"type":"text","text":"看一下任务。"},{"type":"tool_use","id":"tu1","name":"list_jobs","input":{}}]}`, `{"stop_reason":"end_turn","content":[{"type":"text","text":"没有任务。"}]}`)
	action(t, s, "message", Request{ProjectID: pid, AgentID: "operator", Message: "任务情况？"})
	first, second := (*seen)[0], (*seen)[1]
	if first["_path"] != "/v1/messages" || first["_key"] != "k" || first["system"] == nil || first["max_tokens"] == nil || len(first["tools"].([]any)) != len(presetAgents[4].Tools) {
		t.Fatalf("invalid anthropic request: %v", first)
	}
	msgs := second["messages"].([]any)
	result := msgs[len(msgs)-1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if len(msgs) != 4 || msgs[len(msgs)-2].(map[string]any)["role"] != "assistant" || result["type"] != "tool_result" || result["tool_use_id"] != "tu1" {
		t.Fatalf("anthropic tool_result not shaped: %v", msgs)
	}
	if m := s.Store.Snapshot().Projects[0].Messages; m[1].Content != "没有任务。" || m[1].Trace[0].Tool != "list_jobs" {
		t.Fatalf("anthropic reply lost: %#v", m)
	}

	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "gemini", Protocol: "gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta", Model: "gemini-x", APIKeyEnv: "DBA_TEST_KEY"}})
	gid := s.Store.Snapshot().Settings.Models[1].ID
	action(t, s, "saveAgent", Request{Agent: Agent{ID: "pipeline", System: "x", ModelID: gid, MaxSteps: 2}})
	seen = fakeModel(t, `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"functionCall":{"name":"get_pipeline","args":{}}}]}}]}`, `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"流程只有一步。"}]}}]}`, `{"stop_reason":"end_turn","content":[{"type":"text","text":"已汇总流程检查结果。"}]}`)
	action(t, s, "message", Request{ProjectID: pid, AgentID: "pipeline", Message: "审阅流程"})
	first, second = (*seen)[0], (*seen)[1]
	if first["_path"] != "/v1beta/models/gemini-x:generateContent" || first["_goog"] != "k" || first["system_instruction"] == nil {
		t.Fatalf("invalid gemini request: %v", first)
	}
	contents := second["contents"].([]any)
	part := contents[len(contents)-1].(map[string]any)["parts"].([]any)[0].(map[string]any)
	if len(contents) < 3 || contents[len(contents)-2].(map[string]any)["role"] != "model" || part["functionResponse"] == nil {
		t.Fatalf("gemini functionResponse not shaped: %v", contents)
	}
	if m := s.Store.Snapshot().Projects[0].Messages; m[len(m)-2].Content != "流程只有一步。" || !strings.Contains(m[len(m)-2].Trace[0].Result, "user workflow") {
		t.Fatalf("gemini reply lost: %#v", m)
	}
}

func TestSchemaOneMigration(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"schema":1,"projects":[{"id":"p","name":"old","messages":[{"role":"user","content":"hi"}]}],"settings":{"intervalSeconds":60,"maxRetries":3,"modelUrl":"https://old.example/v1","model":"old-model","apiKeyEnv":"OLD_KEY"}}`
	if e := os.WriteFile(filepath.Join(dir, "workbench.json"), []byte(legacy), 0600); e != nil {
		t.Fatal(e)
	}
	store, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	st := store.Snapshot()
	if st.Schema != SchemaVersion || len(st.Settings.Models) != 1 || st.Settings.Models[0].Protocol != "openai" || st.Settings.Models[0].Model != "old-model" || st.Settings.Models[0].APIKeyEnv != "OLD_KEY" {
		t.Fatalf("legacy model not migrated: %#v", st.Settings)
	}
	if len(st.Settings.Agents) != len(presetAgents) || st.Projects[0].Messages[0].Agent != "assistant" {
		t.Fatalf("presets or message tags missing: %#v", st.Settings.Agents)
	}
}
