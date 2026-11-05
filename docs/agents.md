# Agents

The **AI assistant** is a dedicated page and the default entry point. Describe a goal there; the Project Assistant can create and configure a project, search, review candidates, configure and lock a pipeline, queue jobs, inspect outputs and export a manifest. The other pages display the same state and remain available for manual operations. The right-hand **Work progress** panel contains monitoring controls, active agent steps, help requests and recent events; it does not contain a second conversation.

## Conversations and progress

Choose a project in the navigation selector, or **Workspace chat** to start before a project exists. Opening, selecting or creating a project goes directly to its main-agent conversation. Each scope has a searchable history with independent saved chats, titles, pins, archives, and Markdown export. Every chat is led by the Project Assistant; specialist agents share that chat instead of opening separate role conversations. Workspace messages remain in `State.Messages`; project messages remain in `Project.Messages`, linked through `conversationId` to `State.Conversations`. Project tools default to the conversation's project and accept an explicit `projectId`, which is required in workspace chat. The assistant must only use a different project when the user's request calls for it. Candidate and job IDs are checked against the selected tool project.

Each chat has its own draft. Drafts, selected chats, unread counts, and unconfirmed submissions are saved in browser storage under the workspace path. The model receives up to the last 16 messages from completed turns in that conversation, including their specialist results; future queued prompts, other conversations, and failed attempts are excluded. History is retained without the former 200-message truncation. The UI initially renders 30 turns and can load earlier messages without losing the reading position. It preserves expanded tool details, input selection, and scroll position during background updates. A jump-to-latest button appears when reading older messages. The composer grows with the input, uses an accessible round send button, and handles Chinese IME composition without sending an unfinished word.

Type `@` (or click **@ Subagent**) to choose a specialist. Use arrow keys to navigate and Enter/Tab to insert a mention. `@scout`, `@reviewer`, `@pipeline`, `@operator` and their Chinese role names are recognized. Multiple mentions run sequentially, once per named specialist, before the main agent summarizes and continues. Email addresses and mentions in code blocks/inline code do not dispatch agents. Unknown mentions report an error and keep the draft. Legacy API requests with a specialist `agentId` are treated as a mention in the shared conversation.

The main agent can also call `delegate_agent` autonomously with an `agentId`, concrete `task` and optional `projectId`. Each child uses its own configured model, instructions, step budget and fixed tool permissions, with shared conversation context and fresh project state. Only the main agent can delegate; children cannot delegate recursively. A conversation turn allows up to eight delegations. Child results are returned as tool results and saved as expandable task cards (`Message.kind = delegation`) in the same conversation, with task, result, tool traces and failure status. Child failures preserve completed writes and let the main agent explain or recover. No separate child chat is created.

`POST /api/enqueueMessage` saves the user message and returns its conversation/message IDs immediately. A client-generated `clientId` makes acceptance idempotent, including when the browser loses the acknowledgement. Execution belongs to the service, so navigating or reloading the page does not cancel it. Within each project or the workspace, messages across conversations and agents run in FIFO order, with at most 20 waiting messages. Separate project queues can run independently. Queued messages can be edited in place or cancelled; users can also pause/resume the queue or stop the active request. The synchronous `message` command remains available for existing callers.

`/api/state` exposes transient `agentRuns` with conversation/message IDs, the current model step, tool arguments, start times, and active specialist/task IDs. Child tool traces are checkpointed independently and remain attached to their parent request, including when other prompts are queued. The chat displays an execution timeline with running, successful, and failed steps, elapsed time, and expandable arguments/results. Completed tool calls are checkpointed on the request and recorded as events. Trace arguments are limited to 8,000 bytes and results to 16,000 bytes; truncated traces are labelled, and all content is HTML-escaped. Cancelling a request does not roll back tools that have already completed.

Failure or stopping the active request pauses the scope's remaining queue. A retry creates a new request in the same conversation and joins the queue; resuming the queue is explicit. After an application restart, in-flight messages become `interrupted`, their completed tool traces are preserved, and waiting messages stay paused. No interrupted tool operation is automatically replayed. Schema 1 and 2 workspaces migrate each legacy project or workspace history into a shared main-agent conversation, retaining the original authors and child results and preserve the original messages. Migration IDs and recovery changes are written atomically before the workspace opens. A chat with pending messages cannot be archived.

## The loop

```mermaid
sequenceDiagram
    participant U as User
    participant L as Agent loop
    participant M as Model
    participant T as Tools
    participant S as Service / Store
    U->>S: enqueue goal with clientId
    S-->>U: persisted conversation/message IDs
    S->>L: run next queued message in scope
    loop up to maxSteps
        L->>M: instructions + history + scoped tools
        M-->>L: text and/or tool calls
        loop each call, sequentially
            L->>T: validate arguments, read fresh state
            T->>S: read snapshot or dispatch validated command
            S-->>T: result
            T-->>L: JSON result or error
            L->>S: checkpoint tool trace and progress event
        end
    end
    L->>M: conclude if budget exhausted
    L->>S: persist reply or interruption with trace
    Note over L,S: continue FIFO on success; pause on stop/failure
```

