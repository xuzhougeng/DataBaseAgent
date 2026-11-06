package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zalando/go-keyring"
)

// MCPServer contains configuration only. Tokens live in the OS credential store,
// or are resolved from the launching process environment at connection time.
type MCPServer struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Transport         string   `json:"transport"`
	URL               string   `json:"url,omitempty"`
	Command           string   `json:"command,omitempty"`
	Args              []string `json:"args,omitempty"`
	EnvVars           []string `json:"envVars,omitempty"`
	Enabled           bool     `json:"enabled"`
	BearerTokenEnv    string   `json:"bearerTokenEnv,omitempty"`
	TokenRef          string   `json:"tokenRef,omitempty"`
	StartupTimeoutSec int      `json:"startupTimeoutSec"`
	ToolTimeoutSec    int      `json:"toolTimeoutSec"`
	EnabledTools      []string `json:"enabledTools,omitempty"`
	DisabledTools     []string `json:"disabledTools,omitempty"`
}

func defaultMCPServers() []MCPServer {
	return []MCPServer{
		{ID: "github", Name: "GitHub", Transport: "http", URL: "https://api.githubcopilot.com/mcp/", Enabled: true, BearerTokenEnv: "GITHUB_PERSONAL_ACCESS_TOKEN", StartupTimeoutSec: 20, ToolTimeoutSec: 60},
		{ID: "cloudflare", Name: "Cloudflare", Transport: "http", URL: "https://mcp.cloudflare.com/mcp", Enabled: true, BearerTokenEnv: "CLOUDFLARE_API_TOKEN", StartupTimeoutSec: 20, ToolTimeoutSec: 60},
	}
}

var mcpEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateMCPServer(v *MCPServer) error {
	v.Name, v.URL, v.Command = strings.TrimSpace(v.Name), strings.TrimSpace(v.URL), strings.TrimSpace(v.Command)
	v.BearerTokenEnv = strings.TrimSpace(v.BearerTokenEnv)
	if v.Name == "" || len(v.Name) > 120 {
		return errors.New("请填写不超过 120 字节的 MCP 名称")
	}
	if v.StartupTimeoutSec == 0 {
		v.StartupTimeoutSec = 20
	}
	if v.ToolTimeoutSec == 0 {
		v.ToolTimeoutSec = 60
	}
	if v.StartupTimeoutSec < 1 || v.StartupTimeoutSec > 120 || v.ToolTimeoutSec < 1 || v.ToolTimeoutSec > 600 {
		return errors.New("MCP 连接超时需为 1–120 秒，工具超时需为 1–600 秒")
	}
	if v.BearerTokenEnv != "" && !mcpEnvName.MatchString(v.BearerTokenEnv) {
		return errors.New("MCP Token 环境变量名无效")
	}
	switch v.Transport {
	case "http":
		if err := validHTTPURL(v.URL); err != nil {
			return err
		}
		u, _ := url.Parse(v.URL)
		if u.Fragment != "" || u.RawQuery != "" {
			return errors.New("MCP 地址不能包含查询参数或片段；请通过 Token 配置认证")
		}
		if u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
			return errors.New("远程 MCP 必须使用 HTTPS；HTTP 仅用于本机服务")
		}
		v.Command, v.Args, v.EnvVars = "", nil, nil
	case "stdio":
		if v.Command == "" || len(v.Command) > 4096 || strings.ContainsAny(v.Command, "\x00\r\n") {
			return errors.New("请填写 MCP 可执行文件路径或命令名")
		}
		if len(v.Args) > 100 || len(v.EnvVars) > 100 {
			return errors.New("MCP 参数或环境变量过多")
		}
		for _, a := range v.Args {
			if len(a) > 8192 || strings.ContainsRune(a, 0) {
				return errors.New("MCP 命令参数无效")
			}
		}
		for _, e := range v.EnvVars {
			if !mcpEnvName.MatchString(e) {
				return errors.New("MCP 环境变量列表仅接受变量名，不接受密钥明文")
			}
		}
		v.URL, v.BearerTokenEnv = "", ""
	default:
		return errors.New("MCP 传输方式需为 http 或 stdio")
	}
	for _, names := range [][]string{v.EnabledTools, v.DisabledTools} {
		if len(names) > 1000 {
			return errors.New("MCP 工具过滤列表过长")
		}
		for _, n := range names {
			if n == "" || len(n) > 256 || strings.ContainsAny(n, "\r\n\x00") {
				return errors.New("MCP 工具名称无效")
			}
		}
	}
	return nil
}

