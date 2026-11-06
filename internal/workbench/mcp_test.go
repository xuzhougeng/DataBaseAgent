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
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
)

func saveTestMCP(t *testing.T, s *Service, v MCPServer) MCPServer {
	t.Helper()
	action(t, s, "saveMCPServer", Request{MCPServer: v})
	for _, saved := range s.Store.Snapshot().Settings.MCPServers {
		if saved.Name == v.Name {
			return saved
		}
	}
	t.Fatal("server not saved")
	return MCPServer{}
}

func TestMCPDefaultsMigrationAndDeletion(t *testing.T) {
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defaults := st.Snapshot().Settings.MCPServers
	if len(defaults) != 2 || defaults[0].ID != "github" || defaults[1].ID != "cloudflare" {
		t.Fatalf("defaults: %#v", defaults)
	}
	// Reopening a schema-3 workspace without the new field migrates once.
	raw, _ := os.ReadFile(filepath.Join(root, "workbench.json"))
	var old map[string]any
	_ = json.Unmarshal(raw, &old)
	delete(old["settings"].(map[string]any), "mcpServers")
	raw, _ = json.Marshal(old)
	if err := os.WriteFile(filepath.Join(root, "workbench.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	st, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(st)
	defer s.Stop()
	for _, v := range defaults {
		action(t, s, "deleteMCPServer", Request{MCPServerID: v.ID})
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Snapshot().Settings.MCPServers) != 0 {
		t.Fatal("deleted defaults resurrected")
	}
}

func TestMCPCredentialsAndRollback(t *testing.T) {
	keyring.MockInit()
	s, _ := fixture(t)
	v := s.Store.Snapshot().Settings.MCPServers[0]
	secret := "mcp-test-token"
	action(t, s, "saveMCPServer", Request{MCPServer: v, APIKey: &secret})
	v = s.Store.Snapshot().Settings.MCPServers[0]
	key, err := s.Store.mcpToken(v)
	if err != nil || key != secret {
		t.Fatal("saved token unavailable", err)
	}
	if v.TokenRef == "" || v.BearerTokenEnv != "" {
		t.Fatal("token not referenced correctly")
	}
	action(t, s, "saveMCPServer", Request{MCPServer: v})
	if s.Store.Snapshot().Settings.MCPServers[0].TokenRef != v.TokenRef {
		t.Fatal("omitted key did not preserve credential")
	}
	raw, _ := os.ReadFile(filepath.Join(s.Store.Root(), "workbench.json"))
	if strings.Contains(string(raw), secret) {
		t.Fatal("token in JSON store")
	}
	recorder := httptest.NewRecorder()
	Handler(s, fstest.MapFS{}).ServeHTTP(recorder, httptest.NewRequest("GET", "/api/state", nil))
	settings := callTool(t, s, "", "assistant", "get_settings", map[string]any{})
	for _, text := range []string{recorder.Body.String(), settings} {
		if strings.Contains(text, v.TokenRef) || strings.Contains(text, secret) {
			t.Fatal("credential exposed")
		}
	}
	// A failed workspace save must leave the previous token usable.
	path := filepath.Join(s.Store.Root(), "workbench.json")
	if err := os.Rename(path, path+".bak"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	replacement := "replacement-token"
	body, _ := json.Marshal(Request{MCPServer: v, APIKey: &replacement})
	if _, err := s.Dispatch(context.Background(), "saveMCPServer", body); err == nil {
		t.Fatal("expected persistence failure")
	}
	key, err = s.Store.mcpToken(v)
	if err != nil || key != secret {
		t.Fatal("old token lost on rollback")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".bak", path); err != nil {
		t.Fatal(err)
	}
	action(t, s, "deleteMCPServer", Request{MCPServerID: v.ID})
	if _, err := keyring.Get(modelKeyService, s.Store.modelKeyAccount(v.TokenRef)); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("deleted token retained")
	}
}

func mcpFixtureServer(calls *atomic.Int32) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, &mcp.ServerOptions{PageSize: 1, Instructions: "Use echo for testing."})
	server.AddTool(&mcp.Tool{Name: "echo", Description: "Echo structured arguments", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}, "required": []string{"message"}}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		var args struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(r.Params.Arguments, &args)
		if args.Message == "slow" {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				// Stateless servers may finish work even after the client times out.
				return &mcp.CallToolResult{Content: []mcp.Content{}}, nil
			}
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: args.Message}}, StructuredContent: map[string]any{"message": args.Message}, IsError: args.Message == "fail"}, nil
	})
	server.AddTool(&mcp.Tool{Name: "forbidden", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{}, nil
	})
	return server
}

