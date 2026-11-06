package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func entrezDelay() <-chan time.Time { return time.After(400 * time.Millisecond) }

// ---- Agent presets: least-privilege tools. Default instructions live in agent_prompts.go;
// model, MaxSteps and System are user-editable.

var presetAgents = []Agent{
	{ID: "assistant", Name: "项目助手", Icon: "agent", MaxSteps: 20, Description: "根据目标执行从建项目、检索复核到流程运行与产物归档的完整流程。"},
	{ID: "scout", Name: "检索策略师", Icon: "compass", MaxSteps: 12, Description: "制定并保存检索策略，运行检索、导入候选并检查结果。", Tools: []string{"get_project", "list_searches", "list_candidates", "get_candidate", "update_project", "search", "import_candidates"}},
	{ID: "reviewer", Name: "复核员", Icon: "review", MaxSteps: 20, Description: "按标准核对原文，记录纳入决定、依据与已知样本分组。", Tools: []string{"get_project", "list_candidates", "get_candidate", "review_candidate"}},
	{ID: "pipeline", Name: "流程顾问", Icon: "flow", MaxSteps: 12, Description: "配置处理步骤、命令、资源与产物，锁定流程并创建任务。", Tools: []string{"get_project", "get_pipeline", "list_servers", "list_jobs", "get_job", "save_pipeline", "lock_pipeline", "launch_jobs", "probe_server", "list_artifacts"}},
	{ID: "operator", Name: "运维巡检员", Icon: "wrench", MaxSteps: 12, Description: "检查服务器与任务日志，调整巡检策略并恢复已解决的任务。", Tools: []string{"list_jobs", "get_job", "list_servers", "recent_events", "probe_server", "add_server", "get_settings", "update_settings", "resolve_job", "tick", "list_artifacts", "export_manifest"}},
}

var legacyAgentSystems = map[string]string{
	"assistant": "你是 DBA 本地数据库构建工作台的项目助手。帮助用户了解项目当前阶段、各环节状态与下一步该做什么。工作台流程：确定主题 → 偏宽检索 → 人工复核纳入 → 定义并锁定处理流程 → 远程下载与处理 → 校验同步产物。",
	"scout":     "你是文献与数据集检索策略师。根据项目的研究目标与纳入标准，结合检索历史和已获取候选的质量，评估现有检索式的召回与噪音，提出可直接使用的改进检索表达式（Europe PMC、PubMed 或 GEO 语法），并逐条说明修改理由。",
	"reviewer":  "你是候选数据集复核员。依据项目纳入标准，只根据标题与摘要判断候选是否应纳入，给出建议、置信度与可在原文中逐字核对的证据片段；不能推断缺失样本或捏造编号。资料不足时判为 uncertain。最终纳入由用户确认。",
	"pipeline":  "你是生物信息处理流程顾问。审阅项目的处理步骤、shell 命令、CPU 与并发配置以及产物契约，指出可能的错误（路径、线程数、缺失输出、环境变量 DBA_* 的使用）并给出具体修改建议。流程变更由用户在“处理流程”页面修改并锁定新版本。",
	"operator":  "你是远程任务运维巡检员。读取任务状态、运行日志、错误与服务器连接信息，判断失败属于网络类可恢复错误、环境或命令错误，还是需要科研判断的问题；给出最可能的原因、核对方法和在服务器上的具体修复步骤。用户修复后在任务详情中恢复任务。",
}
var legacyAgentSteps = map[string]int{"assistant": 6, "scout": 6, "reviewer": 4, "pipeline": 6, "operator": 8}

