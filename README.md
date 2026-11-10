<h1 align="center">DataBaseAgent (DBA)</h1>

<p align="center">
  A local-first, agent-driven workbench for building curated research datasets —<br>
  from data collection and analysis to websites designed from data and domain skills.
</p>

<p align="center">
  <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Wails v3" src="https://img.shields.io/badge/Wails-v3.1-DF0000">
  <img alt="Platforms" src="https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-4c8a6a">
  <img alt="LLM protocols" src="https://img.shields.io/badge/LLM%20protocols-OpenAI%20%7C%20Responses%20%7C%20Anthropic%20%7C%20Gemini-6f42c1">
  <img alt="No npm dependencies" src="https://img.shields.io/badge/frontend-0%20npm%20dependencies-informational">
</p>

<p align="center">
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="docs/architecture.md">Architecture</a> ·
  <a href="docs/agents.md">Agents</a> ·
  <a href="docs/models.md">Model Providers</a> ·
  <a href="docs/pipelines.md">Pipelines & Remote Execution</a> ·
  <a href="docs/build.md">Build & Install</a>
</p>

---

DBA turns a research question into a reproducible dataset. You define the topic and inclusion criteria; DBA runs broad literature and repository searches, lets a reviewer agent propose include/exclude decisions backed by verbatim evidence, locks an immutable processing pipeline, dispatches jobs to your Linux servers over SSH, supervises them with bounded automatic recovery, and archives every output locally with SHA-256 verification and a machine-readable provenance manifest.

DBA also starts from results a researcher has already analyzed. The Agent inspects the data, reads a relevant domain SKILL, and writes a website suited to the available results and the intended audience. The broader design includes both static and dynamic websites; the current implemented and tested publishing workflow produces standalone static HTML.

Everything runs on your machine. DBA writes workspace state to a single JSON file atomically and saves API keys in the system credential store. The AI assistant reads and modifies workspace data through role-scoped tools backed by the same validated commands as the manual pages.

<p align="center"><img src="assets/DBA-overview.jpg" width="960" alt="DBA project overview in English for the GPSAdb 2.0 SK-N-SH perturbation dataset archive"></p>

## Design principles

| Principle | What it means in practice |
| --- | --- |
| **A goal can drive the workflow** | Agents can record decisions, configure pipelines and create or resume jobs through validated tools. Manual pages remain available, and decisions retain their reasons and actor. |
| **Data and domain knowledge guide website design** | The Agent chooses layout, plots and interactions from the actual data and domain SKILL. Skills supply interpretation rules and guidance; optional fixed templates remain available. |
| **Evidence or it did not happen** | A reviewer suggestion is only stored if its quoted evidence appears verbatim in the title or abstract; otherwise it is downgraded to *uncertain*. |
| **Immutable execution snapshots** | A job carries a frozen copy of its pipeline version, input record and server. Editing the draft never alters running or past jobs. |
| **Bounded, observable recovery** | Only a short allow-list of network-class failures is retried, with exponential back-off and a hard cap. Everything else — unknown state, command errors, exhausted retries — becomes a help request with the full log attached. |
| **Least-privilege agents** | Each preset agent has a fixed tool scope. Project commands resolve a project ID and validate record membership; tool results are treated as untrusted data. |
| **Local, atomic, inspectable state** | One `workbench.json`, written via temp-file + rename with transactional rollback; results live in plain directories with per-file checksums and a `run_manifest.json`. |

## How it works

```
 ┌──────────┐   ┌─────────────┐   ┌──────────────┐   ┌───────────────┐   ┌───────────────┐   ┌──────────────┐
 │  Define  │ → │ Broad search│ → │ Evidence-    │ → │ Lock pipeline │ → │ Remote jobs   │ → │ Verified     │
 │  topic   │   │ EPMC/PubMed │   │ backed review│   │ (versioned)   │   │ via SSH, with │   │ local archive│
 │ criteria │   │ GEO / import│   │ AI / manual  │   │               │   │ supervision   │   │ + manifest   │
 └──────────┘   └─────────────┘   └──────────────┘   └───────────────┘   └───────────────┘   └──────────────┘
```