func TestMCPHTTPDiscoveryAndCalls(t *testing.T) {
	for _, stateless := range []bool{false, true} {
		for _, jsonResponse := range []bool{false, true} {
			t.Run(string([]byte{byte('0' + boolInt(stateless)), byte('0' + boolInt(jsonResponse))}), func(t *testing.T) {
				s, _ := fixture(t)
				var calls atomic.Int32
				server := mcpFixtureServer(&calls)
				handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: stateless, JSONResponse: jsonResponse})
				t.Setenv("MCP_TEST_TOKEN", "secret-fixture-token")
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer secret-fixture-token" {
						t.Error("missing bearer token")
						w.WriteHeader(401)
						return
					}
					handler.ServeHTTP(w, r)
				}))
				defer remote.Close()
				v := saveTestMCP(t, s, MCPServer{Name: "test", Transport: "http", URL: remote.URL, Enabled: true, BearerTokenEnv: "MCP_TEST_TOKEN", DisabledTools: []string{"forbidden"}, ToolTimeoutSec: 1})
				a, _ := agentByID(ptr(s.Store.Snapshot()), "assistant")
				run := &mcpRun{connections: map[string]*mcpConnection{}}
				defer run.close()
				ctx := context.WithValue(context.Background(), mcpRunKey{}, run)
				listing, failed := s.runTool(ctx, "", *a, toolCall("list_mcp_tools", map[string]any{"serverId": v.ID}))
				if failed || !strings.Contains(listing, "inputSchema") || strings.Contains(listing, "forbidden") {
					t.Fatalf("discovery: %s", listing)
				}
				call := func(message string) (string, bool) {
					return s.runTool(ctx, "", *a, toolCall("call_mcp_tool", map[string]any{"serverId": v.ID, "tool": "echo", "arguments": map[string]any{"message": message}}))
				}
				output, failed := call("secret-fixture-token")
				if failed || !strings.Contains(output, "structuredContent") || strings.Contains(output, "secret-fixture-token") || !strings.Contains(output, "[redacted]") {
					t.Fatalf("call: %s", output)
				}
				if _, failed = call("fail"); !failed {
					t.Fatal("isError lost")
				}
				if _, failed = s.runTool(ctx, "", *a, toolCall("call_mcp_tool", map[string]any{"serverId": v.ID, "tool": "forbidden", "arguments": map[string]any{}})); !failed || calls.Load() != 2 {
					t.Fatal("disabled tool executed")
				}
				child := *a
				child.ID = "scout"
				if _, failed = s.runTool(ctx, "", child, toolCall("list_mcp_servers", map[string]any{})); !failed {
					t.Fatal("specialist bypassed MCP scope")
				}
				if len(run.connections) != 1 {
					t.Fatal("session not reused")
				}
				v.Enabled = false
				action(t, s, "saveMCPServer", Request{MCPServer: v})
				if _, failed = call("blocked"); !failed || calls.Load() != 2 {
					t.Fatal("disabled server still callable")
				}
			})
		}
	}
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestMCPTimeoutAndRedirect(t *testing.T) {
	s, _ := fixture(t)
	var calls atomic.Int32
	server := mcpFixtureServer(&calls)
	remote := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	defer remote.Close()
	v := saveTestMCP(t, s, MCPServer{Name: "timeout", Transport: "http", URL: remote.URL, Enabled: true, ToolTimeoutSec: 1})
	a, _ := agentByID(ptr(s.Store.Snapshot()), "assistant")
	start := time.Now()
	_, failed := s.runTool(context.Background(), "", *a, toolCall("call_mcp_tool", map[string]any{"serverId": v.ID, "tool": "echo", "arguments": map[string]any{"message": "slow"}}))
	if !failed || time.Since(start) > 4*time.Second || calls.Load() != 1 {
		t.Fatal("tool timeout/retry boundary failed")
	}
	var received atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	v.URL = redirect.URL
	action(t, s, "saveMCPServer", Request{MCPServer: v})
	if _, err := s.testMCPServer(context.Background(), v.ID); err == nil || received.Load() != 0 {
		t.Fatal("MCP redirect followed")
	}
}

