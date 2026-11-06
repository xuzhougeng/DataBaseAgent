package workbench

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// statusSequence answers each request with the next status code, then repeats the last one.
func statusSequence(t *testing.T, codes ...int) *int {
	t.Helper()
	previous, previousBackoff := httpClient, llmBackoff
	t.Cleanup(func() { httpClient, llmBackoff = previous, previousBackoff })
	llmBackoff = 0
	calls := new(int)
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		code := codes[min(*calls, len(codes)-1)]
		*calls++
		return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})}
	return calls
}

func TestPostJSONRetriesOnlyTransientFailures(t *testing.T) {
	var out struct{ OK bool }
	calls := statusSequence(t, 503, 429, 200)
	if e := postJSON(context.Background(), "https://model.example/v1/x", "k", nil, map[string]any{}, &out); e != nil || !out.OK || *calls != 3 {
		t.Fatalf("transient statuses not retried: calls=%d err=%v", *calls, e)
	}

	calls = statusSequence(t, 400)
	if e := postJSON(context.Background(), "https://model.example/v1/x", "k", nil, map[string]any{}, &out); e == nil || *calls != 1 {
		t.Fatalf("client error must not retry: calls=%d err=%v", *calls, e)
	}

	calls = statusSequence(t, 500)
	if e := postJSON(context.Background(), "https://model.example/v1/x", "k", nil, map[string]any{}, &out); e == nil || !strings.Contains(e.Error(), "HTTP 500") || *calls != 1+llmRetries {
		t.Fatalf("retry budget not respected: calls=%d err=%v", *calls, e)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = statusSequence(t, 503)
	if e := postJSON(ctx, "https://model.example/v1/x", "k", nil, map[string]any{}, &out); e == nil || *calls > 1 {
		t.Fatalf("cancelled context must not retry: calls=%d err=%v", *calls, e)
	}
}

func TestResponsesProtocolRoundTrip(t *testing.T) {
	s, pid := fixture(t)
	t.Setenv("DBA_TEST_KEY", "k")
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "gpt", Protocol: "openai-responses", BaseURL: "https://api.openai.com/v1", Model: "gpt-x", APIKeyEnv: "DBA_TEST_KEY"}})
	seen := fakeModel(t,
		`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"看一下任务。"}]},{"type":"function_call","call_id":"call1","name":"list_jobs","arguments":"{}"}]}`,
		`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"没有任务。"}]}]}`)
	action(t, s, "message", Request{ProjectID: pid, AgentID: "operator", Message: "任务情况？"})
	first, second := (*seen)[0], (*seen)[1]
	tools := first["tools"].([]any)
	if first["_path"] != "/v1/responses" || first["_auth"] != "Bearer k" || first["instructions"] == nil || len(tools) != len(presetAgents[4].Tools) {
		t.Fatalf("invalid responses request: %v", first)
	}
	if tool := tools[0].(map[string]any); tool["name"] == nil || tool["parameters"] == nil || tool["strict"] != false {
		t.Fatalf("function tool not flat: %v", tool)
	}
	input := second["input"].([]any)
	call, result := input[len(input)-2].(map[string]any), input[len(input)-1].(map[string]any)
	if len(input) < 4 || input[0].(map[string]any)["role"] != "user" || call["type"] != "function_call" || call["call_id"] != "call1" || result["type"] != "function_call_output" || result["call_id"] != "call1" {
		t.Fatalf("function_call_output not shaped: %v", input)
	}
	if m := s.Store.Snapshot().Projects[0].Messages; m[1].Content != "没有任务。" || m[1].Trace[0].Tool != "list_jobs" {
		t.Fatalf("responses reply lost: %#v", m)
	}
}

func TestResponsesIncompleteMapsToMaxTokens(t *testing.T) {
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"部分"}]}]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	reply, e := complete(context.Background(), ModelProfile{Protocol: "openai-responses", BaseURL: "https://model.example/v1", Model: "m"}, "", "sys", []Turn{{Role: "user", Text: "hi"}}, nil, false)
	if e != nil || reply.Text != "部分" || reply.Stop != "max_tokens" {
		t.Fatalf("incomplete reply not normalized: %#v %v", reply, e)
	}
}