// withPresets returns presets in canonical order with the user's edits (system, model, steps) applied.
// Instructions that are still a shipped default follow lang; custom instructions are kept as written.
func withPresets(stored []Agent, lang string) []Agent {
	out := make([]Agent, 0, len(presetAgents))
	for _, p := range presetAgents {
		p.System = presetSystem(p.ID, lang)
		for _, a := range stored {
			if a.ID == p.ID {
				p.ModelID = a.ModelID
				if a.System != "" && !defaultAgentSystem(a.ID, a.System) {
					p.System = a.System
				}
				if a.MaxSteps > 0 && !(a.System == legacyAgentSystems[a.ID] && a.MaxSteps == legacyAgentSteps[a.ID]) {
					p.MaxSteps = a.MaxSteps
				}
				break
			}
		}
		out = append(out, p)
	}
	return out
}
func agentByID(st *State, aid string) (*Agent, error) {
	if aid == "" {
		aid = "assistant"
	}
	for i := range st.Settings.Agents {
		if st.Settings.Agents[i].ID == aid {
			return &st.Settings.Agents[i], nil
		}
	}
	return nil, errors.New("Agent 不存在")
}
func modelFor(st *State, a Agent) (ModelProfile, error) {
	if len(st.Settings.Models) == 0 {
		return ModelProfile{}, errors.New("请先在偏好设置中添加模型")
	}
	for _, m := range st.Settings.Models {
		if m.ID == a.ModelID {
			return m, nil
		}
	}
	return st.Settings.Models[0], nil
}

// ---- Read tools and validated application commands; project defaults come from the conversation.

type toolSpec struct {
	Workspace bool
	Action    string
	Desc      string
	Params    map[string]any
	Run       func(st *State, p *Project, args map[string]any) any
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}
func argStr(a map[string]any, k string) string { s, _ := a[k].(string); return s }
func argInt(a map[string]any, k string, def, max int) int {
	if f, ok := a[k].(float64); ok && int(f) > 0 {
		return min(int(f), max)
	}
	return def
}
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + strings.ToValidUTF8(s[len(s)-n:], "")
}
func candidateRow(c Candidate) map[string]any {
	return map[string]any{"id": c.ID, "title": c.Title, "accession": c.Accession, "source": c.Source, "organism": c.Organism, "year": c.Year, "suggestion": c.Suggestion, "decision": c.Decision}
}
func jobRow(j Job) map[string]any {
	return map[string]any{"id": j.ID, "accession": j.Accession, "status": j.Status, "attempt": j.Attempt, "failures": j.Failures, "error": j.Error, "help": j.Help, "server": j.Server.Name, "pipelineVersion": j.Pipeline.Version, "remoteDir": j.RemoteDir, "updatedAt": j.UpdatedAt}
}

