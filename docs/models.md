# Model Providers

DBA talks to language models through three wire protocols behind one neutral interface. You register any number of *model profiles* in **Preferences → Models** and assign one per agent. No SDKs are bundled; every adapter is a small, auditable HTTP client in `internal/workbench/llm.go`.

## Profiles

| Field | Meaning |
| --- | --- |
| Name | Label shown in agent selectors. |
| Protocol | `openai`, `anthropic` or `gemini` (see below). |
| API base URL | Service prefix. DBA appends the protocol-specific path. `http` or `https`, no embedded credentials. |
| Model ID | Passed through verbatim (`model` field or URL segment). |
| API key | Enter the key from your provider. Leave it blank for a service that runs without authentication. |

DBA saves the key in the current user's system credential store (macOS Keychain, Windows Credential Manager, or a Linux Secret Service provider such as GNOME Keyring). The workspace stores a credential reference. Re-enter the key if you move the workspace to a different directory or machine.

When editing a model, leave the API key blank to keep its current settings. Enter a new key to replace them or select **Remove current key settings** to use the model without a key. Profiles created in older versions keep reading their configured environment variable until you replace or remove the key settings.

## Protocols

| Protocol | Request | Auth header | Tool calling | Notes |
| --- | --- | --- | --- | --- |
| `openai` | `POST {base}/chat/completions` | `Authorization: Bearer` | `tools` / `tool_calls` / `role: tool` | Works with OpenAI, DeepSeek, Qwen (DashScope compatible mode), Ollama (`/v1`), vLLM, LM Studio, OpenRouter and most gateways. Invalid JSON in `arguments` is replaced by `{}`. |
| `anthropic` | `POST {base}/messages` | `x-api-key` + `anthropic-version: 2023-06-01` | `tools` / `tool_use` / `tool_result` blocks | `max_tokens` is fixed at 8192. Tool results from one turn are merged into a single `user` message, as the API requires. Tool definitions stay in the request even for the forced final answer (`tool_choice: none`). |
| `gemini` | `POST {base}/models/{model}:generateContent` | `x-goog-api-key` | `functionDeclarations` / `functionCall` / `functionResponse` | Gemini issues no call ids; DBA assigns local ids for tracing and matches responses by name. The forced final answer uses `functionCallingConfig.mode = NONE`. |

Common behaviour:

- No sampling parameters are sent; provider defaults apply. (Several current models reject `temperature`.)
- Responses are read up to 4 MiB; non-200 responses surface the status code and the first 300 bytes of the body in the UI.
- The HTTP client has a 90 s timeout per call and honours `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` from the environment.
- Stop reasons are normalized to `end_turn`, `tool_use` or `max_tokens`; an empty response is an error, not a silent blank reply.

## Neutral conversation model

Adapters convert to and from this representation:

```
Turn  { Role: user | assistant | tool; Text; Calls []ToolCall; CallID; Name }
ToolCall { ID; Name; Args json }
ToolDef  { Name; Description; Params json-schema | nil }
Reply { Text; Calls []ToolCall; Stop }
```

The agent loop (see [agents.md](agents.md)) only ever sees `Turn` and `Reply`; switching an agent between providers changes nothing about its behaviour except the model's own quality.

## Recipes

**Ollama (local, no key)**

| Field | Value |
| --- | --- |
| Protocol | `openai` |
| Base URL | `http://127.0.0.1:11434/v1` |
| Model ID | the model you pulled, e.g. `qwen3` |
| API key | *(empty)* |

Pick a model that supports tool calling; otherwise agents still work but will answer without consulting tools.

**DeepSeek**

Use protocol `openai`, base `https://api.deepseek.com/v1`, model `deepseek-chat` and your DeepSeek API key.

**Anthropic**

Use protocol `anthropic`, base `https://api.anthropic.com/v1`, your chosen model ID and your Anthropic API key.

**Google Gemini**

Use protocol `gemini`, base `https://generativelanguage.googleapis.com/v1beta`, your model ID from Google AI Studio and your Gemini API key.

**OpenAI-compatible gateway with a custom prefix**

Enter exactly the prefix the gateway documents (for example `https://gateway.example.com/openai/v1`). DBA only appends `/chat/completions`.

## Assigning models to agents

Each agent can use a different profile: a cheap local model for the Project Assistant, a stronger hosted model for the Reviewer. Agents without an explicit choice fall back to the first profile in the list; deleting a profile clears the references that pointed to it.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| API key could not be saved or read | Check that your system credential store is running and unlocked. Re-enter the key in Preferences if the entry is missing. |
| "environment variable X is not set" | The profile still uses an environment variable. Enter the API key in Preferences or set the variable and relaunch from the same shell. |
| `HTTP 404` from an `openai` profile | Base URL already includes `/chat/completions`, or the gateway uses a different prefix. Enter the prefix only. |
| `HTTP 400` mentioning `tools` on a local server | The model or server does not support tool calling. Choose another model. |
| Reply says it cannot see data | The agent's tool scope excludes what you asked for. Use a different agent or ask the Project Assistant. |
| `connection refused` | The local server is not running on that port, or a proxy variable is routing loopback traffic; add the host to `NO_PROXY`. |
