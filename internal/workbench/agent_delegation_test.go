package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRequestedSubagents(t *testing.T) {
	state := State{Settings: Settings{Agents: withPresets(nil)}}
	for _, tc := range []struct {
		message, legacy string
		want            []string
		invalid         bool
	}{
		{"@scout @复核员 @scout 请协作", "", []string{"scout", "reviewer"}, false},
		{"请 @运维巡检员 检查", "operator", []string{"operator"}, false},
		{"@assistant @项目助手 你好", "", []string{}, false},
		{"mail user@scout.com; `@reviewer`\n```\n@pipeline\n```", "", []string{}, false},
		{"@missing 检查", "", nil, true},
		{"hello", "missing", nil, true},
	} {
		t.Run(tc.message, func(t *testing.T) {
			got, err := requestedAgents(&state, tc.legacy, tc.message)
			if (err != nil) != tc.invalid || !tc.invalid && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func delegationModels(t *testing.T, s *Service, ids ...string) {
	t.Helper()
	for _, aid := range append([]string{"assistant"}, ids...) {
		action(t, s, "saveModel", Request{Model: ModelProfile{Name: aid, Protocol: "openai", BaseURL: "https://model.example/v1", Model: aid}})
		models := s.Store.Snapshot().Settings.Models
		callTool(t, s, "", "assistant", "save_agent", map[string]any{"agent": map[string]any{"id": aid, "modelId": models[len(models)-1].ID, "maxSteps": 3}})
	}
}

func mockDelegationModel(t *testing.T, respond func(model string, body map[string]any) (string, error)) {
	t.Helper()
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		reply, err := respond(body["model"].(string), body)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(reply))}, nil
	})}
}

func textReply(text string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
	return string(b)
}