var tools = map[string]toolSpec{
	"get_project": {Desc: "当前项目的研究目标、纳入标准、检索式与各阶段统计。", Run: func(st *State, p *Project, _ map[string]any) any {
		n := map[string]int{}
		for _, c := range p.Candidates {
			n[c.Decision]++
		}
		jobs := 0
		for _, j := range st.Jobs {
			if j.ProjectID == p.ID {
				jobs++
			}
		}
		return map[string]any{"id": p.ID, "name": p.Name, "mode": projectMode(p), "description": p.Description, "organism": p.Organism, "assay": p.Assay, "criteria": p.Criteria, "query": p.Query, "source": p.Source, "candidates": map[string]int{"total": len(p.Candidates), "pending": n["pending"], "accepted": n["accepted"], "excluded": n["excluded"]}, "searches": len(p.Searches), "pipelineLocked": p.Draft.Locked, "pipelineVersion": p.Draft.Version, "lockedVersions": len(p.Versions), "jobs": jobs}
	}},
	"list_candidates": {Desc: "分页列出候选（不含摘要），可按 decision 筛选。返回 total、offset 和当前页。", Params: obj(map[string]any{"decision": enumParam("pending", "accepted", "excluded", "all"), "limit": intParam(1, 50), "offset": intParam(0, 1000000)}), Run: func(_ *State, p *Project, a map[string]any) any {
		d, out := argStr(a, "decision"), []map[string]any{}
		for _, c := range p.Candidates {
			if d == "" || d == "all" || c.Decision == d {
				out = append(out, candidateRow(c))
			}
		}
		offset := min(argInt(a, "offset", 0, 1000000), len(out))
		page := out[offset:min(len(out), offset+argInt(a, "limit", 20, 50))]
		return map[string]any{"count": len(page), "total": len(out), "offset": offset, "candidates": page}
	}},
	"get_candidate": {Desc: "读取一条候选的完整信息，包括摘要、Agent 建议、复核决定及操作者。", Params: obj(map[string]any{"id": map[string]any{"type": "string"}}, "id"), Run: func(_ *State, p *Project, a map[string]any) any {
		for _, c := range p.Candidates {
			if c.ID == argStr(a, "id") || c.Accession != "" && c.Accession == argStr(a, "id") {
				c.Abstract = clip(c.Abstract, 4000)
				return c
			}
		}
		return map[string]string{"error": "候选不存在"}
	}},
	"list_searches": {Desc: "检索历史：检索式、来源、命中总数、获取数与新增数。", Run: func(_ *State, p *Project, _ map[string]any) any {
		return p.Searches[max(0, len(p.Searches)-20):]
	}},
	"get_pipeline": {Desc: "当前流程草稿（步骤与命令、服务器、CPU、并发、产物）及已锁定版本列表。", Run: func(st *State, p *Project, _ map[string]any) any {
		server := ""
		if s, e := serverByID(st, p.Draft.ServerID); e == nil {
			server = s.Name
		}
		versions := []map[string]any{}
		for _, v := range p.Versions {
			versions = append(versions, map[string]any{"version": v.Version, "name": v.Name, "steps": len(v.Steps), "createdAt": v.CreatedAt})
		}
		return map[string]any{"draft": p.Draft, "server": server, "versions": versions, "env": "DBA_ACCESSION DBA_SOURCE_URL DBA_INPUT DBA_OUTPUT DBA_SAMPLE_GROUPS DBA_CPUS"}
	}},
	"list_servers": {Desc: "执行服务器及其连接状态、配额与当前占用。", Run: func(st *State, _ *Project, _ map[string]any) any {
		out := []map[string]any{}
		for _, s := range st.Servers {
			cpu, jobs := 0, 0
			for _, j := range st.Jobs {
				if j.Server.ID == s.ID && (j.Status == "starting" || j.Status == "running") {
					cpu += j.Pipeline.CPUs
					jobs++
				}
			}
			out = append(out, map[string]any{"id": s.ID, "name": s.Name, "host": s.Host, "port": s.Port, "user": s.User, "workDir": s.WorkDir, "status": s.Status, "checkedAt": s.CheckedAt, "details": clip(s.Details, 600), "cpuQuota": s.CPUQuota, "maxJobs": s.MaxJobs, "usedCPU": cpu, "runningJobs": jobs})
		}
		return out
	}},
	"list_jobs": {Desc: "分页列出项目任务（不含日志）。status 可筛选，如 needs_help、running、succeeded。", Params: obj(map[string]any{"status": strParam("可选的任务状态"), "offset": intParam(0, 1000000), "limit": intParam(1, 50)}), Run: func(st *State, p *Project, a map[string]any) any {
		out := []map[string]any{}
		for _, j := range st.Jobs {
			if j.ProjectID == p.ID && (argStr(a, "status") == "" || j.Status == argStr(a, "status")) {
				out = append(out, jobRow(j))
			}
		}
		offset := min(argInt(a, "offset", 0, 1000000), len(out))
		return map[string]any{"total": len(out), "offset": offset, "jobs": out[offset:min(len(out), offset+argInt(a, "limit", 20, 50))]}
	}},
	"get_job": {Desc: "任务详情，含最近 4000 字节远端日志、错误、求助说明、输入与流程快照。", Params: obj(map[string]any{"id": map[string]any{"type": "string", "description": "任务 id 或数据编号"}}, "id"), Run: func(st *State, p *Project, a map[string]any) any {
		for _, j := range st.Jobs {
			if j.ProjectID == p.ID && (j.ID == argStr(a, "id") || j.Accession == argStr(a, "id")) {
				row := jobRow(j)
				row["log"] = tail(j.Log, 4000)
				row["input"] = map[string]any{"accession": j.Input.Accession, "downloadUrl": j.Input.DownloadURL, "groups": j.Input.Groups, "reviewNote": j.Input.ReviewNote}
				row["pipeline"] = j.Pipeline
				row["resumeStatus"] = j.ResumeStatus
				return row
			}
		}
		return map[string]string{"error": "任务不存在"}
	}},
	"recent_events": {Desc: "最近的操作与巡检记录（含全局事件）。", Params: obj(map[string]any{"limit": map[string]any{"type": "integer", "description": "默认 20，上限 50"}}), Run: func(st *State, p *Project, a map[string]any) any {
		limit, out := argInt(a, "limit", 20, 50), []Event{}
		for i := len(st.Events) - 1; i >= 0 && len(out) < limit; i-- {
			if e := st.Events[i]; p == nil || e.ProjectID == "" || e.ProjectID == p.ID {
				out = append(out, e)
			}
		}
		return out
	}},
}

