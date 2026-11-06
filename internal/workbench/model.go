package workbench

import "time"

const SchemaVersion = 3

type State struct {
	Skills          []Skill         `json:"skills"`
	Schema          int             `json:"schema"`
	Projects        []Project       `json:"projects"`
	Servers         []Server        `json:"servers"`
	Jobs            []Job           `json:"jobs"`
	Artifacts       []Artifact      `json:"artifacts"`
	Events          []Event         `json:"events"`
	Messages        []Message       `json:"messages,omitempty"` // workspace conversation, before a project is selected
	Conversations   []Conversation  `json:"conversations"`
	ChatQueuePaused map[string]bool `json:"chatQueuePaused"`
	Settings        Settings        `json:"settings"`
	LastTick        string          `json:"lastTick"`
}

type Project struct {
	Mode        string         `json:"mode"`
	Site        *StaticSite    `json:"site,omitempty"`
	Example     string         `json:"example,omitempty"`     // built-in example this project was created from
	ExampleLang string         `json:"exampleLang,omitempty"` // its data language: "zh" (also when empty) or "en"
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Organism    string         `json:"organism"`
	Assay       string         `json:"assay"`
	Criteria    string         `json:"criteria"`
	Query       string         `json:"query"`
	Source      string         `json:"source"`
	Candidates  []Candidate    `json:"candidates"`
	Searches    []SearchRecord `json:"searches"`
	Draft       Pipeline       `json:"draft"`
	Versions    []Pipeline     `json:"versions"`
	Messages    []Message      `json:"messages"`
	CreatedAt   string         `json:"createdAt"`
	UpdatedAt   string         `json:"updatedAt"`
}

type Candidate struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Source      string `json:"source"`
	Accession   string `json:"accession"`
	DOI         string `json:"doi"`
	URL         string `json:"url"`
	DownloadURL string `json:"downloadUrl"`
	Abstract    string `json:"abstract"`
	Organism    string `json:"organism"`
	Year        string `json:"year"`
	Suggestion  string `json:"suggestion"`
	Confidence  string `json:"confidence"`
	Reason      string `json:"reason"`
	Evidence    string `json:"evidence"`
	Decision    string `json:"decision"`
	ReviewNote  string `json:"reviewNote"`
	ReviewedAt  string `json:"reviewedAt"`
	ReviewedBy  string `json:"reviewedBy,omitempty"`
	Groups      string `json:"groups"`
}

type SearchRecord struct {
	ID      string `json:"id"`
	Query   string `json:"query"`
	Source  string `json:"source"`
	Total   int    `json:"total"`
	Fetched int    `json:"fetched"`
	Added   int    `json:"added"`
	At      string `json:"at"`
}

type Pipeline struct {
	ID          string   `json:"id"`
	Version     int      `json:"version"`
	Name        string   `json:"name"`
	ServerID    string   `json:"serverId"`
	CPUs        int      `json:"cpus"`
	Concurrency int      `json:"concurrency"`
	Steps       []Step   `json:"steps"`
	Outputs     []string `json:"outputs"`
	Locked      bool     `json:"locked"`
	CreatedAt   string   `json:"createdAt"`
}

type Step struct {
	Name    string `json:"name"`
	Tool    string `json:"tool"`
	Command string `json:"command"`
}

type Server struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	KeyPath   string `json:"keyPath"`
	WorkDir   string `json:"workDir"`
	CPUQuota  int    `json:"cpuQuota"`
	MaxJobs   int    `json:"maxJobs"`
	Status    string `json:"status"`
	Details   string `json:"details"`
	CheckedAt string `json:"checkedAt"`
}

type Job struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"projectId"`
	CandidateID  string    `json:"candidateId"`
	Accession    string    `json:"accession"`
	Input        Candidate `json:"input"`
	Pipeline     Pipeline  `json:"pipeline"`
	Server       Server    `json:"server"`
	Status       string    `json:"status"`
	Attempt      int       `json:"attempt"`
	Failures     int       `json:"failures"`
	ResumeStatus string    `json:"resumeStatus"`
	RetryAt      string    `json:"retryAt"`
	RemoteDir    string    `json:"remoteDir"`
	Log          string    `json:"log"`
	Error        string    `json:"error"`
	Help         string    `json:"help"`
	CreatedAt    string    `json:"createdAt"`
	UpdatedAt    string    `json:"updatedAt"`
}

type Artifact struct {
	ID        string `json:"id"`
	JobID     string `json:"jobId"`
	ProjectID string `json:"projectId"`
	Accession string `json:"accession"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	At        string `json:"at"`
}

