# Pipelines and Remote Execution

A pipeline is a short, explicit contract: ordered shell steps, a server, resource budgets and the files that must exist when the run is over. DBA runs it on your Linux machines over plain SSH and treats the result as data only after every declared output has been hashed, transferred and verified.

## Pipeline contract

| Field | Rules |
| --- | --- |
| Name | Free text. |
| Server | A registered server; must exist at lock time. |
| CPUs per job | 1 … server CPU quota. Exposed to steps as `DBA_CPUS`; it governs dispatch capacity, not thread counts — pass it to your tools explicitly. |
| Concurrency | 1 … 128 jobs of this project at once. |
| Steps | ≥ 1. Each has a name, a tool/version label and a shell command. Executed in order with `set -e`; the first failure stops the run. |
| Outputs | ≥ 1 relative paths, unique, `[A-Za-z0-9._-/]` only, no `..`, not `run_manifest.json` (reserved). |

The default draft contains a `curl --continue-at -` download step and an empty "process" step to be defined by the user.

### Locking and versions

Saving writes the draft. **Locking** validates the draft, assigns the next version number and stores an immutable copy under `Versions`. Jobs reference a version by id and carry a full snapshot of it. Editing after locking creates a new unlocked draft; running jobs keep their version, and only candidates not yet run with the *new* version are queued when you launch again.

### Environment available to steps

| Variable | Value |
| --- | --- |
| `DBA_ACCESSION` | Accession of the input dataset (or candidate id if none). |
| `DBA_SOURCE_URL` | Direct download URL entered during review (may be empty). |
| `DBA_INPUT` | `<workDir>/<project>/<job>/input` — shared across attempts, so downloads survive retries. |
| `DBA_OUTPUT` | `<workDir>/<project>/<job>/attempt-N/output` — the working directory of every step. |
| `DBA_SAMPLE_GROUPS` | Sample grouping entered during review or when resolving a help request. |
| `DBA_CPUS` | CPU budget of this job. |

Output paths are resolved relative to `DBA_OUTPUT`.

## Servers

Register a Linux host with user, port, optional private-key path, a dedicated absolute work directory, a CPU quota and a maximum number of concurrent jobs. SSH is invoked with `-T -o BatchMode=yes -o StrictHostKeyChecking=yes -o ConnectTimeout=12 -o ServerAliveInterval=15 -o ServerAliveCountMax=2`; verify key-based login and accept the host key from a terminal first.

**Connection check** creates the work directory, confirms it is writable and reports `uname -sr`, the CPU count, the presence of `nohup`, `sha256sum` and `realpath`, and `df -h`. Remote requirements: `sh`, `nohup`, `realpath`, `sha256sum` and whatever tools your steps use.

Quotas are enforced across projects: a server never runs more than `maxJobs` jobs or more than `cpuQuota` CPUs worth of jobs at once, regardless of which project queued them.

## Remote protocol

Each attempt lives in `<workDir>/<projectId>/<jobId>/attempt-N/`.

**Start** (idempotent):

```
mkdir -p attempt-N && cd attempt-N
if [ -e submitted ]; exit 0                       # already running: never start twice
mkdir .submit-lock, or reclaim it when .submit-lock/owner is no longer alive, else exit 75
echo $$ > .submit-lock/owner                      # released on exit, HUP, INT and TERM
cat > run.sh <<'DBA_SCRIPT_<job>_<attempt>'        # generated script, unique heredoc marker
…
printf submitted > submitted
nohup sh run.sh > run.log 2>&1 < /dev/null &
echo $! > pid
<start time of $! from /proc/$!/stat> > pid.start  # empty when /proc is unavailable
```

A lost SSH acknowledgement therefore cannot cause a duplicate execution: the next start finds `submitted` and returns immediately. A submitter whose SSH session is killed with `SIGKILL` cannot release the lock; the next start sees that the recorded owner is gone and reclaims it instead of failing until the job escalates.