func toolDefs(names []string) []ToolDef {
	defs := []ToolDef{}
	for _, n := range names {
		if t, ok := tools[n]; ok {
			defs = append(defs, ToolDef{Name: n, Description: t.Desc, Params: t.Params})
		}
	}
	return defs
}

// runTool re-reads committed state for every call, including calls in the same model reply.
func (s *Service) runTool(ctx context.Context, pid string, a Agent, c ToolCall) (string, bool) {
	fail := func(msg string) (string, bool) {
		b, _ := json.Marshal(map[string]string{"error": msg})
		return string(b), true
	}
	if err := ctx.Err(); err != nil {
		return fail(err.Error())
	}
	allowed := false
	for _, n := range a.Tools {
		allowed = allowed || n == c.Name
	}
	spec, ok := tools[c.Name]
	if !ok || !allowed {
		return fail("该 Agent 不能使用工具 " + c.Name)
	}
	var args map[string]any
	if e := json.Unmarshal(c.Args, &args); e != nil || args == nil {
		return fail("参数不是 JSON 对象")
	}
	schema := spec.Params
	if schema == nil {
		schema = obj(map[string]any{})
	}
	if e := validateToolArgs(args, schema, c.Name); e != nil {
		return fail(e.Error())
	}
	if c.Name == "list_mcp_servers" || c.Name == "list_mcp_tools" || c.Name == "call_mcp_tool" {
		return s.runMCPTool(ctx, a, c, args)
	}
	// A project conversation is confined to its project, so instructions hidden in
	// one project's records cannot reach another project's data or jobs.
	if explicit := argStr(args, "projectId"); explicit != "" {
		if pid != "" && explicit != pid {
			return fail("当前对话属于项目 " + pid + "，工具只能操作该项目；跨项目操作请在工作空间对话中进行")
		}
		pid = explicit
	}
	st := s.Store.Snapshot()
	var p *Project
	if pid != "" {
		var e error
		p, e = projectByID(&st, pid)
		if e != nil {
			return fail(e.Error())
		}
	}
	if p == nil && !spec.Workspace {
		return fail("请提供 projectId，或先创建项目")
	}
	if approvalReasons[c.Name] != "" && c.Name != "call_mcp_tool" {
		detail := ""
		if c.Name == "launch_jobs" {
			var e error
			if detail, e = launchPreview(s.Store.Snapshot(), pid); e != nil {
				return fail(e.Error())
			}
		}
		if e := s.awaitApproval(ctx, pid, a, c.Name, c.Args, detail); e != nil {
			return fail(e.Error())
		}
		// The workspace may have changed while the user decided.
		st = s.Store.Snapshot()
		if pid != "" {
			var e error
			if p, e = projectByID(&st, pid); e != nil {
				return fail(e.Error())
			}
		}
	}
	if c.Name == "delegate_agent" {
		if a.ID != "assistant" {
			return fail("只有主 Agent 可以调度子 Agent")
		}
		out, err := s.delegateAgent(ctx, pid, argStr(args, "agentId"), argStr(args, "task"))
		if err != nil {
			if out != "" {
				return out, true
			}
			return fail(err.Error())
		}
		return out, false
	}
	var result any
	var e error
	if spec.Action != "" {
		result, e = s.dispatchTool(ctx, spec, &st, p, a, args)
	} else {
		result = spec.Run(&st, p, args)
	}
	if e != nil {
		return fail(e.Error())
	}
	b, e := json.Marshal(result)
	if e != nil {
		return fail(e.Error())
	}
	failed := false
	if m, ok := result.(map[string]string); ok {
		failed = m["error"] != ""
	}
	limit := 16000
	if c.Name == "read_skill" {
		limit = 80000
	} // Keep the complete bounded SKILL.md, even after JSON escaping.
	if len(b) > limit {
		b, _ = json.Marshal(map[string]any{"truncated": true, "partial": clip(string(b), 16000), "hint": "缩小 limit 或按 id 读取详情"})
	}
	return string(b), failed
}