// The test binary doubles as a real local STDIO MCP process.
func TestMCPStdioHelper(t *testing.T) {
	if os.Getenv("DBA_MCP_HELPER") != "1" {
		return
	}
	if os.Getenv("DBA_UNRELATED_SECRET") != "" {
		os.Exit(3)
	}
	var calls atomic.Int32
	if err := mcpFixtureServer(&calls).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestMCPStdioLifecycle(t *testing.T) {
	s, _ := fixture(t)
	t.Setenv("DBA_MCP_HELPER", "1")
	t.Setenv("DBA_UNRELATED_SECRET", "must-not-leak")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	v := saveTestMCP(t, s, MCPServer{Name: "local", Transport: "stdio", Command: executable, Args: []string{"-test.run=^TestMCPStdioHelper$"}, EnvVars: []string{"DBA_MCP_HELPER"}, Enabled: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := &mcpRun{connections: map[string]*mcpConnection{}}
	defer run.close()
	ctx = context.WithValue(ctx, mcpRunKey{}, run)
	a, _ := agentByID(ptr(s.Store.Snapshot()), "assistant")
	listing, failed := s.runTool(ctx, "", *a, toolCall("list_mcp_tools", map[string]any{"serverId": v.ID, "limit": 1, "offset": 1}))
	if failed || !strings.Contains(listing, "forbidden") {
		t.Fatalf("stdio pagination: %s", listing)
	}
	out, failed := s.runTool(ctx, "", *a, toolCall("call_mcp_tool", map[string]any{"serverId": v.ID, "tool": "echo", "arguments": map[string]any{"message": "stdio works"}}))
	if failed || !strings.Contains(out, "stdio works") {
		t.Fatalf("stdio call: %s", out)
	}
	c := run.connections[v.ID]
	run.close()
	delete(run.connections, v.ID)
	if _, err := c.session.ListTools(ctx, nil); err == nil {
		t.Fatal("session still open")
	}
}

func TestMCPAgentLoopTrace(t *testing.T) {
	s, _ := fixture(t)
	var calls atomic.Int32
	server := mcpFixtureServer(&calls)
	remote := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}))
	defer remote.Close()
	v := saveTestMCP(t, s, MCPServer{Name: "loop", Transport: "http", URL: remote.URL, Enabled: true})
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "test", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "test"}})
	previous := httpClient
	t.Cleanup(func() { httpClient = previous })
	step := 0
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var reply string
		switch step {
		case 0:
			reply = modelReply(toolCall("list_mcp_tools", map[string]any{"serverId": v.ID}))
		case 1:
			reply = modelReply(toolCall("call_mcp_tool", map[string]any{"serverId": v.ID, "tool": "echo", "arguments": map[string]any{"message": "verified"}}))
		default:
			reply = modelReply()
		}
		step++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(reply))}, nil
	})}
	if err := s.Chat(context.Background(), "", "assistant", "Use the configured MCP plugin"); err != nil {
		t.Fatal(err)
	}
	messages := s.Store.Snapshot().Messages
	trace := messages[len(messages)-1].Trace
	if len(trace) != 2 || trace[1].Tool != "call_mcp_tool" || trace[1].Error || !strings.Contains(trace[1].Result, "verified") {
		t.Fatalf("trace: %#v", trace)
	}
	if calls.Load() != 1 {
		t.Fatal("tool did not run exactly once")
	}
}