`run.sh` exports the `DBA_*` variables, creates the input and output directories, runs the steps inside a `set -e` subshell in `DBA_OUTPUT`, prints a `[DBA step i] name · tool` banner before each step, and finally writes the exit code to `exitcode.tmp` and renames it to `exitcode` (atomic marker).

**Poll** reports one of:

- `EXIT:<code>` when `exitcode` exists,
- `RUNNING` when `pid` is alive and its start time still matches `pid.start` (after a remote reboot the PID may belong to an unrelated process; jobs submitted before `pid.start` existed are checked by PID only),
- `UNKNOWN` otherwise (no marker, process gone),

followed by the last 64 KiB of `run.log`, which is stored on the job and shown in the UI.

**Sync**, for each declared output:

1. Remote: resolve with `realpath` and refuse anything outside `output/` (exit 65); report `sha256sum` and byte size (≤ 100 GiB).
2. Local: if the file already exists with the same hash, skip. Otherwise stream from the remote with `tail -c +<offset+1>` into `<name>.partial`, resuming from the local partial size.
3. Verify size and SHA-256; on mismatch truncate the partial and signal a retry. On success rename into place. If the stream stops early, the bytes already written are kept for the next attempt.

Local results are stored under `<workspace>/projects/<projectId>/results/<jobId>/` next to `run_manifest.json`.

## Supervision and recovery

The loop (see [architecture.md](architecture.md#job-state-machine)) classifies what it observes:

| Observation | Action |
| --- | --- |
| `EXIT:0` | Move to `syncing`. |
| Non-zero exit and the log tail matches the transient pattern, and `attempt ≤ maxRetries` | `retry_wait` with exponential back-off, then a fresh attempt (new `attempt-N`, same `input/`). |
| Non-zero exit otherwise | Help request; resume phase `queued`. |
| `UNKNOWN` | Help request asking the user to check the remote process; resume phase `running` (no re-execution). |
| SSH / transfer error in any phase | `failures += 1`, back-off, stay in phase; after `maxRetries` consecutive failures, help request with the current phase as resume point. |
| Sync error after new bytes were kept locally | `failures` restarts at 1 and the transfer resumes after back-off. A large file over a slow link is not escalated while it keeps advancing; a checksum mismatch or a file that changed during transfer resets the partial and counts as a failure. |

The transient pattern is intentionally narrow:

```
curl: \((6|7|18|28|35|52|56)\) | connection (reset|timed out|refused) | temporary failure | network is unreachable
```

Resolving a help request requires a note describing what was done and optionally new sample groups. The job resumes in its recorded phase: a `running` job is re-polled, a `syncing` job re-transfers, a `queued` job gets a new attempt. The remote process is never restarted implicitly.

Closing DBA does not stop remote work; on the next launch the loop re-attaches to every in-flight job from the persisted state.

## Provenance manifest

`run_manifest.json` is written before the job is marked `succeeded`:

```json
{
  "schema": 3,
  "job_id": "job_…",
  "dataset_id": "candidate_…",
  "source_accession": "GSE…",
  "source_url": "https://…",
  "organism": "…",
  "review_decision": "accepted",
  "review_note": "…",
  "sample_groups": "sample, group\n…",
  "pipeline": { "id": "pipeline_…", "version": 1, "name": "…", "steps": [ … ], "outputs": [ … ], "cpus": 8, "concurrency": 2 },
  "server_id": "server_…",
  "attempt": 1,
  "output_files": [ { "name": "expression_matrix.tsv", "path": "…", "sha256": "…", "size": 12345 } ],
  "completed_at": "2026-11-05T10:00:00Z"
}
```

The project-level export (the `exportManifest` action, written to `projects/<project id>/results/dba_manifest.json`) bundles the project, its jobs (with key paths removed) and all artifacts into one JSON document for downstream database construction.

## Limitations

- Executor: plain SSH on Linux only. No Slurm/PBS, containers, memory or disk quotas, or systemd services.
- No remote kill/delete, no server editing after creation, no automatic migration of running jobs to a new pipeline version.
- Raw-data acquisition is whatever your locked download step does; DBA does not infer FASTQ download plans from papers.