func mcpServerByID(st *State, sid string) (MCPServer, error) {
	for _, v := range st.Settings.MCPServers {
		if v.ID == sid {
			return v, nil
		}
	}
	return MCPServer{}, errors.New("MCP 插件不存在")
}

func (s *Service) manageMCPServer(action string, r Request) (any, error) {
	var newRef, oldRef string
	var result any
	err := s.Store.Update(func(st *State) error {
		if action == "deleteMCPServer" {
			v, err := mcpServerByID(st, r.MCPServerID)
			if err != nil {
				return err
			}
			oldRef = v.TokenRef
			kept := []MCPServer{}
			for _, item := range st.Settings.MCPServers {
				if item.ID != v.ID {
					kept = append(kept, item)
				}
			}
			st.Settings.MCPServers = kept
			event(st, "", "", "user", "MCP 插件已删除", v.Name)
			return nil
		}
		v := r.MCPServer
		v.TokenRef = "" // Never accept credential references from callers.
		if err := validateMCPServer(&v); err != nil {
			return err
		}
		index := -1
		if v.ID != "" {
			for i, item := range st.Settings.MCPServers {
				if item.ID == v.ID {
					index = i
					oldRef = item.TokenRef
					break
				}
			}
			if index < 0 {
				return errors.New("MCP 插件不存在")
			}
			previous := st.Settings.MCPServers[index]
			if r.APIKey == nil && v.Transport == "http" {
				if previous.URL != v.URL && oldRef != "" {
					return errors.New("修改 MCP 地址时请重新填写或移除 Token")
				}
				v.TokenRef = oldRef
			}
		} else {
			if len(st.Settings.MCPServers) >= 50 {
				return errors.New("最多配置 50 个 MCP 插件")
			}
			v.ID = id("mcp")
		}
		if r.APIKey != nil && strings.TrimSpace(*r.APIKey) != "" {
			if v.Transport != "http" {
				return errors.New("STDIO 请使用环境变量传入凭据")
			}
			var err error
			newRef, err = s.Store.saveModelKey(strings.TrimSpace(*r.APIKey))
			if err != nil {
				return err
			}
			v.TokenRef = newRef
			v.BearerTokenEnv = ""
		}
		if index < 0 {
			st.Settings.MCPServers = append(st.Settings.MCPServers, v)
		} else {
			st.Settings.MCPServers[index] = v
		}
		if v.TokenRef == oldRef {
			oldRef = ""
		}
		result = map[string]string{"mcpServerId": v.ID}
		event(st, "", "", "user", "MCP 插件配置已保存", v.Name)
		return nil
	})
	if err != nil {
		s.Store.deleteModelKey(newRef)
	} else {
		s.Store.deleteModelKey(oldRef)
	}
	return result, err
}

func mcpStatus(v MCPServer) string {
	if !v.Enabled {
		return "disabled"
	}
	if v.Transport == "http" && v.TokenRef == "" && v.BearerTokenEnv != "" && os.Getenv(v.BearerTokenEnv) == "" {
		return "needs_token"
	}
	for _, name := range v.EnvVars {
		if _, ok := os.LookupEnv(name); !ok {
			return "needs_env"
		}
	}
	return "configured" // Configuration readiness is not a successful connection test.
}

func (s *Store) mcpToken(v MCPServer) (string, error) {
	if v.TokenRef != "" {
		key, err := keyring.Get(modelKeyService, s.modelKeyAccount(v.TokenRef))
		if err != nil || key == "" {
			return "", errors.New("无法读取 MCP Token，请解锁系统凭据库或重新填写")
		}
		return key, nil
	}
	if v.BearerTokenEnv != "" {
		key := os.Getenv(v.BearerTokenEnv)
		if key == "" {
			return "", fmt.Errorf("请在偏好设置中填写 MCP Token，或设置环境变量 %s", v.BearerTokenEnv)
		}
		return key, nil
	}
	return "", nil
}

// Bound both JSON and SSE responses. Refuse redirects so credentials cannot be
// forwarded to a different endpoint by a remote server.
type mcpTransport struct {
	base  http.RoundTripper
	token string
}

func (t mcpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	res, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	res.Body = &mcpBody{ReadCloser: res.Body, remaining: 8 << 20}
	return res, nil
}

type mcpBody struct {
	io.ReadCloser
	remaining int64
}

