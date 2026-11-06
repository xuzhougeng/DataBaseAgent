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
	"strings"
	"sync"
)

type Service struct {
	Store        *Store
	Runner       Executor
	tickMu       sync.Mutex
	searchMu     sync.Mutex
	opMu         sync.Mutex
	agentMu      sync.Mutex
	agentRuns    map[string]AgentRun
	agentCancels map[string]context.CancelFunc
	chatWorkers  map[string]bool
	chatWaiters  map[string]chan error
	chatErrors   map[string]string
	chatWG       sync.WaitGroup
	inFlight     map[string]bool
	opWG         sync.WaitGroup // operate goroutines started by Tick
	approvals    map[string]*Approval
	autoApprove  bool // tests only: the desktop app always asks the user
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewService(store *Store) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{Store: store, Runner: &SSHExecutor{}, ctx: ctx, cancel: cancel, inFlight: map[string]bool{}, agentRuns: map[string]AgentRun{}, agentCancels: map[string]context.CancelFunc{}, chatWorkers: map[string]bool{}, chatWaiters: map[string]chan error{}, chatErrors: map[string]string{}, approvals: map[string]*Approval{}}
}
func (s *Service) Stop() {
	s.cancel()
	s.agentMu.Lock() // synchronize with the last possible worker registration
	s.agentMu.Unlock()
	s.chatWG.Wait()
	s.tickMu.Lock() // Tick registers operations while holding tickMu
	s.tickMu.Unlock()
	s.opWG.Wait() // in-flight operations may still persist their outcome
	s.agentMu.Lock()
	for mid := range s.chatWaiters {
		s.finishChatWaiterLocked(mid, context.Canceled)
	}
	s.agentMu.Unlock()
}

type Request struct {
	Mode           string       `json:"mode"`
	SkillID        string       `json:"skillId"`
	ExampleID      string       `json:"exampleId"`
	SkillContent   string       `json:"skillContent"`
	Enabled        *bool        `json:"enabled,omitempty"`
	Data           string       `json:"data"`
	Format         string       `json:"format"`
	ConversationID string       `json:"conversationId"`
	MessageID      string       `json:"messageId"`
	ClientID       string       `json:"clientId"`
	RetryOf        string       `json:"retryOf"`
	Title          *string      `json:"title,omitempty"`
	Pinned         *bool        `json:"pinned,omitempty"`
	Archived       *bool        `json:"archived,omitempty"`
	ProjectID      string       `json:"projectId"`
	CandidateID    string       `json:"candidateId"`
	JobID          string       `json:"jobId"`
	ServerID       string       `json:"serverId"`
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	Organism       string       `json:"organism"`
	Assay          string       `json:"assay"`
	Criteria       string       `json:"criteria"`
	Query          string       `json:"query"`
	Source         string       `json:"source"`
	Limit          int          `json:"limit"`
	Decision       string       `json:"decision"`
	Note           string       `json:"note"`
	Groups         string       `json:"groups"`
	DownloadURL    string       `json:"downloadUrl"`
	URL            string       `json:"url"`
	Accession      string       `json:"accession"`
	Message        string       `json:"message"`
	Pipeline       Pipeline     `json:"pipeline"`
	Server         Server       `json:"server"`
	Candidates     []Candidate  `json:"candidates"`
	Settings       Settings     `json:"settings"`
	AgentID        string       `json:"agentId"`
	ModelID        string       `json:"modelId"`
	Agent          Agent        `json:"agent"`
	Model          ModelProfile `json:"model"`
	APIKey         *string      `json:"apiKey,omitempty"` // omitted keeps the stored key; empty removes it
	MCPServer      MCPServer    `json:"mcpServer"`
	MCPServerID    string       `json:"mcpServerId"`
	ApprovalID     string       `json:"approvalId"`
	Approve        bool         `json:"approve"`
}

func decode(raw json.RawMessage) (Request, error) {
	var r Request
	err := json.Unmarshal(raw, &r)
	return r, err
}

// Dispatch runs one named command. UI requests and agent tools share this path,
// so every write passes the same validation.
func (s *Service) Dispatch(ctx context.Context, action string, raw json.RawMessage) (any, error) {
	cmd, ok := commands[action]
	if !ok {
		return nil, errors.New("未知操作")
	}
	r, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if cmd.collect && r.ProjectID != "" {
		if err := s.Store.Read(func(st *State) error {
			p, e := projectByID(st, r.ProjectID)
			if e != nil {
				return e
			}
			if projectMode(p) == ModeStatic {
				return errors.New("已有数据模式直接建立静态网站，无需检索、复核或远程处理")
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if cmd.run != nil {
		return cmd.run(s, ctx, r)
	}
	var result any
	err = s.Store.Update(func(st *State) error {
		var e error
		result, e = cmd.apply(s, ctx, st, r)
		return e
	})
	if err != nil {
		return nil, err
	}
	return result, nil
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

// openURL opens an http(s) link in the system browser; the desktop WebView cannot open new windows itself.
func openURL(raw string) error {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("只能打开 http / https 链接")
	}
	cmd := browserCommand(u.String())
	hideCommand(cmd)
	if e = cmd.Start(); e != nil {
		return e
	}
	go cmd.Wait()
	return nil
}
