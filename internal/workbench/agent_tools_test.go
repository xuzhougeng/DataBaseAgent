package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func toolCall(name string, args any) ToolCall {
	b, _ := json.Marshal(args)
	return ToolCall{ID: name, Name: name, Args: b}
}

func callTool(t *testing.T, s *Service, pid, aid, name string, args any) string {
	t.Helper()
	a, _ := agentByID(ptr(s.Store.Snapshot()), aid)
	out, failed := s.runTool(context.Background(), pid, *a, toolCall(name, args))
	if failed {
		t.Fatalf("%s failed: %s", name, out)
	}
	return out
}

func modelReply(calls ...ToolCall) string {
	message := map[string]any{"content": "任务已创建，等待巡检调度。"}
	if len(calls) > 0 {
		items := []any{}
		for _, c := range calls {
			items = append(items, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Args)}})
		}
		message = map[string]any{"tool_calls": items}
	}
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": message}}})
	return string(b)
}

func TestAssistantCompletesWorkflowFromWorkspaceConversation(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(store)
	s.autoApprove = true
	t.Cleanup(s.Stop)
	s.Runner = &fakeExecutor{observation: Observation{Status: "succeeded", Log: "done"}}
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "test", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "test"}})
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	step := 0
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		reply := ""
		if r.Method == "GET" {
			if r.URL.Query().Get("query") != "test query" {
				t.Error("search did not use the project settings saved in the same tool batch")
			}
			reply = `{"hitCount":1,"resultList":{"result":[{"id":"1","source":"MED","title":"Test dataset","abstractText":"Two groups with verified counts."}]}}`
		} else {
			runs := s.AgentRuns()
			if len(runs) != 1 || runs[0].ProjectID != "" || runs[0].Step != step+1 {
				t.Fatalf("missing live progress: %#v", runs)
			}
			st := store.Snapshot()
			switch step {
			case 0:
				reply = modelReply(toolCall("create_project", map[string]any{"name": "Agent project", "description": "Build counts"}), toolCall("add_server", map[string]any{"server": map[string]any{"name": "test", "host": "example.org", "user": "research", "workDir": "/home/research/dba", "cpuQuota": 8, "maxJobs": 1}}))
			case 1:
				pid := st.Projects[0].ID
				reply = modelReply(toolCall("update_project", map[string]any{"projectId": pid, "criteria": "Verified counts", "query": "test query"}), toolCall("search", map[string]any{"projectId": pid, "limit": 2}))
			case 2:
				p := st.Projects[0]
				reply = modelReply(toolCall("review_candidate", map[string]any{"projectId": p.ID, "candidateId": p.Candidates[0].ID, "decision": "accepted", "note": "Two groups with verified counts.", "accession": "GSE_TEST"}), toolCall("save_pipeline", map[string]any{"projectId": p.ID, "pipeline": map[string]any{"serverId": st.Servers[0].ID, "cpus": 4, "steps": []any{map[string]any{"name": "process", "tool": "test@1", "command": "printf 'gene\\tcount\\nA\\t1\\n' > expression.tsv"}}, "outputs": []string{"expression.tsv"}}}), toolCall("lock_pipeline", map[string]any{"projectId": p.ID}), toolCall("launch_jobs", map[string]any{"projectId": p.ID}))
			case 3:
				if len(st.Jobs) != 1 || st.Jobs[0].Status != "queued" {
					t.Fatalf("consecutive writes did not queue the job: %#v", st.Jobs)
				}
				reply = modelReply()
			default:
				t.Fatal("unexpected extra model turn")
			}
			step++
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(reply))}, nil
	})}
	if err := s.Chat(context.Background(), "", "assistant", "创建数据库并完成处理流程"); err != nil {
		t.Fatal(err)
	}
	st := store.Snapshot()
	if len(st.Messages) != 2 || len(st.Messages[1].Trace) != 8 || len(s.AgentRuns()) != 0 {
		t.Fatalf("conversation or trace missing: %#v", st.Messages)
	}
	for _, trace := range st.Messages[1].Trace {
		if trace.Error {
			t.Fatalf("failed workflow step: %#v", trace)
		}
	}
	if st.Projects[0].Candidates[0].ReviewedBy != "agent:assistant" || st.Projects[0].Description != "Build counts" {
		t.Fatal("review attribution or partial update was lost")
	}
	tickWait(t, s)
	tickWait(t, s)
	tickWait(t, s)
	st = store.Snapshot()
	if st.Jobs[0].Status != "succeeded" || len(st.Artifacts) != 1 {
		t.Fatal("agent-created job failed to reach archived outputs")
	}
	callTool(t, s, "", "assistant", "export_manifest", map[string]any{"projectId": st.Projects[0].ID})
	// Restart retains the workspace conversation as well as the regular project data.
	reopened, err := Open(store.Root())
	if err != nil || len(reopened.Snapshot().Messages) != 2 {
		t.Fatalf("workspace conversation did not survive restart: %v", err)
	}
}