Calls in one model reply execute sequentially, each against the latest committed state. A saved query can therefore be used by the next search, and a freshly locked pipeline by the next launch, in the same batch. The default Project Assistant and Reviewer budgets are 20 model steps; the other roles default to 12. Budgets remain configurable from 1–20. Long remote jobs continue under the scheduler after the model finishes its reply.

Tool schemas are validated locally, including required fields, types, enums, numeric bounds and unknown fields. Errors are returned to the model as JSON and marked in the trace. Tool output is capped at 16,000 bytes; list candidates, jobs and artifacts support `offset` and `limit` (maximum 50). Abstracts and job logs remain clipped to 4,000 characters/bytes. Every invocation records name, argument excerpt, result excerpt, latency and error status.

## Roles

| Agent | Scope |
| --- | --- |
| Project Assistant | All read and application-command tools; complete workflow and workspace configuration |
| Search Strategist | Project scope, queries, searches, candidate import and inspection |
| Reviewer | Candidate inspection and recorded decisions, reasons, accessions, download URLs and groups |
| Pipeline Advisor | Pipeline editing/locking, server checks, job creation and output inspection |
| Operations Inspector | Server registration/checks, monitoring settings, job inspection/recovery and manifests |

Tools, icons and descriptions are fixed in code. In **Agent center**, right-click a role card (or focus it and press Shift+F10) to edit its model, instructions and step budget in a dialog, or restore its preset instructions and step budget. A normal click also opens the configuration dialog. Use **@ Subagent** in the main conversation to request specialist work. Existing workspaces adopt the executable preset scopes; prompts and step budgets that exactly match the former defaults are upgraded. Custom instructions and model choices are preserved.

The separate **Agent review** button on the Review page still generates suggestions only, for up to 20 pending candidates. Its quote must appear verbatim in the title or abstract; otherwise the suggestion becomes `uncertain / low`. The conversational Reviewer can record decisions using `review_candidate`. Decisions require a reason and record `reviewedBy` as `user` or `agent:<id>`; legacy decisions without this field are shown as manual. Chat decisions are model judgments, not automatically verified evidence quotes.

## Tool catalogue

| Tools | Behavior |
| --- | --- |
| `delegate_agent` | Main-agent-only specialist delegation, with task, optional project, scoped permissions, and up to eight child calls per turn |
| `list_projects`, `get_project` | Project IDs, goals, criteria, queries and stage counts |
| `create_project`, `update_project` | Create a project or patch its editable fields |
| `search`, `list_searches`, `import_candidates` | Execute real discovery, inspect history and import records with de-duplication |
| `list_candidates`, `get_candidate` | Paginated candidates and source evidence |
| `review_candidates`, `review_candidate` | Batch suggestions, or save a decision and candidate input fields |
| `get_pipeline`, `save_pipeline`, `lock_pipeline` | Inspect, patch, validate and version a pipeline |
| `list_servers`, `add_server`, `probe_server` | Inspect/register servers and run an actual connection/environment check |
| `launch_jobs`, `list_jobs`, `get_job`, `resolve_job` | Queue accepted data, inspect status/logs and resume a resolved help request |
| `tick`, `recent_events` | Trigger supervision and inspect the audit trail |
| `list_artifacts`, `export_manifest` | Inspect archived files and export provenance |
| `get_settings`, `update_settings` | Read configuration and patch monitoring policy |
| `save_model`, `delete_model`, `save_agent`, `reset_agent` | Manage model profiles, instructions, model assignment and budgets |

Mutation tools call `Service.Dispatch`, the same command layer used by forms. Partial updates preserve omitted fields; provided arrays replace entire lists. Invalid commands roll back through `Store.Update`. Pipeline versions, queued inputs, CPU quotas, output-path validation and launch de-duplication apply equally to AI and manual operations.

The tool surface does not include recursive chat, generic shell execution or opening arbitrary browser windows. Model configuration tools can refer to an existing API-key environment variable or preserve a stored credential; raw keys and credential-store references are never returned by configuration tools. Initial model setup and direct credential entry remain in Preferences. Remote execution uses the configured SSH executor and its existing host-key checks.

## Instructions and model resolution

Every prompt states that titles, abstracts, logs and tool results are untrusted data, and that embedded instructions must be ignored. Agents should execute an authorized goal, ask in the conversation for missing facts, leave uncertain decisions pending, and distinguish saved/queued/running/completed states. Requests for explanation alone do not authorize mutations. Deleting a model requires the user to have asked for deletion.

Model resolution uses the agent's selected profile, or the first profile when it no longer exists or is unset. Readiness requires a base URL, model ID and a configured key source when authentication is configured. The assistant page links to **Preferences** to add a model, or to **Agent center** to change the selected model when profiles already exist.

To add a tool, register a read implementation in `agent.go` or a schema and command mapping in `agent_tools.go`, then add it to appropriate specialist scopes. The primary assistant receives the complete catalogue. New commands should reuse service validation, return concrete IDs/results, and include tests for partial updates, errors and project boundaries.