type Event struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	JobID     string `json:"jobId"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	At        string `json:"at"`
}

type Message struct {
	Kind           string  `json:"kind,omitempty"`
	Task           string  `json:"task,omitempty"`
	Error          bool    `json:"error,omitempty"`
	RequestedAgent string  `json:"requestedAgent,omitempty"`
	ID             string  `json:"id"`
	ConversationID string  `json:"conversationId"`
	ClientID       string  `json:"clientId,omitempty"`
	ReplyTo        string  `json:"replyTo,omitempty"`
	RetryOf        string  `json:"retryOf,omitempty"`
	Status         string  `json:"status"`
	StartedAt      string  `json:"startedAt,omitempty"`
	FinishedAt     string  `json:"finishedAt,omitempty"`
	Role           string  `json:"role"`
	Agent          string  `json:"agent"`
	Content        string  `json:"content"`
	Trace          []Trace `json:"trace,omitempty"`
	At             string  `json:"at"`
}

type Conversation struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	AgentID   string `json:"agentId"`
	Title     string `json:"title"`
	Pinned    bool   `json:"pinned"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// AgentRun is transient progress; persisted messages and events survive a restart.
type AgentRun struct {
	SubagentID     string `json:"subagentId,omitempty"`
	Task           string `json:"task,omitempty"`
	DelegationID   string `json:"delegationId,omitempty"`
	ConversationID string `json:"conversationId"`
	MessageID      string `json:"messageId"`
	ProjectID      string `json:"projectId"`
	AgentID        string `json:"agentId"`
	StartedAt      string `json:"startedAt"`
	Step           int    `json:"step"`
	Tool           string `json:"tool"`
	ToolArgs       string `json:"toolArgs,omitempty"`
	ToolStartedAt  string `json:"toolStartedAt,omitempty"`
	ApprovalID     string `json:"approvalId,omitempty"` // set while the current tool waits for the user's confirmation
}

// Trace records one tool invocation inside an agent loop step.
type Trace struct {
	ID        string `json:"id,omitempty"`
	Step      int    `json:"step,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Tool      string `json:"tool"`
	Args      string `json:"args"`
	Result    string `json:"result"`
	Error     bool   `json:"error,omitempty"`
	Ms        int64  `json:"ms"`
}

// ModelProfile is one LLM endpoint. Protocol: openai (Chat Completions 兼容) | anthropic (Messages) | gemini (generateContent).
type ModelProfile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"apiKeyEnv"`
	APIKeyRef string `json:"apiKeyRef,omitempty"`
}

// Agent is a preset persona: instructions + least-privilege tool set + chosen model.
type Agent struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Icon        string   `json:"icon"`
	Description string   `json:"description"`
	System      string   `json:"system"`
	Tools       []string `json:"tools"`
	ModelID     string   `json:"modelId"`
	MaxSteps    int      `json:"maxSteps"`
}

type Settings struct {
	Paused          bool           `json:"paused"`
	IntervalSeconds int            `json:"intervalSeconds"`
	MaxRetries      int            `json:"maxRetries"`
	Models          []ModelProfile `json:"models"`
	Agents          []Agent        `json:"agents"`
	MCPServers      []MCPServer    `json:"mcpServers"`
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func defaultPipeline() Pipeline {
	return Pipeline{Name: "自定义数据处理流程", CPUs: 8, Concurrency: 2, Steps: []Step{
		{Name: "数据获取", Tool: "curl", Command: "mkdir -p \"$DBA_INPUT\"\n: \"${DBA_SOURCE_URL:?请为数据集填写直接下载地址}\"\ncurl --fail --location --retry 3 --continue-at - --output \"$DBA_INPUT/source.dat\" \"$DBA_SOURCE_URL\""},
		{Name: "数据处理", Tool: "用户定义", Command: ""},
	}, Outputs: []string{"expression_matrix.tsv", "qc_report.html"}}
}