func (b *mcpBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errors.New("MCP 响应超过 8 MiB")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

type mcpConnection struct {
	session *mcp.ClientSession
	config  MCPServer
	secrets []string
}

func (c *mcpConnection) redact(text string) string {
	for _, secret := range c.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
			quoted, _ := json.Marshal(secret)
			text = strings.ReplaceAll(text, string(quoted[1:len(quoted)-1]), "[redacted]")
		}
	}
	return text
}
func (c *mcpConnection) safeError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(c.redact(err.Error()))
}

// Redact string values before serializing. Replacing secrets in serialized JSON
// could corrupt numbers or escape sequences (STDIO env values may be "1").
func (c *mcpConnection) resultJSON(value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var clean func(any) any
	clean = func(v any) any {
		switch v := v.(type) {
		case string:
			return c.redact(v)
		case []any:
			for i := range v {
				v[i] = clean(v[i])
			}
		case map[string]any:
			out := make(map[string]any, len(v))
			for key, item := range v {
				out[c.redact(key)] = clean(item)
			}
			return out
		}
		return v
	}
	return json.Marshal(clean(decoded))
}
func (c *mcpConnection) close() { _ = c.session.Close() }

// Sessions are lazy and confined to one agent turn. This keeps STDIO state across
// discovery/calls, closes subprocesses on completion, and avoids startup traffic.
type mcpRunKey struct{}
type mcpRun struct{ connections map[string]*mcpConnection }

func (r *mcpRun) close() {
	for _, c := range r.connections {
		c.close()
	}
}

func (s *Service) connectMCP(ctx context.Context, v MCPServer) (*mcpConnection, error) {
	if err := validateMCPServer(&v); err != nil {
		return nil, err
	}
	c := &mcpConnection{config: v}
	var transport mcp.Transport
	switch v.Transport {
	case "http":
		token, err := s.Store.mcpToken(v)
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(token, "\r\n\x00") {
			return nil, errors.New("MCP Token 必须为单行")
		}
		c.secrets = append(c.secrets, token)
		client := &http.Client{Timeout: time.Duration(max(v.StartupTimeoutSec, v.ToolTimeoutSec)) * time.Second,
			Transport: mcpTransport{base: http.DefaultTransport, token: token}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		transport = &mcp.StreamableClientTransport{Endpoint: v.URL, HTTPClient: client, DisableStandaloneSSE: true, MaxRetries: -1, MaxEventSize: 4 << 20}
	case "stdio":
		cmd := exec.CommandContext(ctx, v.Command, v.Args...)
		cmd.Dir = s.Store.Root()
		cmd.WaitDelay = time.Second
		cmd.Env = []string{}
		// Do not leak unrelated model/provider secrets into every local plugin.
		for _, name := range []string{"PATH", "HOME", "USER", "TMPDIR", "TMP", "TEMP", "SystemRoot", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "APPDATA", "LOCALAPPDATA", "USERPROFILE", "LANG", "LC_ALL", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
			if value, ok := os.LookupEnv(name); ok {
				cmd.Env = append(cmd.Env, name+"="+value)
			}
		}
		for _, name := range v.EnvVars {
			value, ok := os.LookupEnv(name)
			if !ok {
				return nil, fmt.Errorf("MCP 环境变量 %s 未设置", name)
			}
			cmd.Env = append(cmd.Env, name+"="+value)
			c.secrets = append(c.secrets, value)
		}
		transport = &mcp.CommandTransport{Command: cmd, TerminateDuration: time.Second}
	}
	startup, cancel := context.WithTimeout(ctx, time.Duration(v.StartupTimeoutSec)*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "DataBaseAgent", Version: "0.2.0"}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}, MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
	session, err := client.Connect(startup, transport, nil)
	if err != nil {
		return nil, c.safeError(err)
	}
	c.session = session
	return c, nil
}

func (s *Service) acquireMCP(ctx context.Context, sid string) (*mcpConnection, func(), error) {
	st := s.Store.Snapshot()
	v, err := mcpServerByID(&st, sid)
	if err != nil {
		return nil, nil, err
	}
	if !v.Enabled {
		return nil, nil, errors.New("MCP 插件已停用")
	}
	run, _ := ctx.Value(mcpRunKey{}).(*mcpRun)
	if run != nil {
		if old := run.connections[sid]; old != nil {
			a, _ := json.Marshal(old.config)
			b, _ := json.Marshal(v)
			if string(a) == string(b) {
				return old, func() {}, nil
			}
			old.close()
			delete(run.connections, sid)
		}
	}
	c, err := s.connectMCP(ctx, v)
	if err != nil {
		return nil, nil, err
	}
	if run != nil {
		run.connections[sid] = c
		return c, func() {}, nil
	}
	return c, c.close, nil
}

