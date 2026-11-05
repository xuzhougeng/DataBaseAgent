package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Service struct {
	Store    *Store
	Runner   Executor
	tickMu   sync.Mutex
	searchMu sync.Mutex
	opMu     sync.Mutex
	inFlight map[string]bool
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewService(store *Store) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{Store: store, Runner: &SSHExecutor{}, ctx: ctx, cancel: cancel, inFlight: map[string]bool{}}
}
func (s *Service) Stop() { s.cancel() }

type Request struct {
	ProjectID   string      `json:"projectId"`
	CandidateID string      `json:"candidateId"`
	JobID       string      `json:"jobId"`
	ServerID    string      `json:"serverId"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Organism    string      `json:"organism"`
	Assay       string      `json:"assay"`
	Criteria    string      `json:"criteria"`
	Query       string      `json:"query"`
	Source      string      `json:"source"`
	Limit       int         `json:"limit"`
	Decision    string      `json:"decision"`
	Note        string      `json:"note"`
	Groups      string      `json:"groups"`
	DownloadURL string      `json:"downloadUrl"`
	Accession   string      `json:"accession"`
	Message     string      `json:"message"`
	Pipeline    Pipeline    `json:"pipeline"`
	Server      Server      `json:"server"`
	Candidates  []Candidate `json:"candidates"`
	Settings    Settings    `json:"settings"`
}

func decode(raw json.RawMessage) (Request, error) {
	var r Request
	err := json.Unmarshal(raw, &r)
	return r, err
}
func (s *Service) Dispatch(ctx context.Context, action string, raw json.RawMessage) (any, error) {
	r, err := decode(raw)
	if err != nil {
		return nil, err
	}
	switch action {
	case "search":
		return s.Search(ctx, r)
	case "probeServer":
		return nil, s.Probe(ctx, r.ServerID)
	case "reviewAgent":
		return nil, s.AgentReview(ctx, r.ProjectID, r.CandidateID)
	case "message":
		return nil, s.Chat(ctx, r.ProjectID, r.Message)
	case "tick":
		return nil, s.Tick(ctx)
	}
	err = s.Store.Update(func(st *State) error {
		switch action {
		case "createProject":
			if strings.TrimSpace(r.Name) == "" {
				return errors.New("请输入项目名称")
			}
			p := Project{ID: id("project"), Name: strings.TrimSpace(r.Name), Description: r.Description, Source: "Europe PMC", Candidates: []Candidate{}, Searches: []SearchRecord{}, Versions: []Pipeline{}, Messages: []Message{}, Draft: defaultPipeline(), CreatedAt: now(), UpdatedAt: now()}
			st.Projects = append(st.Projects, p)
			event(st, p.ID, "", "user", "项目已创建", p.Name)
		case "saveProject":
			p, e := projectByID(st, r.ProjectID)
			if e != nil {
				return e
			}
			if strings.TrimSpace(r.Name) == "" {
				return errors.New("项目名称不能为空")
			}
			p.Name = strings.TrimSpace(r.Name)
			p.Description = r.Description
			p.Organism = r.Organism
			p.Assay = r.Assay
			p.Criteria = r.Criteria
			p.Query = r.Query
			p.Source = r.Source
			p.UpdatedAt = now()
			event(st, p.ID, "", "user", "研究范围与检索策略已保存", p.Query)
		case "importCandidates":
			p, e := projectByID(st, r.ProjectID)
			if e != nil {
				return e
			}
			if len(r.Candidates) == 0 || len(r.Candidates) > 1000 {
				return errors.New("每次导入 1–1000 条记录")
			}
			added := mergeCandidates(p, r.Candidates)
			event(st, p.ID, "", "user", "候选数据已导入", strconv.Itoa(added)+" 条新增记录")
		case "review":
			p, e := projectByID(st, r.ProjectID)
			if e != nil {
				return e
			}
			if r.Decision != "accepted" && r.Decision != "excluded" && r.Decision != "pending" {
				return errors.New("无效的复核结论")
			}
			if strings.TrimSpace(r.Note) == "" {
				return errors.New("请填写人工复核理由")
			}
			for i := range p.Candidates {
				c := &p.Candidates[i]
				if c.ID == r.CandidateID {
					if r.DownloadURL != "" {
						if e := validHTTPURL(r.DownloadURL); e != nil {
							return e
						}
					}
					c.Decision = r.Decision
					c.ReviewNote = r.Note
					c.Groups = r.Groups
					c.DownloadURL = r.DownloadURL
					c.Accession = r.Accession
					c.ReviewedAt = now()
					event(st, p.ID, "", "review", "人工复核已记录", c.Title+" → "+r.Decision+"；"+r.Note)
					return nil
				}
			}
			return errors.New("候选记录不存在")
		case "savePipeline", "lockPipeline":
			p, e := projectByID(st, r.ProjectID)
			if e != nil {
				return e
			}
			v := r.Pipeline
			v.Locked = false
			v.ID = ""
			v.Version = 0
			if action == "lockPipeline" {
				if e := validatePipeline(st, v); e != nil {
					return e
				}
				v.ID = id("pipeline")
				v.Version = len(p.Versions) + 1
				v.CreatedAt = now()
				v.Locked = true
				p.Versions = append(p.Versions, v)
				event(st, p.ID, "", "user", "流程版本已锁定", "v"+strconv.Itoa(v.Version)+" · "+v.Name)
			}
			p.Draft = v
			p.UpdatedAt = now()
		case "addServer":
			v := r.Server
			if e := validateServer(v); e != nil {
				return e
			}
			v.ID = id("server")
			v.Status = "unchecked"
			st.Servers = append(st.Servers, v)
			event(st, "", "", "user", "服务器配置已保存", v.Name)
		case "launch":
			return queueJobs(st, r.ProjectID)
		case "resolveJob":
			j, e := jobByID(st, r.JobID)
			if e != nil {
				return e
			}
			if j.Status != "needs_help" {
				return errors.New("任务当前没有待处理求助")
			}
			if strings.TrimSpace(r.Note) == "" {
				return errors.New("请说明已完成的修复")
			}
			if r.Groups != "" {
				if e := validateGroups(r.Groups); e != nil {
					return e
				}
				j.Input.Groups = r.Groups
			}
			j.Error = ""
			j.Help = ""
			j.Failures = 0
			j.RetryAt = ""
			j.UpdatedAt = now()
			j.Status = "queued"
			if j.ResumeStatus == "syncing" || j.ResumeStatus == "running" || j.ResumeStatus == "starting" {
				j.Status = j.ResumeStatus
			}
			j.ResumeStatus = ""
			event(st, j.ProjectID, j.ID, "user", "人工处理完成，任务恢复", r.Note)
		case "settings":
			v := r.Settings
			if v.IntervalSeconds < 10 || v.IntervalSeconds > 86400 || v.MaxRetries < 0 || v.MaxRetries > 10 {
				return errors.New("巡检间隔需为 10–86400 秒，重试次数需为 0–10")
			}
			if v.ModelURL != "" {
				if e := validHTTPURL(v.ModelURL); e != nil {
					return e
				}
			}
			if v.APIKeyEnv == "" {
				v.APIKeyEnv = "DBA_MODEL_API_KEY"
			}
			if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(v.APIKeyEnv) {
				return errors.New("API Key 环境变量名无效")
			}
			st.Settings = v
			event(st, "", "", "user", "Agent 设置已更新", fmt.Sprintf("每 %d 秒巡检，最多重试 %d 次", v.IntervalSeconds, v.MaxRetries))
		default:
			return errors.New("未知操作")
		}
		return nil
	})
	return nil, err
}

func validHTTPURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || !(u.Scheme == "https" || u.Scheme == "http") {
		return errors.New("请输入不含用户名密码的 HTTP(S) 地址")
	}
	return nil
}

func validateServer(v Server) error {
	if strings.TrimSpace(v.Name) == "" || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`).MatchString(v.Host) || !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`).MatchString(v.User) {
		return errors.New("请输入服务器名、主机和 SSH 用户")
	}
	if v.Port < 1 || v.Port > 65535 || v.CPUQuota < 1 || v.MaxJobs < 1 || v.MaxJobs > 128 {
		return errors.New("端口或资源配额无效")
	}
	if !strings.HasPrefix(v.WorkDir, "/") || strings.ContainsAny(v.WorkDir, "\r\n\x00") || strings.Contains("/"+v.WorkDir+"/", "/../") || path.Clean(v.WorkDir) == "/" {
		return errors.New("远程工作目录需为专用绝对路径，例如 /home/research/dba")
	}
	if v.KeyPath != "" {
		info, e := os.Stat(v.KeyPath)
		if e != nil || info.IsDir() {
			return errors.New("本机 SSH 密钥文件不存在")
		}
	}
	return nil
}
func validatePipeline(st *State, v Pipeline) error {
	if _, e := serverByID(st, v.ServerID); e != nil {
		return e
	}
	server, _ := serverByID(st, v.ServerID)
	if v.CPUs < 1 || v.CPUs > server.CPUQuota || v.Concurrency < 1 || v.Concurrency > 128 {
		return errors.New("任务资源超出服务器 CPU 配额，或并发无效")
	}
	if len(v.Steps) == 0 || len(v.Outputs) == 0 {
		return errors.New("请至少配置一个步骤和一个输出文件")
	}
	for _, step := range v.Steps {
		if strings.TrimSpace(step.Name) == "" || strings.TrimSpace(step.Tool) == "" || strings.TrimSpace(step.Command) == "" {
			return errors.New("每个步骤都需要名称、工具版本和实际执行命令")
		}
	}
	seen := map[string]bool{}
	for _, name := range v.Outputs {
		if !safeRelative(name) || name == "run_manifest.json" || seen[name] {
			return errors.New("产物名需为唯一相对路径；run_manifest.json 由应用生成")
		}
		seen[name] = true
	}
	return nil
}
func validateGroups(groups string) error {
	seen := map[string]bool{}
	labels := map[string]bool{}
	for _, row := range strings.Split(strings.TrimSpace(groups), "\n") {
		cols := strings.Split(row, ",")
		if len(cols) != 2 || strings.TrimSpace(cols[0]) == "" || strings.TrimSpace(cols[1]) == "" || seen[strings.TrimSpace(cols[0])] {
			return errors.New("样本分组需每行“样本编号, 分组”，样本不能重复")
		}
		seen[strings.TrimSpace(cols[0])] = true
		labels[strings.TrimSpace(cols[1])] = true
	}
	if len(seen) < 2 || len(labels) < 2 {
		return errors.New("请至少提供两个样本、两个分组")
	}
	return nil
}

func queueJobs(st *State, pid string) error {
	p, e := projectByID(st, pid)
	if e != nil {
		return e
	}
	v := p.Draft
	if !v.Locked {
		return errors.New("请先锁定处理流程")
	}
	if e := validatePipeline(st, v); e != nil {
		return e
	}
	server, _ := serverByID(st, v.ServerID)
	count := 0
	for _, c := range p.Candidates {
		if c.Decision != "accepted" {
			continue
		}
		exists := false
		for _, j := range st.Jobs {
			if j.ProjectID == pid && j.CandidateID == c.ID && j.Pipeline.ID == v.ID {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		accession := c.Accession
		if accession == "" {
			accession = c.ID
		}
		j := Job{ID: id("job"), ProjectID: pid, CandidateID: c.ID, Accession: accession, Input: c, Pipeline: v, Server: *server, Status: "queued", CreatedAt: now(), UpdatedAt: now()}
		st.Jobs = append(st.Jobs, j)
		count++
	}
	if count == 0 {
		return errors.New("没有新数据可提交；请确认纳入记录，或锁定新的流程版本")
	}
	event(st, pid, "", "queue", "任务已加入队列", fmt.Sprintf("%d 个任务 · 流程 v%d", count, v.Version))
	return nil
}

func (s *Service) PreviewArtifact(artifactID string) (string, error) {
	for _, a := range s.Store.Snapshot().Artifacts {
		if a.ID == artifactID {
			base := filepath.Join(s.Store.root, "projects")
			rel, e := filepath.Rel(base, a.Path)
			if e != nil || strings.HasPrefix(rel, "..") {
				return "", errors.New("无效的产物路径")
			}
			f, e := os.Open(a.Path)
			if e != nil {
				return "", e
			}
			defer f.Close()
			b := make([]byte, 64*1024)
			n, _ := f.Read(b)
			if strings.IndexByte(string(b[:n]), 0) >= 0 {
				return "", errors.New("二进制文件请在本地结果目录中查看")
			}
			return strings.ToValidUTF8(string(b[:n]), "�"), nil
		}
	}
	return "", errors.New("产物不存在")
}