```
                       ┌──────────────────────────── Desktop (Wails v3) / Browser preview ────────────────────────────┐
                       │  Vanilla HTML/CSS/JS · 14 views · polls /api/state · zero npm dependencies                   │
                       └───────────────────────────────────────────┬──────────────────────────────────────────────────┘
                                                                   │ loopback HTTP, same-origin enforced
 ┌─────────────────────────────────────────────────────────────────▼─────────────────────────────────────────────────┐
 │  Go core (internal/workbench)                                                                                     │
 │                                                                                                                   │
 │   Store ── atomic JSON state, schema migration, transactional updates                                             │
 │   Service ─ command dispatcher, validation, quotas, pipeline locking                                              │
 │   Loop ──── scheduler tick: dispatch · poll · classify failures · retry · sync · escalate                          │
 │   Discovery  Europe PMC · PubMed · GEO adapters, cursor paging, de-duplication                                     │
 │   Agents ── bounded tool-use loop · 5 preset personas · read/write tools · per-call trace                         │
 │   LLM ───── protocol adapters: OpenAI Chat Completions · OpenAI Responses · Anthropic Messages · Google Gemini     │
 │   Executor  SSH: idempotent submission, nohup, exit-code marker, resumable transfer, SHA-256                       │
 └─────────────────────────────────────┬──────────────────────────────────────┬──────────────────────────────────────┘
                                       │ ssh (BatchMode, strict host keys)    │ HTTPS (proxy-aware)
                              ┌────────▼────────┐                    ┌────────▼────────┐
                              │ Linux compute   │                    │ LLM endpoints · │
                              │ nodes           │                    │ EPMC · NCBI     │
                              └─────────────────┘                    └─────────────────┘
```

## Feature highlights

- **Multi-project workspace** — independent topic, search strategy, review ledger, pipeline versions and artifacts per project; cross-project server quotas.
- **Discovery adapters** — Europe PMC (cursor paging), PubMed (E-utilities), GEO (DataSets) and JSON import; hit count vs. fetched count recorded per search, automatic merge on normalized title / DOI / accession.
- **Evidence-verified review** — suggestions carry a confidence level and a quote that is checked verbatim against the source text before being stored.
- **Versioned pipelines** — shell steps with named tools, CPU and concurrency budgets, declared outputs; locking produces an immutable version that jobs bind to.
- **Supervised remote execution** — idempotent submission markers, durable exit codes, PID liveness checks, transient-failure classification, exponential back-off, help requests with resume-from-phase.
- **Verified archive** — per-file SHA-256, resumable transfer with `.partial` files, 100 GiB per-file ceiling, path confinement on both ends, `run_manifest.json` provenance.
- **Agent team** — five preset personas with distinct icons, tool scopes and models; a tool-use loop with step budgets, forced final answers and a persisted trace of every call.
- **Provider-agnostic LLM layer** — one neutral conversation model, four wire protocols, per-agent model selection and direct API key entry.
- **MCP plugins** — GitHub and Cloudflare presets, custom Streamable HTTP / STDIO servers, browser OAuth sign-in or secure token storage, connection tests, tool filters, and traced on-demand calls from the main assistant. [Setup guide](docs/mcp.md)
- **Local & portable** — single binary, embedded assets, no database server, works offline except for discovery and model calls.

## Agents

| Agent | Icon | Purpose | Tools |
| --- | --- | --- | --- |
| Project Assistant | 🤖 | Executes the complete workflow from a goal | all application tools |
| Search Strategist | 🧭 | Saves search strategy, runs discovery and imports candidates | project, search, import |
| Reviewer | 📋 | Records decisions, reasons and known sample groups; also powers batch suggestions | project, candidates, review |
| Pipeline Advisor | 🔀 | Configures, locks and launches pipelines | project, pipeline, servers, jobs |
| Operations Inspector | 🔧 | Checks servers, configures monitoring and resumes resolved jobs | jobs, servers, settings, events, outputs |

Each project has one conversation on the **AI assistant** page, led by the Project Assistant. Type `@` or click **@ Subagent** to mention `@scout`, `@reviewer`, `@pipeline` or `@operator`; the main agent can also delegate autonomously. Specialists use their own models and scoped tools. Their results and traces stay in expandable task cards in the shared conversation, and the main agent summarizes and continues. Multiple mentions run sequentially, with at most eight child tasks per turn and no recursive delegation. Existing role histories appear together without data migration. Click role cards in **Agent center** to configure them.

The right panel retains work progress, monitoring and help requests. Other pages show the same state and support manual edits. Workspace chat can start before a project exists.

Chats have independent, searchable histories with rename, pin, archive, and export actions. Continue sending while the agent works: each project processes its durable message queue in order, and waiting messages can be edited or removed. The execution timeline shows tool status, duration, arguments, and results. Stopping or failing pauses subsequent messages; restarting preserves the queue and marks interrupted requests for manual review. Drafts and the selected chat survive page reloads.

Each agent runs a bounded loop: *model → tool calls → results → model*, up to a configurable step budget, after which a final answer is forced. Every tool invocation (arguments, result excerpt, latency, error flag) is stored with the reply and can be expanded in the chat. See [docs/agents.md](docs/agents.md).

## Model providers