func TestToolScopesValidationAndPartialUpdates(t *testing.T) {
	s, pid := fixture(t)
	a, _ := agentByID(ptr(s.Store.Snapshot()), "assistant")
	st := s.Store.Snapshot()
	cid := st.Projects[0].Candidates[0].ID
	callTool(t, s, pid, "assistant", "review_candidate", map[string]any{"candidateId": cid, "note": "updated reason"})
	c := s.Store.Snapshot().Projects[0].Candidates[0]
	if c.Accession != "GSE1" || c.Decision != "accepted" || c.ReviewNote != "updated reason" {
		t.Fatal("partial review erased existing fields")
	}
	callTool(t, s, pid, "assistant", "save_pipeline", map[string]any{"pipeline": map[string]any{"cpus": 2}})
	p := s.Store.Snapshot().Projects[0]
	if p.Draft.Locked || p.Draft.CPUs != 2 || p.Draft.Steps[0].Command != st.Projects[0].Draft.Steps[0].Command || p.Versions[0].CPUs != 4 {
		t.Fatal("partial pipeline edit damaged an immutable version or omitted fields")
	}
	callTool(t, s, "", "assistant", "update_settings", map[string]any{"settings": map[string]any{"paused": true}})
	if settings := s.Store.Snapshot().Settings; !settings.Paused || settings.IntervalSeconds != 60 || settings.MaxRetries != 3 {
		t.Fatal("partial settings edit erased defaults")
	}
	for _, c := range []ToolCall{
		toolCall("review_candidate", map[string]any{"candidateId": cid}),
		toolCall("review_candidate", map[string]any{"candidateId": cid, "note": "", "decision": "accepted"}),
		toolCall("update_settings", map[string]any{"settings": map[string]any{"intervalSeconds": 1}}),
		toolCall("update_project", map[string]any{"name": 12}),
		toolCall("update_project", map[string]any{"description": nil}),
		toolCall("save_pipeline", map[string]any{"pipeline": map[string]any{"locked": true}}),
		toolCall("save_model", map[string]any{"model": map[string]any{"apiKey": "must-not-store"}}),
		toolCall("get_candidate", map[string]any{"id": "does-not-exist"}),
		toolCall("message", map[string]any{}),
		{Name: "update_project", Args: json.RawMessage("null")},
	} {
		if out, failed := s.runTool(context.Background(), pid, *a, c); !failed {
			t.Errorf("invalid tool call accepted: %s %s", c.Name, out)
		}
	}
	scout, _ := agentByID(ptr(s.Store.Snapshot()), "scout")
	if _, failed := s.runTool(context.Background(), pid, *scout, toolCall("launch_jobs", map[string]any{})); !failed {
		t.Fatal("specialist escaped its tool scope")
	}
	if _, failed := s.runTool(context.Background(), "", *a, toolCall("update_project", map[string]any{"name": "no project"})); !failed {
		t.Fatal("project command without a project succeeded")
	}
	action(t, s, "lockPipeline", Request{ProjectID: pid, Pipeline: p.Draft})
	action(t, s, "launch", Request{ProjectID: pid})
	action(t, s, "createProject", Request{Name: "other"})
	st = s.Store.Snapshot()
	if _, failed := s.runTool(context.Background(), st.Projects[1].ID, *a, toolCall("resolve_job", map[string]any{"jobId": st.Jobs[0].ID, "note": "fixed"})); !failed {
		t.Fatal("job from another project was accepted")
	}
}

