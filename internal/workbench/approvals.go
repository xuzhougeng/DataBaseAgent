package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Approval is an agent's pending request to run a high-impact tool. Model input
// includes untrusted titles, abstracts, logs and plugin results, so actions that
// execute remote commands, change where conversations or credentials are sent,
// or write to external services wait for the user. Requests are transient: an
// unanswered request ends with its agent run (stop, failure, timeout or shutdown).
type Approval struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	ConversationID string `json:"conversationId,omitempty"`
	MessageID      string `json:"messageId,omitempty"`
	AgentID        string `json:"agentId"`
	Tool           string `json:"tool"`
	Reason         string `json:"reason"`
	Args           string `json:"args"`
	Detail         string `json:"detail,omitempty"`
	CreatedAt      string `json:"createdAt"`
	decision       chan bool
}

const approvalTimeout = 30 * time.Minute

// approvalReasons lists the tools that need the user's confirmation, with the risk shown to them.
// Read-only MCP tools (readOnlyHint) are exempt; every other MCP call is confirmed.
var approvalReasons = map[string]string{
	"launch_jobs":   "创建任务后，调度器会在远端服务器上执行已锁定流程的命令。",
	"resolve_job":   "恢复任务会继续或重新执行远端处理。",
	"add_server":    "注册新的远程执行服务器，之后的流程命令可能在该主机上运行。",
	"save_model":    "修改模型配置会改变对话内容发往的服务。",
	"delete_model":  "删除模型配置，使用它的 Agent 会改用其他模型。",
	"save_agent":    "修改 Agent 的指令、模型或步数，会持续影响之后的对话。",
	"reset_agent":   "恢复 Agent 的默认指令和步数。",
	"call_mcp_tool": "调用外部插件工具，写入、部署或删除可能立即生效。",
}

// Approvals lists pending requests, oldest first.
func (s *Service) Approvals() []Approval {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	out := []Approval{}
	for _, a := range s.approvals {
		item := *a
		item.decision = nil
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt+out[i].ID < out[j].CreatedAt+out[j].ID })
	return out
}

func prettyArgs(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return clip(string(raw), 6000)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return clip(string(raw), 6000)
	}
	return clip(string(b), 6000)
}

// awaitApproval blocks the agent until the user allows or denies the call.
func (s *Service) awaitApproval(ctx context.Context, pid string, a Agent, tool string, args json.RawMessage, detail string) error {
	if s.autoApprove {
		return nil
	}
	ap := &Approval{ID: id("approval"), ProjectID: pid, AgentID: a.ID, Tool: tool, Reason: approvalReasons[tool], Args: prettyArgs(args), Detail: clip(detail, 6000), CreatedAt: now(), decision: make(chan bool, 1)}
	progressID := pid
	if execution, ok := ctx.Value(chatExecutionKey{}).(*chatExecution); ok {
		ap.ProjectID, ap.ConversationID, ap.MessageID = execution.ProjectID, execution.ConversationID, execution.MessageID
		progressID = execution.ProjectID
	}
	s.agentMu.Lock()
	if s.ctx.Err() != nil {
		s.agentMu.Unlock()
		return s.ctx.Err()
	}
	s.approvals[ap.ID] = ap
	if run, ok := s.agentRuns[progressID]; ok {
		run.ApprovalID = ap.ID
		s.agentRuns[progressID] = run
	}
	s.agentMu.Unlock()
	defer func() {
		s.agentMu.Lock()
		delete(s.approvals, ap.ID)
		if run, ok := s.agentRuns[progressID]; ok && run.ApprovalID == ap.ID {
			run.ApprovalID = ""
			s.agentRuns[progressID] = run
		}
		s.agentMu.Unlock()
	}()
	_ = s.Store.Update(func(st *State) error {
		event(st, ap.ProjectID, "", "help", a.Name+" 请求确认 · "+tool, ap.Reason)
		return nil
	})
	timer := time.NewTimer(approvalTimeout)
	defer timer.Stop()
	select {
	case ok := <-ap.decision:
		if !ok {
			return errors.New("用户拒绝了该操作，未执行。说明你原本打算做什么，并询问用户如何调整；不要重复申请同一操作")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("等待用户确认超时，操作未执行")
	}
}

func (s *Service) decideApproval(aid string, approve bool) error {
	s.agentMu.Lock()
	ap := s.approvals[aid]
	if ap != nil {
		delete(s.approvals, aid)
		ap.decision <- approve
	}
	s.agentMu.Unlock()
	if ap == nil {
		return errors.New("该确认请求已结束")
	}
	title := "已允许 Agent 操作 · " + ap.Tool
	if !approve {
		title = "已拒绝 Agent 操作 · " + ap.Tool
	}
	return s.Store.Update(func(st *State) error {
		event(st, ap.ProjectID, "", "user", title, ap.Reason)
		return nil
	})
}

// launchPreview dry-runs job creation on a copy of the state, so the user sees
// the exact commands and job count, and an invalid launch fails without asking.
func launchPreview(st State, pid string) (string, error) {
	before := len(st.Jobs)
	if err := queueJobs(&st, pid); err != nil {
		return "", err
	}
	j := st.Jobs[len(st.Jobs)-1]
	var b strings.Builder
	fmt.Fprintf(&b, "将创建 %d 个任务 · 流程 v%d %s\n服务器 %s（%s@%s:%d，%s）\n", len(st.Jobs)-before, j.Pipeline.Version, j.Pipeline.Name, j.Server.Name, j.Server.User, j.Server.Host, j.Server.Port, j.Server.WorkDir)
	for i, step := range j.Pipeline.Steps {
		fmt.Fprintf(&b, "\n[%d] %s · %s\n%s\n", i+1, step.Name, step.Tool, step.Command)
	}
	return b.String(), nil
}
