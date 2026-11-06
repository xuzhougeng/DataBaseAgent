package workbench

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// command is one named application action. Exactly one of run or apply is set:
//   - apply runs inside a single Store.Update transaction and must not perform
//     slow I/O (network, SSH, system keychain), because it holds the state lock.
//   - run manages its own locking and I/O, for actions that talk to the outside world.
//
// collect marks actions that only exist in collection-mode projects.
type command struct {
	run     func(s *Service, ctx context.Context, r Request) (any, error)
	apply   func(s *Service, ctx context.Context, st *State, r Request) (any, error)
	collect bool
}

// commands is filled in init: several commands reach Dispatch again through
// agent tools, which would otherwise be a package initialization cycle.
var commands map[string]command

func init() {
	conversation := func(action string) command {
		return command{run: func(s *Service, _ context.Context, r Request) (any, error) {
			return nil, s.manageConversation(action, r)
		}}
	}
	skill := func(action string) command {
		return command{apply: func(_ *Service, _ context.Context, st *State, r Request) (any, error) {
			return updateSkill(st, action, r)
		}}
	}
	mcpConfig := func(action string) command {
		return command{run: func(s *Service, _ context.Context, r Request) (any, error) { return s.manageMCPServer(action, r) }}
	}
	commands = map[string]command{
		// Outside-world actions: each manages its own state updates.
		"saveMCPServer":   mcpConfig("saveMCPServer"),
		"deleteMCPServer": mcpConfig("deleteMCPServer"),
		"testMCPServer": {run: func(s *Service, ctx context.Context, r Request) (any, error) {
			return s.testMCPServer(ctx, r.MCPServerID)
		}},
		"search": {collect: true, run: func(s *Service, ctx context.Context, r Request) (any, error) { return s.Search(ctx, r) }},
		"probeServer": {run: func(s *Service, ctx context.Context, r Request) (any, error) {
			return nil, s.Probe(ctx, r.ServerID)
		}},
		"reviewAgent": {collect: true, run: func(s *Service, ctx context.Context, r Request) (any, error) {
			return nil, s.AgentReview(ctx, r.ProjectID, r.CandidateID)
		}},
		"message": {run: func(s *Service, ctx context.Context, r Request) (any, error) {
			return nil, s.Chat(ctx, r.ProjectID, r.AgentID, r.Message)
		}},
		"enqueueMessage": {run: func(s *Service, _ context.Context, r Request) (any, error) {
			return s.enqueueMessage(r, nil, false)
		}},
		"editQueuedMessage":  conversation("editQueuedMessage"),
		"cancelMessage":      conversation("cancelMessage"),
		"resumeChatQueue":    conversation("resumeChatQueue"),
		"pauseChatQueue":     conversation("pauseChatQueue"),
		"updateConversation": conversation("updateConversation"),
		"decideApproval": {run: func(s *Service, _ context.Context, r Request) (any, error) {
			return nil, s.decideApproval(r.ApprovalID, r.Approve)
		}},
		"tick":           {run: func(s *Service, ctx context.Context, _ Request) (any, error) { return nil, s.Tick(ctx) }},
		"exportManifest": {run: func(s *Service, _ context.Context, r Request) (any, error) { return s.ExportManifest(r.ProjectID) }},
		"openURL":        {run: func(_ *Service, _ context.Context, r Request) (any, error) { return nil, openURL(r.URL) }},
		"saveModel":      {run: (*Service).saveModel},
		"deleteModel":    {run: (*Service).deleteModel},

		// State-only actions: one transaction each.
		"importSkill":      skill("importSkill"),
		"toggleSkill":      skill("toggleSkill"),
		"deleteSkill":      skill("deleteSkill"),
		"buildStaticSite":  {apply: buildStaticSite},
		"createProject":    {apply: createProject},
		"saveProject":      {apply: saveProject},
		"importCandidates": {collect: true, apply: importCandidates},
		"review":           {collect: true, apply: reviewCandidate},
		"savePipeline":     {collect: true, apply: savePipeline(false)},
		"lockPipeline":     {collect: true, apply: savePipeline(true)},
		"addServer":        {apply: addServer},

		// Built-in examples (examples.go): at most one project per example.
		"createExampleProject": {apply: createExampleProject},
		"launch": {collect: true, apply: func(_ *Service, _ context.Context, st *State, r Request) (any, error) {
			return nil, queueJobs(st, r.ProjectID)
		}},
		"resolveJob": {apply: resolveJob},
		"settings":   {apply: saveSettings},
		"saveAgent":  {apply: saveAgent},
		"resetAgent": {apply: resetAgent},
		// The UI language: agent prompts, and preset instructions not yet customized, follow it.
		"setLanguage": {apply: func(_ *Service, _ context.Context, st *State, r Request) (any, error) {
			st.Settings.Language = uiLang(r.Lang)
			st.Settings.Agents = withPresets(st.Settings.Agents, st.Settings.Language)
			return nil, nil
		}},
	}
}