| Protocol | Works with | Endpoint DBA calls |
| --- | --- | --- |
| `openai` — Chat Completions | OpenAI, DeepSeek, Qwen, Ollama, vLLM, OpenRouter, LM Studio, any compatible gateway | `{base}/chat/completions` |
| `openai-responses` — Responses | OpenAI Responses API and gateways that expose it | `{base}/responses` |
| `anthropic` — Messages | Anthropic API and compatible proxies | `{base}/messages` |
| `gemini` — generateContent | Google AI Studio / Gemini API | `{base}/models/{model}:generateContent` |

Add model profiles in **Preferences** and enter the API key directly. Leave the key blank for services that run without authentication. Existing profiles can still read keys from their configured environment variables. [Model provider guide](docs/models.md)

## Quick start

**Prerequisites:** Go 1.26+ to build; an `ssh` client for remote execution; platform WebView (WebView2 on Windows, WebKit on macOS, GTK4 + WebKitGTK on Linux). No Node.js, no C toolchain for Windows builds.

Run the browser preview (same backend, same UI, no native toolchain):

```bash
go run ./cmd/dba-server -port 3415 -workspace .tmp/dev-workspace
```

Build the desktop app for the current platform:

```bash
scripts/build.sh
```

Windows:

```powershell
./scripts/build.ps1
```

Installers, cross-compilation and workspace locations are covered in [docs/build.md](docs/build.md).

Use **All projects → Manage projects** to confirm moving a project and its records to Trash, or **Trash → Restore project** to recover it. Local and remote files are retained, and deletion does not stop remote processes. Unfinished jobs (including queued, retrying, syncing and needs-help jobs), unfinished project AI messages, or ongoing AI, search or review work prevent deletion. Trash persists across restarts.

### GPSAdb example