// ---- Agent loop: model → tool calls → results → model, bounded by MaxSteps, every call traced.

func (s *Service) runAgent(ctx context.Context, a Agent, m ModelProfile, key, system string, turns []Turn, pid string, initialCalls ...ToolCall) (string, []Trace, error) {
	mcpRun := &mcpRun{connections: map[string]*mcpConnection{}}
	defer mcpRun.close()
	ctx = context.WithValue(ctx, mcpRunKey{}, mcpRun)
	defs, trace := toolDefs(a.Tools), []Trace{}
	progressID := pid
	if execution, ok := ctx.Value(chatExecutionKey{}).(*chatExecution); ok {
		progressID = execution.ProjectID
	}
	if len(initialCalls) > 0 {
		turns = append(turns, Turn{Role: "assistant", Calls: initialCalls})
		results, calls, err := s.executeAgentCalls(ctx, a, initialCalls, pid, progressID, 0, trace)
		trace = append(trace, calls...)
		if err != nil {
			return "", trace, err
		}
		turns = append(turns, results...)
	}
	for step := 0; step < max(1, a.MaxSteps); step++ {
		if err := ctx.Err(); err != nil {
			return "", trace, err
		}
		s.agentProgress(progressID, "", step+1)
		reply, e := complete(ctx, m, key, system, turns, defs, false)
		if e != nil {
			return "", trace, e
		}
		if len(reply.Calls) == 0 {
			if strings.TrimSpace(reply.Text) == "" {
				return "", trace, errors.New("模型未返回内容")
			}
			return strings.TrimSpace(reply.Text), trace, nil
		}
		turns = append(turns, Turn{Role: "assistant", Text: reply.Text, Calls: reply.Calls})
		results, calls, err := s.executeAgentCalls(ctx, a, reply.Calls, pid, progressID, step+1, trace)
		trace = append(trace, calls...)
		if err != nil {
			return "", trace, err
		}
		turns = append(turns, results...)
	}
	turns = append(turns, Turn{Role: "user", Text: "工具调用预算已用完。请根据实际工具结果总结已完成、进行中与未完成的步骤，并说明还缺哪些信息；不得声称未执行的操作已完成。"})
	reply, e := complete(ctx, m, key, system, turns, defs, true)
	if e != nil {
		return "", trace, e
	}
	if strings.TrimSpace(reply.Text) == "" {
		return "", trace, errors.New("模型未返回总结；请查看工作进展中的已完成操作")
	}
	return strings.TrimSpace(reply.Text), trace, nil
}