func agentActor(ctx context.Context) string {
	actor, _ := ctx.Value(agentActorKey{}).(string)
	return actor
}

func buildStaticSite(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	p, e := projectByID(st, r.ProjectID)
	if e != nil {
		return nil, e
	}
	if projectMode(p) != ModeStatic {
		return nil, errors.New("收集模式目前止于数据收集与分析，不支持建站")
	}
	site, e := newStaticSite(r.Name, r.Description, r.Format, r.Data)
	if e != nil {
		return nil, e
	}
	if e := applySiteTemplate(st, r.SkillID, site); e != nil {
		return nil, e
	}
	p.Site, p.UpdatedAt = site, now()
	event(st, p.ID, "", "user", "静态网站已生成", site.Title)
	return map[string]any{"projectId": p.ID, "rows": len(site.Rows), "builtAt": site.BuiltAt}, nil
}

func createProject(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	if r.Mode == "" {
		r.Mode = ModeCollect
	}
	if r.Mode != ModeStatic && r.Mode != ModeCollect {
		return nil, errors.New("无效的项目模式")
	}
	if strings.TrimSpace(r.Name) == "" {
		return nil, errors.New("请输入项目名称")
	}
	p := newProject(r.Mode, r.Name, r.Description)
	st.Projects = append(st.Projects, p)
	event(st, p.ID, "", "user", "项目已创建", p.Name)
	return map[string]string{"projectId": p.ID, "name": p.Name}, nil
}

func newProject(mode, name, description string) Project {
	return Project{ID: id("project"), Mode: mode, Name: strings.TrimSpace(name), Description: description, Source: "Europe PMC", Candidates: []Candidate{}, Searches: []SearchRecord{}, Versions: []Pipeline{}, Messages: []Message{}, Draft: defaultPipeline(), CreatedAt: now(), UpdatedAt: now()}
}

func saveProject(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	p, e := projectByID(st, r.ProjectID)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(r.Name) == "" {
		return nil, errors.New("项目名称不能为空")
	}
	p.Name = strings.TrimSpace(r.Name)
	p.Description, p.Organism, p.Assay, p.Criteria, p.Query, p.Source = r.Description, r.Organism, r.Assay, r.Criteria, r.Query, r.Source
	p.UpdatedAt = now()
	event(st, p.ID, "", "user", "研究范围与检索策略已保存", p.Query)
	return nil, nil
}

func importCandidates(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	p, e := projectByID(st, r.ProjectID)
	if e != nil {
		return nil, e
	}
	if len(r.Candidates) == 0 || len(r.Candidates) > 1000 {
		return nil, errors.New("每次导入 1–1000 条记录")
	}
	added := mergeCandidates(p, r.Candidates)
	event(st, p.ID, "", "user", "候选数据已导入", strconv.Itoa(added)+" 条新增记录")
	return nil, nil
}

func reviewCandidate(_ *Service, ctx context.Context, st *State, r Request) (any, error) {
	p, e := projectByID(st, r.ProjectID)
	if e != nil {
		return nil, e
	}
	if r.Decision != "accepted" && r.Decision != "excluded" && r.Decision != "pending" {
		return nil, errors.New("无效的复核结论")
	}
	if strings.TrimSpace(r.Note) == "" {
		return nil, errors.New("请填写复核理由")
	}
	for i := range p.Candidates {
		c := &p.Candidates[i]
		if c.ID != r.CandidateID {
			continue
		}
		if r.DownloadURL != "" {
			if e := validHTTPURL(r.DownloadURL); e != nil {
				return nil, e
			}
		}
		c.Decision, c.ReviewNote, c.Groups, c.DownloadURL, c.Accession = r.Decision, r.Note, r.Groups, r.DownloadURL, r.Accession
		c.ReviewedAt, c.ReviewedBy = now(), "user"
		if actor := agentActor(ctx); actor != "" {
			c.ReviewedBy = "agent:" + actor
		}
		event(st, p.ID, "", "review", "复核已记录 · "+c.ReviewedBy, c.Title+" → "+r.Decision+"；"+r.Note)
		return nil, nil
	}
	return nil, errors.New("候选记录不存在")
}

