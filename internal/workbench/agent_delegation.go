package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type chatExecutionKey struct{}
type agentTraceKey struct{}

// One execution owns the conversation lock, including all synchronous child tasks.
type chatExecution struct {
	ProjectID      string
	ConversationID string
	MessageID      string
	Delegated      int
}

var agentMention = regexp.MustCompile(`(?:^|[\s,，。;；:：、(（])@([\p{L}\p{N}_-]+)`)
var quotedCode = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")

// Legacy agentId requests are treated as a mention, never as a separate conversation.
func requestedAgents(st *State, aid, message string) ([]string, error) {
	aliases := map[string]string{"主助手": "assistant", "主Agent": "assistant"}
	for _, a := range st.Settings.Agents {
		aliases[a.ID], aliases[a.Name] = a.ID, a.ID
	}
	targets, seen := []string{}, map[string]bool{}
	add := func(target string) {
		if target != "assistant" && !seen[target] {
			targets = append(targets, target)
			seen[target] = true
		}
	}
	if aid != "" {
		a, err := agentByID(st, aid)
		if err != nil {
			return nil, err
		}
		add(a.ID)
	}
	for _, match := range agentMention.FindAllStringSubmatch(quotedCode.ReplaceAllString(message, " "), -1) {
		target, ok := aliases[match[1]]
		if !ok {
			return nil, fmt.Errorf("未找到 @%s；可用 @scout、@reviewer、@pipeline、@operator", match[1])
		}
		add(target)
	}
	return targets, nil
}

func appendConversationMessage(st *State, pid string, msg Message) error {
	messages := &st.Messages
	if pid != "" {
		p, err := projectByID(st, pid)
		if err != nil {
			return err
		}
		messages = &p.Messages
	}
	*messages = append(*messages, msg)
	return nil
}

func conversationTurns(messages []Message, lang string) []Turn {
	if len(messages) > 16 {
		messages = messages[len(messages)-16:]
	}
	turns := []Turn{}
	for _, msg := range messages {
		text := msg.Content
		if msg.Kind == "delegation" {
			text = fmt.Sprintf(promptText(lang).delegation, msg.Agent, msg.Task, msg.Error, text)
		} else if msg.Role == "assistant" && msg.Agent != "" && msg.Agent != "assistant" {
			text = "[" + msg.Agent + "] " + text
		}
		turns = append(turns, Turn{Role: msg.Role, Text: text})
	}
	return turns
}

func (s *Service) delegateAgent(ctx context.Context, pid, aid, task string) (string, error) {
	execution, ok := ctx.Value(chatExecutionKey{}).(*chatExecution)
	if !ok {
		return "", errors.New("子 Agent 只能在主对话执行期间调度")
	}
	if aid == "assistant" {
		return "", errors.New("不能将主 Agent 作为子 Agent 调度")
	}
	if strings.TrimSpace(task) == "" || len(task) > 8000 {
		return "", errors.New("子任务必须为 1–8000 字节")
	}
	if execution.Delegated >= 8 {
		return "", errors.New("本轮子 Agent 调度已达 8 次，请汇总已有结果")
	}
	state := s.Store.Snapshot()
	a, err := agentByID(&state, aid)
	if err != nil {
		return "", err
	}
	var p *Project
	if pid != "" {
		p, err = projectByID(&state, pid)
		if err != nil {
			return "", err
		}
	}
	execution.Delegated++
	childID := id("msg")
	s.agentMu.Lock()
	parent := s.agentRuns[execution.ProjectID]
	child := parent
	child.SubagentID, child.Task, child.Tool, child.Step = aid, task, "", 0
	child.DelegationID, child.ToolArgs, child.ToolStartedAt = childID, "", ""
	s.agentRuns[execution.ProjectID] = child
	s.agentMu.Unlock()
	defer func() { s.agentMu.Lock(); s.agentRuns[execution.ProjectID] = parent; s.agentMu.Unlock() }()
	if err := s.Store.Update(func(st *State) error {
		if err := appendConversationMessage(st, execution.ProjectID, Message{ID: childID, ConversationID: execution.ConversationID, ReplyTo: execution.MessageID, Role: "assistant", Agent: aid, Kind: "delegation", Task: task, Status: "running", At: now()}); err != nil {
			return err
		}
		event(st, execution.ProjectID, "", "agent", "主 Agent 委派 · "+a.Name, clip(task, 400))
		return nil
	}); err != nil {
		return "", err
	}

	// Include results of earlier children in this turn, including their committed changes.
	history := state.Messages
	if execution.ProjectID != "" {
		conversation, err := projectByID(&state, execution.ProjectID)
		if err != nil {
			return "", err
		}
		history = conversation.Messages
	}
	pt := promptText(state.Settings.Language)
	turns := conversationTurns(completedConversationHistory(history, execution.ConversationID, execution.MessageID, true), state.Settings.Language)
	turns = append(turns, Turn{Role: "user", Text: pt.delegated + task})
	m, key, runErr := s.Store.modelWithKey(*a)
	var reply string
	var trace []Trace
	if runErr == nil {
		system := systemPrompt(*a, p, state.Settings.Language) + skillPrompt(&state) + pt.subagent
		reply, trace, runErr = s.runAgent(context.WithValue(ctx, agentTraceKey{}, childID), *a, m, key, system, turns, pid)
	}
	if runErr != nil {
		reply = fmt.Sprintf(pt.interrupted, runErr.Error())
	}
	if err := s.Store.Update(func(st *State) error {
		messages, err := scopeMessages(st, execution.ProjectID)
		if err != nil {
			return err
		}
		for i := range *messages {
			child := &(*messages)[i]
			if child.ID == childID {
				child.Content, child.Trace, child.Error, child.Status, child.FinishedAt = reply, trace, runErr != nil, "completed", now()
				if runErr != nil {
					child.Status = "failed"
				}
				break
			}
		}
		kind, status := "agent", " 已完成"
		if runErr != nil {
			kind, status = "error", " 执行中断"
		}
		event(st, execution.ProjectID, "", kind, a.Name+" 子任务"+status, clip(reply, 400))
		return nil
	}); err != nil {
		return "", err
	}
	result := map[string]any{"agentId": aid, "projectId": pid, "task": clip(task, 1000), "result": clip(reply, 12000), "ok": runErr == nil}
	if runErr != nil {
		result["error"] = runErr.Error()
	}
	b, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(b), runErr
}