func (s *Service) executeAgentCalls(ctx context.Context, a Agent, calls []ToolCall, pid, progressID string, step int, previous []Trace) ([]Turn, []Trace, error) {
	results, trace := []Turn{}, []Trace{}
	for _, c := range calls {
		if err := ctx.Err(); err != nil {
			return results, trace, err
		}
		start := time.Now()
		s.agentToolProgress(progressID, c, step)
		out, failed := s.runTool(ctx, pid, a, c)
		trace = append(trace, Trace{ID: c.ID, Step: step, Tool: c.Name, Args: clip(string(c.Args), 8000), Result: clip(out, 16000), Truncated: len(c.Args) > 8000 || len(out) > 16000, Error: failed, Ms: time.Since(start).Milliseconds()})
		if err := s.Store.Update(func(st *State) error {
			if mid, ok := ctx.Value(agentTraceKey{}).(string); ok {
				messages, err := scopeMessages(st, progressID)
				if err != nil {
					return err
				}
				for i := range *messages {
					if (*messages)[i].ID == mid {
						(*messages)[i].Trace = append(append([]Trace(nil), previous...), trace...)
						break
					}
				}
			}
			// The trace keeps every call. The audit trail records failures and calls
			// that change something; successful reads would only crowd it out.
			if failed {
				event(st, pid, "", "error", a.Name+" · "+c.Name+" 未完成", clip(out, 400))
			} else if tools[c.Name].Action != "" || c.Name == "call_mcp_tool" {
				event(st, pid, "", "agent", a.Name+" · "+c.Name+" 已完成", clip(out, 400))
			}
			return nil
		}); err != nil {
			return results, trace, err
		}
		results = append(results, Turn{Role: "tool", CallID: c.ID, Name: c.Name, Text: out})
		s.agentProgress(progressID, "", step)
	}
	return results, trace, nil
}

func systemPrompt(a Agent, p *Project, lang string) string {
	pt := promptText(lang)
	scope := pt.workspace
	if p != nil {
		scope = fmt.Sprintf(pt.project, p.Name, p.ID)
		if projectMode(p) == ModeStatic {
			scope += pt.static
		} else {
			scope += pt.collect
		}
	}
	orchestration := ""
	if a.ID == "assistant" {
		orchestration = pt.orchestrate + pt.mcp
	}
	return a.System + orchestration + "\n\n" + pt.guardrails + "\n\n" + scope + pt.now + now()
}

// AgentReview is the batch, structured-output path of the reviewer agent (no tools; evidence is verified locally).
func (s *Service) AgentReview(ctx context.Context, pid, cid string) error {
	state := s.Store.Snapshot()
	p, e := projectByID(&state, pid)
	if e != nil {
		return e
	}
	a, _ := agentByID(&state, "reviewer")
	m, key, e := s.Store.modelWithKey(*a)
	if e != nil {
		return e
	}
	system := a.System + promptText(state.Settings.Language).review
	count := 0
	for _, c := range p.Candidates {
		if cid != "" && c.ID != cid {
			continue
		}
		if c.Decision != "pending" || cid == "" && c.Suggestion != "" {
			continue
		}
		if count >= 20 {
			break
		}
		prompt, _ := json.Marshal(map[string]any{"project": p.Name, "criteria": p.Criteria, "organism": p.Organism, "assay": p.Assay, "candidate": c})
		reply, e := complete(ctx, m, key, system, []Turn{{Role: "user", Text: string(prompt)}}, nil, false)
		if e != nil {
			return e
		}
		text := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(reply.Text), "```json"), "```"), "```"))
		var result struct {
			Suggestion string `json:"suggestion"`
			Confidence string `json:"confidence"`
			Reason     string `json:"reason"`
			Evidence   string `json:"evidence"`
		}
		if e = json.Unmarshal([]byte(text), &result); e != nil {
			return errors.New("模型返回格式无效，未保存该条建议")
		}
		if result.Suggestion != "include" && result.Suggestion != "exclude" && result.Suggestion != "uncertain" {
			return errors.New("模型建议无效")
		}
		if result.Evidence == "" || !strings.Contains(c.Title+"\n"+c.Abstract, result.Evidence) {
			result.Suggestion = "uncertain"
			result.Confidence = "low"
			result.Evidence = ""
			result.Reason = "模型未提供可逐字核对的证据，请人工检查。"
		}
		e = s.Store.Update(func(st *State) error {
			cur, e := projectByID(st, pid)
			if e != nil {
				return e
			}
			for i := range cur.Candidates {
				item := &cur.Candidates[i]
				if item.ID == c.ID && item.Decision == "pending" {
					item.Suggestion = result.Suggestion
					item.Confidence = result.Confidence
					item.Reason = result.Reason
					item.Evidence = result.Evidence
					event(st, pid, "", "review", "复核员提供建议", item.Title+" · "+result.Reason)
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
		count++
	}
	if count == 0 {
		return errors.New("没有需要 Agent 复核的记录")
	}
	return nil
}