func TestMCPResultRedactionPreservesJSON(t *testing.T) {
	c := &mcpConnection{secrets: []string{"1", "quote\"token"}}
	b, err := c.resultJSON(map[string]any{"count": int64(9007199254740993), "text": "quote\"token 1", "items": []any{1, "1"}})
	if err != nil || !json.Valid(b) {
		t.Fatalf("invalid redacted JSON: %s %v", b, err)
	}
	if !strings.Contains(string(b), "9007199254740993") || !strings.Contains(string(b), `"items":[1,"[redacted]"]`) || strings.Contains(string(b), "token") {
		t.Fatalf("redaction changed structure: %s", b)
	}
}

func TestMCPConfigurationValidation(t *testing.T) {
	s, _ := fixture(t)
	cases := []MCPServer{
		{Name: "remote HTTP", Transport: "http", URL: "http://example.com/mcp"},
		{Name: "embedded auth", Transport: "http", URL: "https://user:secret@example.com/mcp"},
		{Name: "query auth", Transport: "http", URL: "https://example.com/mcp?token=secret"},
		{Name: "bad env", Transport: "http", URL: "https://example.com/mcp", BearerTokenEnv: "KEY=secret"},
		{Name: "bad local env", Transport: "stdio", Command: "server", EnvVars: []string{"KEY=secret"}},
		{Name: "missing executable", Transport: "stdio"},
		{Name: "bad timeout", Transport: "http", URL: "https://example.com/mcp", ToolTimeoutSec: 601},
	}
	for _, v := range cases {
		t.Run(v.Name, func(t *testing.T) {
			body, _ := json.Marshal(Request{MCPServer: v})
			if _, err := s.Dispatch(context.Background(), "saveMCPServer", body); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	v := saveTestMCP(t, s, MCPServer{Name: "forged ref", Transport: "http", URL: "https://example.com/mcp", TokenRef: "attacker-ref"})
	if v.TokenRef != "" {
		t.Fatal("caller-supplied credential reference accepted")
	}
}

func TestMCPBridgeAcrossModelProtocols(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic", "gemini"} {
		t.Run(protocol, func(t *testing.T) {
			previous := httpClient
			t.Cleanup(func() { httpClient = previous })
			args := map[string]any{"serverId": "github", "tool": "example", "arguments": map[string]any{"nested": map[string]any{"items": []any{1, "two", true}}}}
			httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				tool := request["tools"].([]any)[0].(map[string]any)
				var schema any
				var response any
				switch protocol {
				case "openai":
					schema = tool["function"].(map[string]any)["parameters"]
					_ = json.Unmarshal([]byte(modelReply(toolCall("call_mcp_tool", args))), &response)
				case "anthropic":
					schema = tool["input_schema"]
					response = map[string]any{"stop_reason": "tool_use", "content": []any{map[string]any{"type": "tool_use", "id": "test", "name": "call_mcp_tool", "input": args}}}
				case "gemini":
					declaration := tool["functionDeclarations"].([]any)[0].(map[string]any)
					if _, exists := declaration["parameters"]; exists {
						t.Fatal("MCP arguments sent through legacy Gemini schema")
					}
					schema = declaration["parametersJsonSchema"]
					response = map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]any{"functionCall": map[string]any{"name": "call_mcp_tool", "args": args}}}}}}}
				}
				encoded, _ := json.Marshal(schema)
				expected, _ := json.Marshal(tools["call_mcp_tool"].Params)
				if string(encoded) != string(expected) {
					t.Fatalf("MCP schema lost: %s", encoded)
				}
				body, _ := json.Marshal(response)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			reply, err := complete(context.Background(), ModelProfile{Protocol: protocol, BaseURL: "https://model.example/v1", Model: "test"}, "", "test", []Turn{{Role: "user", Text: "test MCP"}}, toolDefs([]string{"call_mcp_tool"}), false)
			if err != nil || len(reply.Calls) != 1 {
				t.Fatalf("MCP tool call not decoded: %#v %v", reply, err)
			}
			expected, _ := json.Marshal(args)
			if string(reply.Calls[0].Args) != string(expected) {
				t.Fatalf("nested arguments changed: %s", reply.Calls[0].Args)
			}
		})
	}
}