func TestMentionsShareHistoryModelsProgressAndResults(t *testing.T) {
	s, pid := fixture(t)
	delegationModels(t, s, "scout", "reviewer")
	action(t, s, "createProject", Request{Name: "other"})
	if err := s.Store.Update(func(st *State) error {
		st.Projects[0].Messages = []Message{{Role: "user", Agent: "scout", Content: "earlier search goal"}, {Role: "assistant", Agent: "scout", Content: "earlier specialist result"}}
		st.Projects[1].Messages = []Message{{Role: "user", Agent: "assistant", Content: "unrelated secret context"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	order := []string{}
	mockDelegationModel(t, func(model string, body map[string]any) (string, error) {
		counts[model]++
		order = append(order, model)
		encoded, _ := json.Marshal(body)
		if !strings.Contains(string(encoded), "earlier specialist result") || strings.Contains(string(encoded), "unrelated secret context") {
			t.Fatal("shared history missing or another project's context leaked")
		}
		runs := s.AgentRuns()
		if len(runs) != 1 || runs[0].ProjectID != pid || runs[0].AgentID != "assistant" {
			t.Fatalf("missing coordinator progress: %#v", runs)
		}
		if model != "assistant" {
			if runs[0].SubagentID != model || runs[0].Task == "" {
				t.Fatalf("missing child progress: %#v", runs)
			}
			for _, raw := range body["tools"].([]any) {
				name := raw.(map[string]any)["function"].(map[string]any)["name"]
				if name == "delegate_agent" || name == "launch_jobs" {
					t.Fatal("child received coordinator privileges")
				}
			}
		} else if runs[0].SubagentID != "" {
			t.Fatal("child progress was not restored")
		}
		switch model {
		case "scout":
			if counts[model] == 1 {
				if err := s.Chat(context.Background(), pid, "", "concurrent request"); err == nil {
					t.Fatal("child released conversation lock")
				}
				return modelReply(toolCall("update_project", map[string]any{"criteria": "updated by scout"})), nil
			}
			return textReply("search completed"), nil
		case "reviewer":
			if s.Store.Snapshot().Projects[0].Criteria != "updated by scout" || !strings.Contains(string(encoded), "search completed") {
				t.Fatal("next child did not receive preceding child's result")
			}
			return textReply("review completed"), nil
		case "assistant":
			if !strings.Contains(string(encoded), "search completed") || !strings.Contains(string(encoded), "review completed") {
				t.Fatal("main agent did not receive both tool results")
			}
			return textReply("combined result"), nil
		}
		t.Fatalf("unexpected model %q", model)
		return "", nil
	})
	if err := s.Chat(context.Background(), pid, "", "@scout @reviewer @scout 请协作"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"scout", "scout", "reviewer", "assistant"}) {
		t.Fatalf("wrong execution order: %v", order)
	}
	messages := s.Store.Snapshot().Projects[0].Messages
	if len(messages) != 6 || messages[2].Agent != "assistant" || messages[3].Kind != "delegation" || len(messages[3].Trace) != 1 || messages[4].Agent != "reviewer" || messages[5].Content != "combined result" || len(messages[5].Trace) != 2 || len(s.AgentRuns()) != 0 {
		t.Fatalf("incomplete shared conversation: %#v", messages)
	}
	reopened, err := Open(s.Store.Root())
	if err != nil || !reflect.DeepEqual(reopened.Snapshot().Projects[0].Messages, messages) {
		t.Fatalf("child results did not survive restart: %v", err)
	}
}

func TestMainAgentDelegatesAutonomously(t *testing.T) {
	s, pid := fixture(t)
	delegationModels(t, s, "reviewer")
	cid := s.Store.Snapshot().Projects[0].Candidates[0].ID
	counts := map[string]int{}
	mockDelegationModel(t, func(model string, body map[string]any) (string, error) {
		counts[model]++
		if model == "assistant" && counts[model] == 1 {
			return modelReply(toolCall("delegate_agent", map[string]any{"agentId": "reviewer", "task": "核对候选并记录理由"})), nil
		}
		if model == "reviewer" && counts[model] == 1 {
			return modelReply(toolCall("review_candidate", map[string]any{"candidateId": cid, "note": "A real source sentence.", "decision": "accepted"})), nil
		}
		if model == "reviewer" {
			return textReply("reviewed with evidence"), nil
		}
		encoded, _ := json.Marshal(body)
		if !strings.Contains(string(encoded), "reviewed with evidence") {
			t.Fatal("coordinator did not receive child result")
		}
		return textReply("review complete"), nil
	})
	if err := s.Chat(context.Background(), pid, "assistant", "请按标准复核"); err != nil {
		t.Fatal(err)
	}
	p := s.Store.Snapshot().Projects[0]
	if p.Candidates[0].ReviewedBy != "agent:reviewer" || len(p.Messages) != 3 || p.Messages[1].Agent != "reviewer" || p.Messages[2].Trace[0].Tool != "delegate_agent" {
		t.Fatalf("autonomous delegation was not executed: %#v", p)
	}
}

func TestChildFailureRetainsWritesAndAllowsMainToRespond(t *testing.T) {
	s, pid := fixture(t)
	delegationModels(t, s, "scout")
	count := 0
	mockDelegationModel(t, func(model string, body map[string]any) (string, error) {
		if model == "scout" {
			count++
			if count == 1 {
				return modelReply(toolCall("update_project", map[string]any{"description": "saved child change"})), nil
			}
			return "", errors.New("child connection lost")
		}
		encoded, _ := json.Marshal(body)
		if !strings.Contains(string(encoded), "child connection lost") {
			t.Fatal("main agent did not receive failure")
		}
		return textReply("partial result explained"), nil
	})
	if err := s.Chat(context.Background(), pid, "", "@scout 更新项目"); err != nil {
		t.Fatal(err)
	}
	p := s.Store.Snapshot().Projects[0]
	if p.Description != "saved child change" || !p.Messages[1].Error || len(p.Messages[1].Trace) != 1 || !p.Messages[2].Trace[0].Error || p.Messages[2].Content != "partial result explained" {
		t.Fatalf("child failure lost progress: %#v", p.Messages)
	}
}

func TestDelegationGuardsAndCancellation(t *testing.T) {
	s, pid := fixture(t)
	delegationModels(t, s, "scout")
	state := s.Store.Snapshot()
	main, _ := agentByID(&state, "assistant")
	scout, _ := agentByID(&state, "scout")
	ctx := context.WithValue(context.Background(), chatExecutionKey{}, &chatExecution{ProjectID: pid, Delegated: 8})
	for _, tc := range []struct {
		ctx   context.Context
		agent Agent
		args  map[string]any
	}{
		{ctx, *main, map[string]any{"agentId": "scout", "task": "over budget"}},
		{context.Background(), *main, map[string]any{"agentId": "scout", "task": "outside chat"}},
		{ctx, *scout, map[string]any{"agentId": "reviewer", "task": "recursive"}},
		{ctx, *main, map[string]any{"agentId": "assistant", "task": "self"}},
	} {
		if _, failed := s.runTool(tc.ctx, pid, tc.agent, toolCall("delegate_agent", tc.args)); !failed {
			t.Fatal("invalid delegation was allowed")
		}
	}
	if err := s.Chat(context.Background(), pid, "", "@unknown hi"); err == nil || len(s.Store.Snapshot().Projects[0].Messages) != 0 {
		t.Fatal("unknown mention was accepted or saved")
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := 0
	mockDelegationModel(t, func(model string, body map[string]any) (string, error) {
		requests++
		cancel()
		return "", context.Canceled
	})
	if err := s.Chat(cancelCtx, pid, "", "@scout @reviewer 检查"); err == nil {
		t.Fatal("cancellation was swallowed")
	}
	messages := s.Store.Snapshot().Projects[0].Messages
	if requests != 1 || len(messages) != 3 || !messages[1].Error || messages[2].Status != "cancelled" || len(s.AgentRuns()) != 0 {
		t.Fatalf("cancellation lost trace or leaked run: %#v", messages)
	}
}
