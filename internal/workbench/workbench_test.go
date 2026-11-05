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

func TestAgentEvidenceAndHumanReviewGate(t *testing.T) {
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
	cfg := s.Store.Snapshot().Settings
	cfg.ModelURL = "https://model.example/v1"
	cfg.Model = "test-model"
	cfg.APIKeyEnv = "DBA_TEST_KEY"
	action(t, s, "settings", Request{Settings: cfg})
	if e := s.AgentReview(context.Background(), pid, ""); e != nil {
		t.Fatal(e)
	}
	c := s.Store.Snapshot().Projects[0].Candidates[0]
	if c.Decision != "pending" || c.Suggestion != "uncertain" || c.Evidence != "" {
		t.Fatal("unverifiable model output bypassed human gate")
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