func savePipeline(lock bool) func(*Service, context.Context, *State, Request) (any, error) {
	return func(_ *Service, _ context.Context, st *State, r Request) (any, error) {
		p, e := projectByID(st, r.ProjectID)
		if e != nil {
			return nil, e
		}
		v := r.Pipeline
		v.Locked, v.ID, v.Version = false, "", 0
		if lock {
			if e := validatePipeline(st, v); e != nil {
				return nil, e
			}
			v.ID, v.Version, v.CreatedAt, v.Locked = id("pipeline"), len(p.Versions)+1, now(), true
			p.Versions = append(p.Versions, v)
			event(st, p.ID, "", "user", "流程版本已锁定", "v"+strconv.Itoa(v.Version)+" · "+v.Name)
		}
		p.Draft = v
		p.UpdatedAt = now()
		return v, nil
	}
}

func addServer(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	v := r.Server
	if e := validateServer(v); e != nil {
		return nil, e
	}
	v.ID = id("server")
	v.Status = "unchecked"
	st.Servers = append(st.Servers, v)
	event(st, "", "", "user", "服务器配置已保存", v.Name)
	return map[string]string{"serverId": v.ID, "name": v.Name}, nil
}

func resolveJob(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	j, e := jobByID(st, r.JobID)
	if e != nil {
		return nil, e
	}
	if j.Status != "needs_help" {
		return nil, errors.New("任务当前没有待处理求助")
	}
	if strings.TrimSpace(r.Note) == "" {
		return nil, errors.New("请说明已完成的修复")
	}
	if r.Groups != "" {
		if e := validateGroups(r.Groups); e != nil {
			return nil, e
		}
		j.Input.Groups = r.Groups
	}
	j.Error, j.Help, j.Failures, j.RetryAt, j.UpdatedAt = "", "", 0, "", now()
	j.Status = "queued"
	if j.ResumeStatus == "syncing" || j.ResumeStatus == "running" || j.ResumeStatus == "starting" {
		j.Status = j.ResumeStatus
	}
	j.ResumeStatus = ""
	event(st, j.ProjectID, j.ID, "user", "处理完成，任务恢复", r.Note)
	return nil, nil
}

func saveSettings(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	v := r.Settings
	if v.IntervalSeconds < 10 || v.IntervalSeconds > 86400 || v.MaxRetries < 0 || v.MaxRetries > 10 {
		return nil, errors.New("巡检间隔需为 10–86400 秒，重试次数需为 0–10")
	}
	st.Settings.Paused, st.Settings.IntervalSeconds, st.Settings.MaxRetries = v.Paused, v.IntervalSeconds, v.MaxRetries
	event(st, "", "", "user", "巡检设置已更新", fmt.Sprintf("每 %d 秒巡检，最多重试 %d 次", v.IntervalSeconds, v.MaxRetries))
	return nil, nil
}

func saveAgent(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	v := r.Agent
	a, e := agentByID(st, v.ID)
	if e != nil {
		return nil, e
	}
	if strings.TrimSpace(v.System) == "" || len(v.System) > 8000 || v.MaxSteps < 1 || v.MaxSteps > 20 {
		return nil, errors.New("请填写不超过 8000 字节的指令，步数上限需为 1–20")
	}
	if v.ModelID != "" {
		found := false
		for _, m := range st.Settings.Models {
			found = found || m.ID == v.ModelID
		}
		if !found {
			return nil, errors.New("所选模型不存在")
		}
	}
	a.System, a.ModelID, a.MaxSteps = strings.TrimSpace(v.System), v.ModelID, v.MaxSteps
	event(st, "", "", "user", "Agent 配置已保存", a.Name)
	return nil, nil
}

func resetAgent(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	a, e := agentByID(st, r.AgentID)
	if e != nil {
		return nil, e
	}
	for _, p := range presetAgents {
		if p.ID == a.ID {
			a.System, a.MaxSteps = presetSystem(p.ID, st.Settings.Language), p.MaxSteps
		}
	}
	event(st, "", "", "user", "Agent 指令已恢复默认", a.Name)
	return nil, nil
}

