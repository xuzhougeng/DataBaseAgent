package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// Browser OAuth sign-in for Streamable HTTP plugins. The whole session (client
// registration plus tokens) is one JSON secret in the OS credential store, kept
// under TokenRef with an "oauth_" prefix so the existing TokenRef plumbing
// (blanking in state responses, deletion, rollback) applies unchanged.
// ponytail: Windows credential blobs cap near 2.5 KB; split the blob if a provider's JWTs exceed it.
const mcpOAuthPrefix = "oauth_"

type mcpOAuthSession struct {
	Config oauth2.Config `json:"config"`
	Token  oauth2.Token  `json:"token"`
}

func mcpOAuth(v MCPServer) bool { return strings.HasPrefix(v.TokenRef, mcpOAuthPrefix) }

// Tests replace this to play the user's browser.
var openBrowser = openURL

func (s *Store) loadMCPOAuth(ref string) (*mcpOAuthSession, error) {
	raw, err := keyring.Get(modelKeyService, s.modelKeyAccount(ref))
	var session mcpOAuthSession
	if err != nil || json.Unmarshal([]byte(raw), &session) != nil || session.Token.AccessToken == "" {
		return nil, errors.New("无法读取 MCP 登录信息，请解锁系统凭据库或重新登录")
	}
	return &session, nil
}

func (s *Store) saveMCPOAuth(ref string, session *mcpOAuthSession) error {
	raw, _ := json.Marshal(session)
	if len(raw) > 16<<10 {
		return errors.New("MCP 登录信息过大，无法保存")
	}
	if err := s.saveSecret(ref, string(raw)); err != nil {
		return errors.New("无法保存 MCP 登录信息，请确认系统凭据库可用且已解锁")
	}
	return nil
}

// savingSource hands every new access token (first grant or refresh) to save.
type savingSource struct {
	mu   sync.Mutex
	src  oauth2.TokenSource
	cfg  *oauth2.Config
	last string
	save func(*oauth2.Config, *oauth2.Token)
}

func (t *savingSource) Token() (*oauth2.Token, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tok, err := t.src.Token()
	if err == nil && tok.AccessToken != t.last {
		t.last = tok.AccessToken
		t.save(t.cfg, tok)
	}
	return tok, err
}

// mcpOAuthHandler builds the SDK handler. fetch is nil for agent-run connections,
// which must never open a browser: an expired session reports an error instead.
func mcpOAuthHandler(v MCPServer, session *mcpOAuthSession, secret, redirect string, fetch auth.AuthorizationCodeFetcher, save func(*oauth2.Config, *oauth2.Token)) (*auth.AuthorizationCodeHandler, error) {
	cfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL: redirect,
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "DataBaseAgent", RedirectURIs: []string{redirect}, TokenEndpointAuthMethod: "none",
			GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
		}},
		RequestRefreshToken:   true,
		AcceptUnadvertisedIss: true,
		Client:                &http.Client{Timeout: 30 * time.Second},
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			if fetch == nil {
				return nil, errors.New("MCP 登录已失效，请在偏好设置中重新登录")
			}
			return fetch(ctx, args)
		},
		NewTokenSource: func(ctx context.Context, c *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			save(c, tok)
			return &savingSource{src: c.TokenSource(ctx, tok), cfg: c, last: tok.AccessToken, save: save}, nil
		},
	}
	// GitHub and other servers without dynamic registration need a client the user registered.
	if v.OAuthClientID != "" {
		if secret == "" && session != nil && session.Config.ClientID == v.OAuthClientID {
			secret = session.Config.ClientSecret
		}
		cfg.PreregisteredClient = &oauthex.ClientCredentials{ClientID: v.OAuthClientID}
		if secret != "" {
			cfg.PreregisteredClient.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: secret}
		}
	}
	if session != nil {
		c := session.Config
		cfg.InitialTokenSource = &savingSource{src: c.TokenSource(context.Background(), &session.Token), cfg: &c, last: session.Token.AccessToken, save: save}
	}
	return auth.NewAuthorizationCodeHandler(cfg)
}

