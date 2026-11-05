package workbench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"time"
)

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	defer s.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := s.Store.Snapshot()
			last, _ := time.Parse(time.RFC3339, state.LastTick)
			if !state.Settings.Paused && time.Since(last) >= time.Duration(state.Settings.IntervalSeconds)*time.Second {
				_ = s.Tick(ctx)
			}
		}
	}
}

func (s *Service) begin(jid string) bool {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.inFlight[jid] || len(s.inFlight) >= 8 {
		return false
	}
	s.inFlight[jid] = true
	return true
}
func (s *Service) end(jid string) { s.opMu.Lock(); delete(s.inFlight, jid); s.opMu.Unlock() }

func (s *Service) Tick(ctx context.Context) error {
	if !s.tickMu.TryLock() {
		return nil
	}
	defer s.tickMu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := s.Store.Update(func(st *State) error {
		st.LastTick = now()
		for i := range st.Jobs {
			j := &st.Jobs[i]
			if j.Status == "retry_wait" {
				at, _ := time.Parse(time.RFC3339, j.RetryAt)
				if !time.Now().Before(at) {
					j.Status = "queued"
				}
			}
		}
		return nil
	}); e != nil {
		return e
	}
	state := s.Store.Snapshot()
	// Observe existing work before allocating additional server resources.
	for _, j := range state.Jobs {
		if j.Status != "starting" && j.Status != "running" && j.Status != "syncing" {
			continue
		}
		at, _ := time.Parse(time.RFC3339, j.RetryAt)
		if time.Now().Before(at) || !s.begin(j.ID) {
			continue
		}
		go s.operate(j)
	}
	for _, j := range state.Jobs {
		if j.Status != "queued" || !s.begin(j.ID) {
			continue
		}
		reserved := false
		e := s.Store.Update(func(st *State) error {
			cur, e := jobByID(st, j.ID)
			if e != nil {
				return e
			}
			if cur.Status != "queued" {
				return nil
			}
			server, e := serverByID(st, cur.Server.ID)
			if e != nil {
				return e
			}
			usedCPU, usedJobs, projectJobs := 0, 0, 0
			for _, other := range st.Jobs {
				if other.Status == "starting" || other.Status == "running" || other.Status == "needs_help" && (other.ResumeStatus == "starting" || other.ResumeStatus == "running") {
					if other.Server.ID == server.ID {
						usedCPU += other.Pipeline.CPUs
						usedJobs++
					}
					if other.ProjectID == cur.ProjectID {
						projectJobs++
					}
				}
			}
			if usedJobs >= server.MaxJobs || usedCPU+cur.Pipeline.CPUs > server.CPUQuota || projectJobs >= cur.Pipeline.Concurrency {
				return nil
			}
			cur.Attempt++
			cur.Failures = 0
			cur.Status = "starting"
			cur.Error = ""
			cur.RetryAt = ""
			cur.UpdatedAt = now()
			cur.RemoteDir = path.Join(cur.Server.WorkDir, cur.ProjectID, cur.ID, fmt.Sprintf("attempt-%d", cur.Attempt))
			j = *cur
			reserved = true
			event(st, j.ProjectID, j.ID, "dispatch", "任务开始提交", fmt.Sprintf("%s · 第 %d 次 · 流程 v%d", j.Accession, j.Attempt, j.Pipeline.Version))
			return nil
		})
		if e != nil || !reserved {
			s.end(j.ID)
			if e != nil {
				return e
			}
			continue
		}
		go s.operate(j)
	}
	return nil
}