GPSAdb 2.0 curates human cell-line RNA-seq datasets for single-gene perturbations. The `2026.10.22` data release contains 7,665 datasets and 42,235 samples covering 2,810 perturbed genes. [GPSAdb 2.0 paper](https://doi.org/10.1093/nar/gkaf1077) · [Versioned data release](https://doi.org/10.5281/zenodo.23585244)

The screenshot shows a DBA project archiving three SK-N-SH contrasts from that release. A locked pipeline checks the published sample assignments against the count columns and archives the count matrix, sample sheet and per-sample summaries for each contrast. [PLLP](https://gpsadb.gzdlab.com/dataset/D25938) · [SUV39H1](https://gpsadb.gzdlab.com/dataset/D26999) · [TSPYL5](https://gpsadb.gzdlab.com/dataset/D27545)

The companion manuscript case follows a FAM167A knockdown from GEO screening through a help request, human-confirmed sample groups and checksum-verified archiving. [Reproduce the case](docs/manuscript/gpsadb-case/README.md) · [DBA workflow guide](docs/workflow.md)

## Documentation

| Document | Covers |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | Components, data model, job state machine, persistence, security boundaries |
| [docs/agents.md](docs/agents.md) | Agent loop, presets, tool catalogue, guardrails, trace format, customization |
| [docs/models.md](docs/models.md) | Protocol adapters, request mapping, keys, proxies, provider recipes |
| [docs/mcp.md](docs/mcp.md) | GitHub / Cloudflare presets, custom MCP servers, authentication, tool access and limitations |
| [docs/discovery.md](docs/discovery.md) | Search adapters, paging, limits, de-duplication, import format |
| [docs/pipelines.md](docs/pipelines.md) | Pipeline contract, environment variables, remote protocol, recovery, sync, manifest |
| [docs/build.md](docs/build.md) | Platform matrix, build scripts, installers, cross-compilation, workspaces |
| [docs/workflow.md](docs/workflow.md) | End-to-end user guide, current scope and non-goals |

## Verification

```bash
go test ./... && go test -race ./internal/workbench && go vet ./...
node --check frontend/workbench.js && node --check frontend/conversations.js && node --check frontend/mcp.js
node scripts/check-i18n.js && node --test scripts/test-conversations.js
```

The suite exercises project isolation, the recorded-inclusion gate, immutable pipeline snapshots, cross-project server quotas, no-resubmission on lost acknowledgements, bounded recovery and hand-off, transfer recovery without re-execution, restart resumption, persistence rollback and corruption detection, discovery de-duplication and fetch limits, evidence verification, same-origin enforcement, agent tool scoping and tracing, all four model protocols, schema migration, conversation-driven workflows and retained traces after failures — using a controllable executor and a stubbed HTTP transport. Real servers and model endpoints still need to be validated with your own configuration.

## Scope

Website authoring currently targets browser-resident static outputs. Large-scale dynamic website construction is a development and evaluation direction, as described below.

This is a working first release aimed at closing the single-server loop end to end. The executor is plain SSH on Linux (no Slurm, containers or memory/disk quotas); CPU budgets govern dispatch capacity, not thread counts. Remote processes keep running when DBA is closed and are re-attached on relaunch. Agents can save and lock pipeline changes through tools; existing jobs retain their frozen commands. See [docs/workflow.md#scope-and-non-goals](docs/workflow.md#scope-and-non-goals) for the full list.

## Repository layout

```
main.go                 Wails desktop entry point (single-instance, workspace flag)
cmd/dba-server/         Browser preview server bound to 127.0.0.1
internal/workbench/     Core: store · service · loop · discovery · agent · llm · ssh · http
frontend/               Embedded UI: index.html · styles.css · i18n.js · workbench.js · conversations.js · mcp.js · examples.js
assets/                 README screenshot and application icon sources
scripts/                build.sh · build.ps1 · installer.nsi · check-i18n.js · test-conversations.js
docs/                   Topic documentation
examples/               Design-demo files for both project modes, embedded for the in-app Examples page (pbmc5k/ is imported manually)
```

## Data-driven website design and domain SKILLs

The design is **user goal + data + domain SKILL → Agent-designed website**. The Agent first inspects fields, sample records, missing values and summaries, then decides which views and interactions the data support. A domain SKILL provides interpretation rules, appropriate visualization choices and constraints on unsupported claims. The Agent writes the HTML/CSS/JavaScript; DBA manages input data, tool execution, preview, iteration and export. Skills can therefore support different domains without adding a dedicated renderer for each one.

Choose **Existing data** for results you have already analyzed. Import CSV/TSV/JSON, click **Save data for Agent design**, and ask the assistant to build or revise the website. The complete data is injected into the generated page, so the Agent does not need to reproduce every record in its response. Fixed research-catalogue and literature-library templates remain available for simple manual builds. Choose **Collect data** for discovery, review, processing and archival; moving an archived table into an existing-data project is currently a manual export/import step.

The `single-cell-explorer` SKILL is a domain example. It guides the Agent to distinguish embeddings, clusters, cell-type annotations and expression values, and to offer only views supported by the supplied columns. In the [PBMC5k walkthrough](docs/walkthroughs/pbmc5k/README.md), an actual DBA Agent generated a t-SNE explorer from 5,025 cells and eight graph clusters, with filtering, barcode search, cell details, zoom/reset and pagination. This validates one processed-data case, not raw sequencing analysis or general website quality. Input data, code, screenshots and verification limits are retained in the walkthrough.

SKILLs load on demand or through `$skill-name`. The generated website retains its data and code snapshot; editing a SKILL does not change an existing build. See [SKILLs and website authoring](docs/skills.md) and [examples](examples/README.md).

### Current limitation and optimization direction

Static describes how the site runs, not whether it is interactive: code and data travel together, and the browser handles the supported interactions without a database or application backend. The current importer accepts one table of up to 2 MiB, 10,000 rows and 100 columns; these are implementation limits, not a measured scalability guarantee.

**Dynamic websites for large datasets are part of DBA's intended scope and remain a current limitation.** Large expression matrices, linked tables or workloads that exceed browser memory and rendering capacity may require database-backed storage, indexed queries, backend APIs, on-demand or chunked loading, and server-side computation. Multi-user permissions and continuous updates can also require a dynamic architecture. DBA does not yet generate, deploy and operate that full stack, and no large-scale dynamic-site benchmark has been performed.

Future work will extend the Agent's tools and domain SKILLs to select an architecture from data size and access requirements, build and validate the storage/API/frontend components, and support deployment, monitoring and updates. Evaluation should measure query latency, memory, concurrent access, correctness and operating cost on progressively larger datasets. Static publishing is the first demonstrated path within this broader design.

## Citation

If you use DBA in your research, please cite the accompanying [manuscript](docs/manuscript/manuscript.pdf):

> Qin, G., & Xu, Z.-G. (2026). DataBaseAgent: a local desktop workbench for building and publishing topic-specific biological data repositories with human-gated AI agents. Manuscript submitted to bioRxiv.

The manuscript has been submitted to bioRxiv. A public bioRxiv DOI has not yet been verified; this citation uses the manuscript hosted in this repository. The submission identifier is not a DOI. Citation metadata is also available in [CITATION.cff](CITATION.cff).

```bibtex
@unpublished{qin2026databaseagent,
  author = {Qin, Guihui and Xu, Zhou-Geng},
  title = {{DataBaseAgent}: a local desktop workbench for building and publishing topic-specific biological data repositories with human-gated AI agents},
  year = {2026},
  note = {Manuscript submitted to bioRxiv},
  url = {https://github.com/xuzhougeng/DataBaseAgent/blob/main/docs/manuscript/manuscript.pdf}
}
```
