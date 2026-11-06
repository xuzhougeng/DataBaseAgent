# MCP plugins

DBA is an MCP **client**. In **Preferences → MCP plugins**, configure GitHub,
Cloudflare, or another MCP server. The main Project Assistant can discover and
invoke the server's tools through any of DBA's model providers (OpenAI-compatible,
Anthropic, or Gemini). Calls appear in the existing conversation timeline and
persisted trace, including arguments, results, duration, and error status.

## Included plugins

| Plugin | Streamable HTTP endpoint | Browser sign-in | Environment variable alternative |
| --- | --- | --- | --- |
| GitHub | `https://api.githubcopilot.com/mcp/` | Needs your own OAuth App (see below) | `GITHUB_PERSONAL_ACCESS_TOKEN` |
| Cloudflare | `https://mcp.cloudflare.com/mcp` | Works out of the box | `CLOUDFLARE_API_TOKEN` |

Both configurations are included in new workspaces and added once to existing
workspaces that have no MCP configuration field. They start enabled but require
credentials before connecting. Removing either configuration, or removing all
plugins, is preserved on restart. Nothing connects merely because the app starts
or a conversation opens.

1. Open a plugin's configuration and save it.
2. Select **Sign in with browser**. DBA opens the server's authorization page in
   your default browser; approve the requested permissions and return to DBA.
   The dialog then lists the available tools. Alternatively, enter a GitHub
   personal access token (PAT) or Cloudflare API token, or set the listed
   environment variable before launching DBA.
3. **Test connection** re-checks the saved configuration and lists tools; it
   does not execute them. “Not tested” means configuration is available, not
   that a connection succeeded. Test results are transient.
4. Ask the main assistant to use GitHub or Cloudflare for a specific task.

## Browser sign-in (OAuth)

Sign-in follows the MCP authorization specification: DBA reads the server's
protected-resource and authorization-server metadata, registers itself as a
public client with dynamic client registration, runs the authorization-code flow
with PKCE, and receives the code on a loopback listener (`http://127.0.0.1:<random
port>/callback`). Refresh tokens are requested when the server offers them, and
refreshed tokens are written back automatically. Sign-in times out after five
minutes. Agent runs never open a browser: an expired or revoked session fails
with a message asking you to sign in again in Preferences.

Servers without dynamic client registration, such as the remote GitHub MCP
server, need a client you registered yourself. Create a GitHub OAuth App (or
GitHub App) with callback URL `http://127.0.0.1/callback` (GitHub ignores the
port for loopback addresses), then enter its client ID under **Advanced
authentication**. Enter the client secret only when signing in; it is stored
with the session in the credential store, never in workspace JSON.

The session (client registration plus tokens) is one entry in the operating
system credential store. Workspace JSON holds only a reference, which state
responses omit. Selecting **Sign out** and saving deletes the entry; entering a
bearer token replaces a saved sign-in, and vice versa.

Tokens entered in the UI are stored in the operating system credential store.
Workspace JSON holds only a reference. State responses and agent settings omit
that reference. Leaving the token field blank preserves the existing credential;
selecting **Remove saved token** deletes it after the configuration is saved.
A saved sign-in or token takes precedence over an environment variable. To use
an unauthenticated HTTP server, clear the saved credential and the variable name.
Changing a server URL while retaining a saved credential requires explicitly
removing it first, so a token is never sent to a different endpoint.

## Custom servers

Choose **Add MCP plugin** and select:

- **Streamable HTTP**: provide an HTTPS MCP endpoint. HTTP is accepted only for
  `localhost`, `127.0.0.1`, or `::1`. URL credentials, query strings, fragments,
  and redirects are rejected; sign in with the browser or use the token fields.
- **STDIO**: provide an installed executable and one argument per line. DBA starts
  the executable directly, without a shell, in the workspace directory. Local
  plugins run with the current user's OS permissions. Use the environment
  variable names field to pass credentials or other custom configuration from
  the DBA process; do not put secrets in command arguments.

STDIO inherits basic OS, path, locale, and proxy environment variables, plus only
explicitly selected variables. The application does not install server packages
or runtimes. For example, a Docker-based GitHub MCP server also needs Docker
available on the host. Native desktop launches may have a different `PATH` from
your terminal; use an absolute executable path when necessary.

Advanced options configure startup timeout (default 20 seconds, maximum 120),
tool/list timeout (default 60 seconds, maximum 600), an optional tool allowlist,
and a denylist. Names are exact MCP tool names; the denylist wins. Empty allowlists
allow all tools not explicitly denied. The server's credentials still determine
its actual resource permissions.

## Agent integration

Only the main assistant has these workspace-level tools; specialist tool scopes
are unchanged:

| DBA tool | Behavior |
| --- | --- |
| `list_mcp_servers` | Lists configuration status without connecting or exposing credentials. |
| `list_mcp_tools` | Connects on demand and returns tool names, descriptions, schemas, and server instructions; supports query/offset/limit. |
| `call_mcp_tool` | Calls an enabled server's allowed tool using its original name and JSON arguments. |

MCP schemas are returned on demand instead of adding every remote tool to every
model request. The assistant reads a schema before calling its tool. The MCP
server validates its own tool arguments; DBA validates the bridge arguments and
checks the enabled server and tool filters at each call. Configuration changes
invalidate a cached connection before its next use. MCP `isError` results are
recorded as failed tool calls, and text plus structured results return to the model.
Remote binary content is represented as JSON rather than rendered as media.

Connections are reused within one agent turn and closed at the end, including
local subprocess shutdown. A standalone connection test has its own session.
Tool pagination is bounded to 100 pages / 1,000 allowed tools. HTTP response bodies
are capped at 8 MiB, SSE events at 4 MiB, and model-facing results at 16,000 bytes
with an explicit truncation marker. Reduce the discovery limit or query when a
large schema is truncated. MCP outputs and server instructions are untrusted
reference material and do not extend the user's authorization.

Calls are not automatically retried by DBA. A timeout or cancellation can occur
after a remote write has already executed; inspect the remote state before
retrying. Disabling or deleting a plugin prevents subsequent calls but does not
undo or cancel an operation already sent to the server. Tool access is controlled
by the configured filters and provider credentials, with no separate per-call
approval dialog.

The client uses the official MCP Go SDK for protocol negotiation, OAuth
authorization, Streamable HTTP (JSON and SSE responses), and STDIO. Legacy
HTTP+SSE endpoints, MCP resources, prompts, sampling, elicitation, plugin
marketplace packages, and Codex TOML config import are outside this release's
UI/tool surface.

## References

The configuration approach follows [OpenAI's MCP documentation](https://learn.chatgpt.com/docs/extend/mcp)
for transport choice, bearer credentials, timeouts, and tool filtering.
Endpoint and authentication details come from the
[GitHub MCP server documentation](https://github.com/github/github-mcp-server/blob/main/docs/remote-server.md)
and [Cloudflare MCP documentation](https://developers.cloudflare.com/agents/model-context-protocol/cloudflare/servers-for-cloudflare/).
Protocol handling uses the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

Tests use local HTTP/SSE and STDIO MCP servers, a mock OAuth authorization
server, and the full agent loop.
Real account permissions and vendor endpoint availability still require the
connection test with your own credentials.
