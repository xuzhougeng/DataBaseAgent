# Workflow Guide

Start on **AI assistant** and describe your goal. Once a model and SSH access are configured, the Project Assistant can use tools to carry out the workflow without switching through forms. Select **Workspace chat** to create a project conversationally. The right-hand panel retains live work progress and monitoring controls.

The pages below show the same project state and provide the manual path: project → topic & search → review → pipeline → jobs → results, with servers and preferences at workspace level.

## 1. Create a project

**All projects → New project.** Give it a name and a research goal. Each project keeps its own topic, searches, candidates, review ledger, pipeline versions, conversations and artifacts; the overview page shows the current stage and the recommended next action.

## 2. Define the topic and search strategy

**Topic & search.** Record organism, data type and the inclusion criteria in plain language — the Reviewer and Search Strategist agents read them. Write a deliberately broad query, choose a source (Europe PMC, PubMed, GEO) and a fetch limit (1–1000), then run it. The result dialog reports hits, fetched and newly added records; the history table keeps every run. You can also import a JSON list. Details: [discovery.md](discovery.md).

## 3. Review candidates

**Review.** Candidates are grouped as pending, accepted, excluded. With a model configured, *Agent review* asks the Reviewer for suggestions on up to 20 pending records at a time; each suggestion shows confidence and a verbatim quote that was verified against the source text. Open the abstract and link, add the dataset accession, an optional direct download URL and sample groups, write your reason, and choose **Include**, **Exclude** or **Later**. Reasons are mandatory and become part of the provenance.

## 4. Register servers

**Servers → Add server.** Host, SSH user and port, optional private-key path, a dedicated absolute work directory, the CPU quota DBA may schedule and the maximum concurrent jobs. Confirm key-based login and the host fingerprint from a terminal first, then run **Connection check**; the details dialog shows OS, CPU count, required tools and free disk.

## 5. Define and lock the pipeline

**Pipeline.** Edit the ordered steps (name, tool and version, shell command), pick the server, set CPUs per job and project concurrency, list the outputs to bring back. **Save & lock** validates everything and creates an immutable version. **Create jobs** queues every accepted candidate that has not yet run with this version. Variables available to commands are documented in [pipelines.md](pipelines.md#environment-available-to-steps).

## 6. Supervise jobs

**Jobs.** The scheduler dispatches within server and project quotas, polls progress, stores the remote log tail, retries transient network failures with back-off and escalates everything else as a *help request*. A help request shows the evidence (error, log) and the phase to resume from; describe what you fixed on the server — or supply sample groups — and resume. **Ask the Operations Inspector** from the job dialog to get a diagnosis with concrete steps.

Closing the app does not interrupt remote processes; reopening re-attaches to them.

## 7. Collect results

**Results.** Outputs are transferred incrementally, verified with SHA-256 and listed with size, hash and archive time. Text files can be previewed (first 64 KiB); the project manifest can be exported as JSON for downstream database construction.

## Working with agents

Use the dedicated **AI assistant** page for conversations. The Project Assistant has the complete workflow toolset; specialists, requested with @ mentions in the same conversation, focus on search, review, pipelines and operations. Review decisions, pipeline changes and job creation can be recorded directly from the conversation; missing facts are requested there. The other pages remain available to inspect or change the results manually. The progress panel shows active tools, recent events and help requests. Add model profiles in **Preferences**. In **Agent center**, right-click a role card to edit its model, instructions and tool step limit in a dialog; a normal click opens the same dialog. See [agents.md](agents.md) and [models.md](models.md).

## Scope and non-goals

DBA is a working first release that closes the single-server loop end to end. Known boundaries:

- **Execution**: SSH on Linux only; no Slurm/PBS, containers, memory or disk quotas, or system services. CPU budgets size dispatch capacity, not thread counts.
- **Lifecycle**: no remote kill/delete, no server editing after creation, no re-binding of existing jobs to newer pipeline versions. Lock a new version and queue new jobs instead.
- **Recovery**: a fixed strategy with a narrow transient-failure allow-list. Agents can edit and lock new pipeline versions or resume resolved jobs through tools; existing jobs retain their frozen commands.
- **Discovery**: no automatic slicing of very large result sets by date; raise the limit or tighten the query.
- **Data acquisition**: downloads are whatever the locked pipeline does; DBA does not infer FASTQ download plans from papers.
- **Remote requirements**: `sh`, `nohup`, `realpath`, `sha256sum` and the tools your steps use.
- **Storage**: one JSON document per workspace, suitable for thousands of candidates and jobs, not millions.