func (s *Service) operate(j Job) {
	defer s.end(j.ID)
	timeout := 45 * time.Second
	if j.Status == "syncing" {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	switch j.Status {
	case "starting":
		e := s.Runner.Start(ctx, j)
		if e != nil {
			s.operationError(j, e)
			return
		}
		_ = s.Store.Update(func(st *State) error {
			cur, e := jobByID(st, j.ID)
			if e != nil {
				return e
			}
			cur.Status = "running"
			cur.Failures = 0
			cur.Error = ""
			cur.RetryAt = ""
			cur.UpdatedAt = now()
			event(st, j.ProjectID, j.ID, "dispatch", "远端已接受任务", j.RemoteDir)
			return nil
		})
	case "running":
		obs, e := s.Runner.Poll(ctx, j)
		if e != nil {
			s.operationError(j, e)
			return
		}
		_ = s.Store.Update(func(st *State) error {
			cur, e := jobByID(st, j.ID)
			if e != nil {
				return e
			}
			cur.Log = obs.Log
			cur.Failures = 0
			cur.Error = ""
			cur.RetryAt = ""
			cur.UpdatedAt = now()
			switch obs.Status {
			case "succeeded":
				cur.Status = "syncing"
				event(st, j.ProjectID, j.ID, "sync", "远端处理完成，准备同步", j.Accession)
			case "failed":
				cur.Error = fmt.Sprintf("远端命令退出码 %d", obs.ExitCode)
				if transientFailure.MatchString(obs.Log) && cur.Attempt <= st.Settings.MaxRetries {
					cur.Status = "retry_wait"
					cur.RetryAt = retryTime(cur.Attempt)
					event(st, j.ProjectID, j.ID, "repair", "检测到可恢复故障，等待重试", cur.Error)
				} else {
					requestHelp(st, cur, "命令执行失败，请检查日志、输入数据与运行环境。", "queued")
				}
			case "unknown":
				requestHelp(st, cur, "无法确认远端进程是否结束。请检查服务器上的提交标记和进程，避免重复计算。", "running")
			}
			return nil
		})
	case "syncing":
		artifacts, e := s.Runner.Sync(ctx, j, s.Store.Root())
		if e != nil {
			s.operationError(j, e)
			return
		}
		if e = writeManifest(s.Store.Root(), j, artifacts); e != nil {
			s.operationError(j, e)
			return
		}
		_ = s.Store.Update(func(st *State) error {
			cur, e := jobByID(st, j.ID)
			if e != nil {
				return e
			}
			cur.Status = "succeeded"
			cur.Error = ""
			cur.Failures = 0
			cur.RetryAt = ""
			cur.UpdatedAt = now()
			existing := map[string]bool{}
			for _, a := range st.Artifacts {
				existing[a.ID] = true
			}
			for _, a := range artifacts {
				if !existing[a.ID] {
					st.Artifacts = append(st.Artifacts, a)
				}
			}
			event(st, j.ProjectID, j.ID, "success", "分析产物已校验并同步到本地", fmt.Sprintf("%s · %d 份文件", j.Accession, len(artifacts)))
			return nil
		})
	}
}

var transientFailure = regexp.MustCompile(`(?i)(curl: \((6|7|18|28|35|52|56)\)|connection (reset|timed out|refused)|temporary failure|network is unreachable)`)

func retryTime(n int) string {
	return time.Now().UTC().Add(time.Duration(min(300, 10*(1<<min(n, 5)))) * time.Second).Format(time.RFC3339)
}
func requestHelp(st *State, j *Job, reason, resume string) {
	j.Status = "needs_help"
	j.ResumeStatus = resume
	j.Help = reason
	j.UpdatedAt = now()
	event(st, j.ProjectID, j.ID, "help", "任务需要人工协助", reason+" "+j.Error)
}
func (s *Service) operationError(j Job, err error) {
	if s.ctx.Err() != nil {
		return
	} // Shutdown preserves the last durable state for the next launch.
	_ = s.Store.Update(func(st *State) error {
		cur, e := jobByID(st, j.ID)
		if e != nil {
			return e
		}
		cur.Failures++
		cur.Error = err.Error()
		cur.UpdatedAt = now()
		if cur.Failures > st.Settings.MaxRetries {
			requestHelp(st, cur, "自动恢复次数已用尽，请检查连接、日志或输出文件。", j.Status)
		} else {
			cur.RetryAt = retryTime(cur.Failures)
			event(st, j.ProjectID, j.ID, "repair", "保留现场，等待恢复", fmt.Sprintf("%s · %d/%d", err.Error(), cur.Failures, st.Settings.MaxRetries))
		}
		return nil
	})
}

// ExportManifest writes the project's provenance manifest into its local results directory and returns the path.
// ponytail: written server-side because the macOS WebView (WKWebView) ignores blob downloads.
func (s *Service) ExportManifest(pid string) (string, error) {
	state := s.Store.Snapshot()
	p, e := projectByID(&state, pid)
	if e != nil {
		return "", e
	}
	jobs, artifacts := []Job{}, []Artifact{}
	for _, j := range state.Jobs {
		if j.ProjectID == pid {
			j.Server.KeyPath = ""
			jobs = append(jobs, j)
		}
	}
	for _, a := range state.Artifacts {
		if a.ProjectID == pid {
			artifacts = append(artifacts, a)
		}
	}
	b, e := json.MarshalIndent(map[string]any{"schema": SchemaVersion, "project": p, "jobs": jobs, "artifacts": artifacts, "exportedAt": now()}, "", "  ")
	if e != nil {
		return "", e
	}
	dir := filepath.Join(s.Store.Root(), "projects", pid, "results")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	name := filepath.Join(dir, "dba_manifest.json")
	return name, os.WriteFile(name, b, 0600)
}

func writeManifest(root string, j Job, artifacts []Artifact) error {
	dir := filepath.Join(root, "projects", j.ProjectID, "results", j.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	payload := map[string]any{"schema": SchemaVersion, "job_id": j.ID, "dataset_id": j.CandidateID, "source_accession": j.Accession, "source_url": j.Input.URL, "organism": j.Input.Organism, "review_decision": j.Input.Decision, "review_note": j.Input.ReviewNote, "sample_groups": j.Input.Groups, "pipeline": j.Pipeline, "server_id": j.Server.ID, "attempt": j.Attempt, "output_files": artifacts, "completed_at": now()}
	b, e := json.MarshalIndent(payload, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, "run_manifest.json"), b, 0600)
}

func (s *Service) Probe(ctx context.Context, sid string) error {
	state := s.Store.Snapshot()
	server, e := serverByID(&state, sid)
	if e != nil {
		return e
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, probeErr := s.Runner.Probe(probeCtx, *server)
	e = s.Store.Update(func(st *State) error {
		cur, e := serverByID(st, sid)
		if e != nil {
			return e
		}
		cur.Status = "online"
		cur.Details = out
		cur.CheckedAt = now()
		if probeErr != nil {
			cur.Status = "error"
			cur.Details = probeErr.Error()
		}
		event(st, "", "", "server", "服务器连接检查", cur.Name+" · "+cur.Status)
		return nil
	})
	if e != nil {
		return e
	}
	return probeErr
}