// loginMCPServer runs the browser flow against the saved configuration, then
// lists tools like testMCPServer. r.APIKey optionally carries a client secret
// for a user-registered OAuth app; it is stored only inside the session blob.
func (s *Service) loginMCPServer(ctx context.Context, r Request) (any, error) {
	st := s.Store.Snapshot()
	v, err := mcpServerByID(&st, r.MCPServerID)
	if err != nil {
		return nil, err
	}
	if v.Transport != "http" {
		return nil, errors.New("浏览器登录仅适用于 Streamable HTTP 插件")
	}
	secret := ""
	if r.APIKey != nil {
		secret = strings.TrimSpace(*r.APIKey)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
	results := make(chan *auth.AuthorizationResult, 1)
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		if req.URL.Path != "/callback" || q.Get("code") == "" {
			http.Error(w, "登录未完成："+q.Get("error")+" "+q.Get("error_description"), 400)
			return
		}
		select {
		case results <- &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>DataBaseAgent</title><body style="font-family:system-ui;padding:48px;text-align:center"><h2>登录完成</h2><p>可以关闭此页面，回到 DataBaseAgent。</p>`))
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	fetch := func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		if err := openBrowser(args.URL); err != nil {
			return nil, fmt.Errorf("无法打开浏览器：%v", err)
		}
		select {
		case res := <-results:
			return res, nil
		case <-ctx.Done():
			return nil, errors.New("登录已取消")
		case <-time.After(5 * time.Minute):
			return nil, errors.New("登录超时，请重试")
		}
	}
	c, err := s.connectMCPOAuth(ctx, v, secret, redirect, fetch)
	if err != nil {
		return nil, err
	}
	defer c.close()
	tools, err := c.tools(ctx)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	signedIn := false
	for _, item := range s.Store.Snapshot().Settings.MCPServers {
		signedIn = signedIn || (item.ID == v.ID && mcpOAuth(item))
	}
	b, err := c.resultJSON(map[string]any{"serverId": v.ID, "toolCount": len(tools), "tools": names, "signedIn": signedIn})
	return json.RawMessage(b), err
}

// connectMCPOAuth connects an HTTP plugin through the OAuth handler. A newly
// granted session is written to the credential store and referenced from the
// workspace; refreshed tokens overwrite the same secret.
func (s *Service) connectMCPOAuth(ctx context.Context, v MCPServer, secret, redirect string, fetch auth.AuthorizationCodeFetcher) (*mcpConnection, error) {
	var session *mcpOAuthSession
	if mcpOAuth(v) {
		var err error
		if session, err = s.Store.loadMCPOAuth(v.TokenRef); err != nil && fetch == nil {
			return nil, err
		}
	}
	c := &mcpConnection{config: v}
	if session != nil {
		c.secrets = append(c.secrets, session.Token.AccessToken, session.Token.RefreshToken)
	}
	var mu sync.Mutex
	ref := ""
	if session != nil {
		ref = v.TokenRef
	}
	save := func(cfg *oauth2.Config, tok *oauth2.Token) {
		mu.Lock()
		defer mu.Unlock()
		c.secrets = append(c.secrets, tok.AccessToken, tok.RefreshToken)
		blob := &mcpOAuthSession{Config: *cfg, Token: *tok}
		if ref != "" {
			_ = s.Store.saveMCPOAuth(ref, blob)
			return
		}
		newRef := id("oauth")
		if err := s.Store.saveMCPOAuth(newRef, blob); err != nil {
			return
		}
		old := ""
		if err := s.Store.Update(func(st *State) error {
			for i := range st.Settings.MCPServers {
				if st.Settings.MCPServers[i].ID == v.ID {
					old = st.Settings.MCPServers[i].TokenRef
					st.Settings.MCPServers[i].TokenRef, st.Settings.MCPServers[i].BearerTokenEnv = newRef, ""
					event(st, "", "", "user", "MCP 插件已登录", v.Name)
					return nil
				}
			}
			return errors.New("MCP 插件不存在")
		}); err != nil {
			s.Store.deleteModelKey(newRef)
			return
		}
		s.Store.deleteModelKey(old)
		ref = newRef
	}
	handler, err := mcpOAuthHandler(v, session, secret, redirect, fetch, save)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: time.Duration(max(v.StartupTimeoutSec, v.ToolTimeoutSec)) * time.Second,
		Transport: mcpTransport{base: http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return s.startMCP(ctx, c, &mcp.StreamableClientTransport{Endpoint: v.URL, HTTPClient: client, OAuthHandler: handler, DisableStandaloneSSE: true, MaxRetries: -1, MaxEventSize: 4 << 20})
}