func TestAgentFailureKeepsCompletedWritesAndTrace(t *testing.T) {
	s, pid := fixture(t)
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "test", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "test"}})
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	n := 0
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n++
		if n > 1 {
			return nil, errors.New("model connection lost")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(modelReply(toolCall("update_project", map[string]any{"description": "saved before failure"}))))}, nil
	})}
	if s.Chat(context.Background(), pid, "assistant", "更新项目") == nil {
		t.Fatal("expected model failure")
	}
	p := s.Store.Snapshot().Projects[0]
	if p.Description != "saved before failure" || len(p.Messages) != 2 || len(p.Messages[1].Trace) != 1 || !strings.Contains(p.Messages[1].Content, "中断") || len(s.AgentRuns()) != 0 {
		t.Fatalf("partial execution was not retained: %#v", p.Messages)
	}
}

func TestPresetMigrationKeepsCustomizations(t *testing.T) {
	stored := []Agent{{ID: "assistant", System: legacyAgentSystems["assistant"], MaxSteps: 6, ModelID: "saved-model"}, {ID: "scout", System: "custom prompt", MaxSteps: 3}}
	agents := withPresets(stored)
	if agents[0].System != presetAgents[0].System || agents[0].MaxSteps != 20 || agents[0].ModelID != "saved-model" || len(agents[0].Tools) != len(tools) {
		t.Fatal("old defaults did not adopt executable tools and instructions")
	}
	if agents[1].System != "custom prompt" || agents[1].MaxSteps != 3 {
		t.Fatal("custom configuration was overwritten")
	}
}

func TestConfigurationToolsPreserveCredentialsAndAgentInstructions(t *testing.T) {
	s, pid := fixture(t)
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "original", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "test"}})
	if err := s.Store.Update(func(st *State) error {
		st.Settings.Models[0].APIKeyRef = "private-credential-reference"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st := s.Store.Snapshot()
	mid := st.Settings.Models[0].ID
	callTool(t, s, pid, "assistant", "save_model", map[string]any{"model": map[string]any{"id": mid, "name": "renamed"}})
	m := s.Store.Snapshot().Settings.Models[0]
	if m.Name != "renamed" || m.APIKeyRef != "private-credential-reference" || m.BaseURL != "https://model.example/v1" {
		t.Fatal("partial model update erased credentials or endpoint")
	}
	if out := callTool(t, s, pid, "assistant", "get_settings", map[string]any{}); strings.Contains(out, "private-credential-reference") {
		t.Fatal("configuration tool disclosed a credential reference")
	}
	callTool(t, s, pid, "assistant", "save_agent", map[string]any{"agent": map[string]any{"id": "scout", "modelId": mid}})
	a, _ := agentByID(ptr(s.Store.Snapshot()), "scout")
	if a.ModelID != mid || a.System != st.Settings.Agents[1].System || a.MaxSteps != st.Settings.Agents[1].MaxSteps {
		t.Fatal("model assignment erased agent instructions or budget")
	}
}

func TestConcurrentChatInSameProjectIsRejected(t *testing.T) {
	s, pid := fixture(t)
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "test", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "test"}})
	previous := httpClient
	started, release := make(chan struct{}, 1), make(chan struct{})
	t.Cleanup(func() { httpClient = previous })
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(modelReply()))}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Chat(ctx, pid, "assistant", "first") }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("first chat did not start")
	}
	err := s.Chat(context.Background(), pid, "scout", "second")
	close(release)
	if firstErr := <-done; firstErr != nil {
		t.Fatal(firstErr)
	}
	if err == nil || !strings.Contains(err.Error(), "正在执行") || len(s.Store.Snapshot().Projects[0].Messages) != 2 {
		t.Fatal("concurrent conversation was not rejected cleanly")
	}
}