func mcpToolAllowed(v MCPServer, name string) bool {
	return !slices.Contains(v.DisabledTools, name) && (len(v.EnabledTools) == 0 || slices.Contains(v.EnabledTools, name))
}

func (c *mcpConnection) tools(ctx context.Context) ([]*mcp.Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.config.ToolTimeoutSec)*time.Second)
	defer cancel()
	out := []*mcp.Tool{}
	cursor := ""
	seen := map[string]bool{}
	for range 100 {
		page, err := c.session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, c.safeError(err)
		}
		for _, tool := range page.Tools {
			if tool != nil && mcpToolAllowed(c.config, tool.Name) {
				out = append(out, tool)
			}
			if len(out) > 1000 {
				return nil, errors.New("MCP 工具超过 1000 个，请配置工具白名单")
			}
		}
		if page.NextCursor == "" {
			return out, nil
		}
		if seen[page.NextCursor] {
			return nil, errors.New("MCP 服务返回重复分页游标")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
	return nil, errors.New("MCP 工具分页超过 100 页")
}

func (s *Service) testMCPServer(ctx context.Context, sid string) (any, error) {
	c, done, err := s.acquireMCP(ctx, sid)
	if err != nil {
		return nil, err
	}
	defer done()
	tools, err := c.tools(ctx)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	b, err := c.resultJSON(map[string]any{"serverId": sid, "toolCount": len(tools), "tools": names})
	return json.RawMessage(b), err
}

func (s *Service) runMCPTool(ctx context.Context, a Agent, call ToolCall, args map[string]any) (string, bool) {
	fail := func(err error) (string, bool) {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b), true
	}
	if a.ID != "assistant" {
		return fail(errors.New("只有主 Agent 可以调用 MCP 插件"))
	}
	if call.Name == "list_mcp_servers" {
		st := s.Store.Snapshot()
		out := []map[string]any{}
		for _, v := range st.Settings.MCPServers {
			out = append(out, map[string]any{"id": v.ID, "name": v.Name, "transport": v.Transport, "status": mcpStatus(v), "enabled": v.Enabled})
		}
		b, _ := json.Marshal(out)
		return string(b), false
	}
	c, done, err := s.acquireMCP(ctx, argStr(args, "serverId"))
	if err != nil {
		return fail(err)
	}
	defer done()
	available, err := c.tools(ctx)
	if err != nil {
		return fail(err)
	}
	var result any
	failed := false
	if call.Name == "list_mcp_tools" {
		filtered := []*mcp.Tool{}
		query := strings.ToLower(argStr(args, "query"))
		for _, tool := range available {
			if query == "" || strings.Contains(strings.ToLower(tool.Name+" "+tool.Description), query) {
				filtered = append(filtered, tool)
			}
		}
		offset := min(argInt(args, "offset", 0, 1000), len(filtered))
		limit := argInt(args, "limit", 5, 20)
		result = map[string]any{"serverId": c.config.ID, "total": len(filtered), "tools": filtered[offset:min(offset+limit, len(filtered))], "instructions": clip(c.session.InitializeResult().Instructions, 4000)}
	} else {
		name := argStr(args, "tool")
		found := false
		for _, tool := range available {
			found = found || tool.Name == name
		}
		if !found {
			return fail(errors.New("MCP 工具不存在或未获准使用，请先 list_mcp_tools"))
		}
		input, ok := args["arguments"].(map[string]any)
		if !ok || input == nil {
			return fail(errors.New("MCP arguments 必须为 JSON 对象"))
		}
		timeout, cancel := context.WithTimeout(ctx, time.Duration(c.config.ToolTimeoutSec)*time.Second)
		defer cancel()
		response, err := c.session.CallTool(timeout, &mcp.CallToolParams{Name: name, Arguments: input})
		if err != nil {
			return fail(c.safeError(err))
		}
		result = response
		failed = response.IsError
	}
	b, err := c.resultJSON(result)
	if err != nil {
		return fail(c.safeError(err))
	}
	text := string(b)
	if len(text) > 16000 {
		b, _ = json.Marshal(map[string]any{"truncated": true, "partial": clip(text, 14000), "hint": "缩小 limit 或 query；工具调用结果可能不完整"})
		text = string(b)
	}
	return text, failed
}