var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// sameEndpoint reports whether two base URLs reach the same scheme, host and port.
// A stored credential may follow path changes but never a different origin.
func sameEndpoint(a, b string) bool {
	ua, ea := url.Parse(strings.TrimSpace(a))
	ub, eb := url.Parse(strings.TrimSpace(b))
	if ea != nil || eb != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host)
}

// saveModel writes a new API key to the system keychain before taking the state
// lock: the keychain can block on an unlock prompt, and the scheduler and chat
// must keep running meanwhile. A new credential gets its own reference, so a
// failed workspace write leaves the previous credential usable.
func (s *Service) saveModel(ctx context.Context, r Request) (any, error) {
	v := r.Model
	v.APIKeyRef = ""
	v.Name, v.Model, v.APIKeyEnv = strings.TrimSpace(v.Name), strings.TrimSpace(v.Model), strings.TrimSpace(v.APIKeyEnv)
	if v.Name == "" || v.Model == "" || !protocols[v.Protocol] {
		return nil, errors.New("请填写模型名称、模型 ID，并选择协议")
	}
	if e := validHTTPURL(v.BaseURL); e != nil {
		return nil, e
	}
	agent := agentActor(ctx) != ""
	newKey := ""
	if r.APIKey != nil {
		v.APIKeyEnv = ""
		newKey = strings.TrimSpace(*r.APIKey)
	} else if v.APIKeyEnv != "" && !envVarName.MatchString(v.APIKeyEnv) {
		return nil, errors.New("API Key 环境变量名无效")
	}
	if agent && r.APIKey != nil {
		return nil, errors.New("Agent 不能读写 API Key，请在偏好设置中填写")
	}
	var newRef, oldRef string
	if newKey != "" {
		ref, err := s.Store.saveModelKey(newKey)
		if err != nil {
			s.Store.deleteModelKey(ref)
			return nil, err
		}
		newRef = ref
	}
	var result any
	err := s.Store.Update(func(st *State) error {
		v.APIKeyRef = newRef
		index := -1
		for i := range st.Settings.Models {
			if st.Settings.Models[i].ID == v.ID {
				index = i
				break
			}
		}
		if index >= 0 {
			prev := st.Settings.Models[index]
			moved := prev.Protocol != v.Protocol || !sameEndpoint(prev.BaseURL, v.BaseURL)
			if r.APIKey == nil {
				if moved && prev.APIKeyRef != "" {
					return errors.New("更换服务地址或协议时请重新填写 API Key，已保存的密钥不会发往新地址")
				}
				v.APIKeyRef = prev.APIKeyRef
			} else {
				oldRef = prev.APIKeyRef
			}
			if agent && (v.APIKeyEnv != prev.APIKeyEnv || moved && prev.APIKeyEnv != "") {
				return errors.New("Agent 不能修改密钥来源，也不能把带密钥的模型改到其他服务地址")
			}
			st.Settings.Models[index] = v
		} else {
			if agent && v.APIKeyEnv != "" {
				return errors.New("Agent 不能设置 API Key 环境变量，请在偏好设置中配置")
			}
			v.ID = id("model")
			st.Settings.Models = append(st.Settings.Models, v)
		}
		result = map[string]string{"modelId": v.ID, "name": v.Name}
		event(st, "", "", "user", "模型配置已保存", v.Name+" · "+v.Protocol+" · "+v.Model)
		return nil
	})
	if err != nil {
		s.Store.deleteModelKey(newRef)
		return nil, err
	}
	s.Store.deleteModelKey(oldRef)
	return result, nil
}

func (s *Service) deleteModel(_ context.Context, r Request) (any, error) {
	var oldRef string
	err := s.Store.Update(func(st *State) error {
		kept := []ModelProfile{}
		for _, m := range st.Settings.Models {
			if m.ID != r.ModelID {
				kept = append(kept, m)
			} else {
				oldRef = m.APIKeyRef
			}
		}
		if len(kept) == len(st.Settings.Models) {
			return errors.New("模型不存在")
		}
		st.Settings.Models = kept
		for i := range st.Settings.Agents {
			if st.Settings.Agents[i].ModelID == r.ModelID {
				st.Settings.Agents[i].ModelID = ""
			}
		}
		event(st, "", "", "user", "模型配置已删除", r.ModelID)
		return nil
	})
	if err == nil {
		s.Store.deleteModelKey(oldRef)
	}
	return nil, err
}
