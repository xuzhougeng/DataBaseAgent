package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const maxQueuedMessages = 20

type MessageReceipt struct {
	ConversationID string `json:"conversationId"`
	MessageID      string `json:"messageId"`
}

func scopeMessages(st *State, pid string) (*[]Message, error) {
	if pid == "" {
		return &st.Messages, nil
	}
	p, err := projectByID(st, pid)
	if err != nil {
		return nil, err
	}
	return &p.Messages, nil
}

func conversationByID(st *State, pid, cid string) (*Conversation, error) {
	for i := range st.Conversations {
		c := &st.Conversations[i]
		if c.ID == cid && c.ProjectID == pid {
			return c, nil
		}
	}
	return nil, errors.New("对话不存在或不属于当前项目")
}

func messageByID(messages []Message, mid string) (*Message, error) {
	for i := range messages {
		if messages[i].ID == mid && messages[i].Role == "user" {
			return &messages[i], nil
		}
	}
	return nil, errors.New("消息不存在")
}

func pendingMessage(m Message) bool {
	return m.Role == "user" && (m.Status == "queued" || m.Status == "running" || m.Status == "stopping")
}

func conversationTitle(message string) string {
	runes := []rune(strings.Join(strings.Fields(message), " "))
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	if len(runes) == 0 {
		return "历史对话"
	}
	return string(runes)
}

// Legacy authors and child tasks are retained in one shared main-agent conversation per scope.
// This is idempotent, including for legacy callers that import messages without IDs.
func migrateConversations(st *State) {
	migrate := func(pid string, messages *[]Message) {
		cid := ""
		for i := len(st.Conversations) - 1; i >= 0; i-- {
			if c := st.Conversations[i]; c.ProjectID == pid && !c.Archived {
				cid = c.ID
				break
			}
		}
		lastUser := ""
		for i := range *messages {
			m := &(*messages)[i]
			if m.ID != "" && m.ConversationID != "" {
				if m.Role == "user" {
					lastUser = m.ID
				}
				continue
			}
			if cid == "" {
				cid = id("chat")
				st.Conversations = append(st.Conversations, Conversation{ID: cid, ProjectID: pid, AgentID: "assistant", Title: conversationTitle(m.Content), CreatedAt: m.At, UpdatedAt: m.At})
			}
			if m.Agent == "" {
				m.Agent = "assistant"
			}
			m.ID, m.ConversationID, m.Status = id("msg"), cid, "completed"
			if m.Role == "user" {
				if m.Agent != "assistant" {
					m.RequestedAgent = m.Agent
				}
				lastUser = m.ID
				m.Status = "interrupted"
			} else if lastUser != "" {
				m.ReplyTo = lastUser
				if m.Kind != "delegation" {
					if user, err := messageByID(*messages, lastUser); err == nil {
						user.Status = "completed"
					}
					lastUser = ""
				}
			}
			if c, err := conversationByID(st, pid, cid); err == nil {
				c.UpdatedAt = m.At
			}
		}
	}
	migrate("", &st.Messages)
	for i := range st.Projects {
		migrate(st.Projects[i].ID, &st.Projects[i].Messages)
	}
}

// Never replay a tool-bearing request after a restart. Pending queues require an explicit resume.
func recoverConversations(st *State) bool {
	changed := false
	recoverScope := func(pid string, messages *[]Message) {
		var replies []Message
		for i := range *messages {
			m := &(*messages)[i]
			if m.Kind == "delegation" && m.Status == "running" {
				m.Status, m.Error, m.Content, changed = "interrupted", true, "应用关闭时子任务中断。已完成操作保留，请核对调用记录。", true
			}
			if !pendingMessage(*m) {
				continue
			}
			if !st.ChatQueuePaused[pid] {
				st.ChatQueuePaused[pid], changed = true, true
			}
			if m.Status != "queued" {
				m.Status, m.FinishedAt, changed = "interrupted", now(), true
				replies = append(replies, Message{ID: id("msg"), ConversationID: m.ConversationID, ReplyTo: m.ID, Role: "assistant", Agent: m.Agent, Status: "interrupted", Content: "应用关闭时执行中断。已完成的工具操作保留，请核对调用记录后再重试。", Trace: m.Trace, At: now()})
				m.Trace = nil
			}
		}
		*messages = append(*messages, replies...)
	}
	recoverScope("", &st.Messages)
	for i := range st.Projects {
		recoverScope(st.Projects[i].ID, &st.Projects[i].Messages)
	}
	return changed
}

// Acceptance is durable and independent of the lifetime of the browser's HTTP request.
// agentMu serializes queue changes with worker transitions; Store.Update provides atomic persistence.
func (s *Service) enqueueMessage(r Request, waiter chan error, legacy bool) (MessageReceipt, error) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	var receipt MessageReceipt
	if s.ctx.Err() != nil {
		return receipt, errors.New("应用正在关闭，请稍后重试")
	}
	if s.chatErrors[r.ProjectID] != "" {
		return receipt, errors.New("保存对话失败，请检查磁盘后重新启动应用：" + s.chatErrors[r.ProjectID])
	}
	if len(r.ClientID) > 100 {
		return receipt, errors.New("消息请求标识过长")
	}
	err := s.Store.Update(func(st *State) error {
		migrateConversations(st)
		messages, err := scopeMessages(st, r.ProjectID)
		if err != nil {
			return err
		}
		if r.AgentID == "" {
			r.AgentID = "assistant"
		}
		if r.RetryOf != "" {
			original, err := messageByID(*messages, r.RetryOf)
			if err != nil {
				return err
			}
			if original.Status != "failed" && original.Status != "cancelled" && original.Status != "interrupted" {
				return errors.New("仅可重试失败、停止或中断的消息")
			}
			r.ConversationID, r.AgentID, r.Message = original.ConversationID, original.RequestedAgent, original.Content
			if r.AgentID == "" {
				r.AgentID = "assistant"
			}
		}
		r.Message = strings.TrimSpace(r.Message)
		if r.Message == "" || len(r.Message) > 8000 {
			return errors.New("请输入不超过 8000 字节的消息")
		}
		for _, m := range *messages {
			if r.ClientID != "" && m.ClientID == r.ClientID {
				requested := m.RequestedAgent
				if requested == "" {
					requested = "assistant"
				}
				if m.Content != r.Message || requested != r.AgentID || m.RetryOf != r.RetryOf || r.ConversationID != "" && m.ConversationID != r.ConversationID {
					return errors.New("消息请求标识已被其他内容使用")
				}
				receipt = MessageReceipt{ConversationID: m.ConversationID, MessageID: m.ID}
				return nil
			}
		}
		queued := 0
		for _, m := range *messages {
			if pendingMessage(m) {
				if legacy {
					return errors.New("当前对话已有 Agent 正在执行，请等待完成")
				}
				if m.Status == "queued" {
					queued++
				}
			}
		}
		if queued >= maxQueuedMessages {
			return fmt.Errorf("当前项目最多排队 %d 条消息，请等待或移除排队消息", maxQueuedMessages)
		}
		if _, err := requestedAgents(st, r.AgentID, r.Message); err != nil {
			return err
		}
		a, err := agentByID(st, "assistant")
		if err != nil {
			return err
		}
		m, err := modelFor(st, *a)
		if err != nil || !modelReady(m) {
			return errors.New("请先为 Agent 配置可用模型")
		}
		if legacy && r.ConversationID == "" {
			for i := len(st.Conversations) - 1; i >= 0; i-- {
				c := st.Conversations[i]
				if c.ProjectID == r.ProjectID && c.AgentID == "assistant" && !c.Archived {
					r.ConversationID = c.ID
					break
				}
			}
			st.ChatQueuePaused[r.ProjectID] = false
		}
		var c *Conversation
		if r.ConversationID == "" {
			st.Conversations = append(st.Conversations, Conversation{ID: id("chat"), ProjectID: r.ProjectID, AgentID: "assistant", Title: conversationTitle(r.Message), CreatedAt: now()})
			c = &st.Conversations[len(st.Conversations)-1]
		} else {
			c, err = conversationByID(st, r.ProjectID, r.ConversationID)
			if err != nil {
				return err
			}
			if c.Archived {
				return errors.New("请恢复已归档对话，或为所选 Agent 新建对话")
			}
		}
		c.UpdatedAt = now()
		msg := Message{ID: id("msg"), ConversationID: c.ID, ClientID: r.ClientID, RetryOf: r.RetryOf, Role: "user", Agent: "assistant", RequestedAgent: r.AgentID, Content: r.Message, At: now(), Status: "queued"}
		*messages = append(*messages, msg)
		receipt = MessageReceipt{ConversationID: c.ID, MessageID: msg.ID}
		return nil
	})
	if err != nil {
		return receipt, err
	}
	if waiter != nil {
		s.chatWaiters[receipt.MessageID] = waiter
	}
	s.startChatWorkerLocked(r.ProjectID)
	return receipt, nil
}

// Chat retains the synchronous entry point for command-line callers. The UI uses enqueueMessage.
func (s *Service) Chat(ctx context.Context, pid, aid, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	receipt, err := s.enqueueMessage(Request{ProjectID: pid, AgentID: aid, Message: message}, done, true)
	if err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = s.manageConversation("cancelMessage", Request{ProjectID: pid, MessageID: receipt.MessageID})
		<-done
		return ctx.Err()
	}
}

func (s *Service) startChatWorkerLocked(pid string) {
	if s.chatWorkers[pid] || s.ctx.Err() != nil || s.chatErrors[pid] != "" {
		return
	}
	s.chatWorkers[pid] = true
	s.chatWG.Add(1)
	go s.processChatQueue(pid)
}

func (s *Service) finishChatWaiterLocked(mid string, err error) {
	if done := s.chatWaiters[mid]; done != nil {
		done <- err
		delete(s.chatWaiters, mid)
	}
}

func (s *Service) processChatQueue(pid string) {
	defer s.chatWG.Done()
	for {
		s.agentMu.Lock()
		state := s.Store.Snapshot()
		messages, _ := scopeMessages(&state, pid)
		var next Message
		if s.ctx.Err() == nil && !state.ChatQueuePaused[pid] && s.chatErrors[pid] == "" && messages != nil {
			for _, m := range *messages {
				if m.Role == "user" && m.Status == "queued" {
					next = m
					break
				}
			}
		}
		if next.ID == "" {
			delete(s.chatWorkers, pid)
			// Synchronous callers must also be released on shutdown or a storage failure.
			if messages != nil && (s.ctx.Err() != nil || s.chatErrors[pid] != "") {
				for _, m := range *messages {
					if pendingMessage(m) {
						s.finishChatWaiterLocked(m.ID, errors.New("对话队列已暂停"))
					}
				}
			}
			s.agentMu.Unlock()
			return
		}
		err := s.Store.Update(func(st *State) error {
			messages, err := scopeMessages(st, pid)
			if err != nil {
				return err
			}
			m, err := messageByID(*messages, next.ID)
			if err != nil {
				return err
			}
			m.Status, m.StartedAt = "running", now()
			event(st, pid, "", "agent", "Agent 开始执行", clip(m.Content, 160))
			return nil
		})
		if err != nil {
			s.chatErrors[pid] = err.Error()
			s.finishChatWaiterLocked(next.ID, err)
			s.agentMu.Unlock()
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.agentCancels[pid] = cancel
		s.agentRuns[pid] = AgentRun{ProjectID: pid, ConversationID: next.ConversationID, MessageID: next.ID, AgentID: next.Agent, StartedAt: now()}
		s.agentMu.Unlock()

		reply, trace, runErr := s.executeMessage(ctx, pid, next)
		s.agentMu.Lock()
		status := "completed"
		if ctx.Err() != nil {
			runErr = ctx.Err()
			status, reply = "cancelled", "已停止本次执行。已完成的工具操作保留，后续消息已暂停；可以核对结果后继续队列。"
			if s.ctx.Err() != nil {
				status, reply = "interrupted", "应用关闭时执行中断。已完成的工具操作保留，请核对调用记录后再重试。"
			}
		} else if runErr != nil {
			status, reply = "failed", "本次执行中断："+runErr.Error()+"。已完成的工具操作保留，请查看调用记录和工作进展。"
		}
		err = s.Store.Update(func(st *State) error {
			messages, err := scopeMessages(st, pid)
			if err != nil {
				return err
			}
			m, err := messageByID(*messages, next.ID)
			if err != nil {
				return err
			}
			m.Status, m.FinishedAt, m.Trace = status, now(), nil
			*messages = append(*messages, Message{ID: id("msg"), ConversationID: next.ConversationID, ReplyTo: next.ID, Role: "assistant", Agent: next.Agent, Content: reply, Status: status, Trace: trace, At: now()})
			if c, err := conversationByID(st, pid, next.ConversationID); err == nil {
				c.UpdatedAt = now()
			}
			kind, title := "agent", "Agent 已回复"
			if runErr != nil {
				st.ChatQueuePaused[pid] = true
				kind, title = "error", "Agent 执行中断"
			}
			event(st, pid, "", kind, title, fmt.Sprintf("%s · 调用工具 %d 次", clip(next.Content, 80), len(trace)))
			return nil
		})
		if err != nil {
			s.chatErrors[pid], runErr = err.Error(), err
		}
		cancel()
		delete(s.agentRuns, pid)
		delete(s.agentCancels, pid)
		s.finishChatWaiterLocked(next.ID, runErr)
		s.agentMu.Unlock()
	}
}

// completedConversationHistory groups a turn with its child cards and final answer.
// Queued prompts and results of other conversations cannot enter either agent's context.
func completedConversationHistory(messages []Message, cid, before string, includeCurrent bool) []Message {
	history := []Message{}
	for _, msg := range messages {
		if msg.ID == before {
			if includeCurrent {
				history = append(history, msg)
				for _, child := range messages {
					if child.ReplyTo == msg.ID && child.Kind == "delegation" && child.Status != "running" {
						history = append(history, child)
					}
				}
			}
			break
		}
		if msg.ConversationID == cid && msg.Role == "user" && msg.Status == "completed" {
			history = append(history, msg)
			for _, reply := range messages {
				if reply.ReplyTo == msg.ID {
					history = append(history, reply)
				}
			}
		}
	}
	return history
}

func (s *Service) executeMessage(ctx context.Context, pid string, next Message) (string, []Trace, error) {
	state := s.Store.Snapshot()
	a, err := agentByID(&state, "assistant")
	if err != nil {
		return "", nil, err
	}
	targets, err := requestedAgents(&state, next.RequestedAgent, next.Content)
	if err != nil {
		return "", nil, err
	}
	m, key, err := s.Store.modelWithKey(*a)
	if err != nil {
		return "", nil, err
	}
	var p *Project
	if pid != "" {
		p, _ = projectByID(&state, pid)
	}
	messages, err := scopeMessages(&state, pid)
	if err != nil {
		return "", nil, err
	}
	turns := conversationTurns(completedConversationHistory(*messages, next.ConversationID, next.ID, false))
	turns = append(turns, Turn{Role: "user", Text: next.Content})
	ctx = context.WithValue(ctx, chatExecutionKey{}, &chatExecution{ProjectID: pid, ConversationID: next.ConversationID, MessageID: next.ID})
	ctx = context.WithValue(ctx, agentTraceKey{}, next.ID)
	initial := []ToolCall{}
	for _, target := range targets {
		args, _ := json.Marshal(map[string]string{"agentId": target, "task": next.Content})
		initial = append(initial, ToolCall{ID: id("delegate"), Name: "delegate_agent", Args: args})
	}
	system := systemPrompt(*a, p) + skillPrompt(&state)
	if next.RetryOf != "" {
		system += "\nThis is an explicit retry. Earlier tools may have already changed state; read current state before repeating any mutation."
	}
	return s.runAgent(ctx, *a, m, key, system, turns, pid, initial...)
}

func (s *Service) manageConversation(action string, r Request) error {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	shouldCancel := false
	err := s.Store.Update(func(st *State) error {
		messages, err := scopeMessages(st, r.ProjectID)
		if err != nil {
			return err
		}
		switch action {
		case "updateConversation":
			c, err := conversationByID(st, r.ProjectID, r.ConversationID)
			if err != nil {
				return err
			}
			if r.Title != nil {
				title := strings.TrimSpace(*r.Title)
				if title == "" || len([]rune(title)) > 100 {
					return errors.New("对话标题需为 1–100 个字符")
				}
				c.Title = title
			}
			if r.Pinned != nil {
				c.Pinned = *r.Pinned
			}
			if r.Archived != nil {
				for _, m := range *messages {
					if *r.Archived && m.ConversationID == c.ID && pendingMessage(m) {
						return errors.New("请先停止执行并移除该对话的排队消息，再归档对话")
					}
				}
				c.Archived = *r.Archived
			}
		case "pauseChatQueue":
			st.ChatQueuePaused[r.ProjectID] = true
		case "resumeChatQueue":
			if run, ok := s.agentRuns[r.ProjectID]; ok {
				if m, _ := messageByID(*messages, run.MessageID); m != nil && m.Status == "stopping" {
					return errors.New("正在停止执行，请稍后继续队列")
				}
			}
			if s.chatErrors[r.ProjectID] != "" {
				return errors.New("保存对话失败，请检查磁盘后重新启动应用：" + s.chatErrors[r.ProjectID])
			}
			st.ChatQueuePaused[r.ProjectID] = false
		case "editQueuedMessage", "cancelMessage":
			m, err := messageByID(*messages, r.MessageID)
			if err != nil {
				return err
			}
			if action == "editQueuedMessage" {
				if m.Status != "queued" {
					return errors.New("消息已开始执行，不能再编辑")
				}
				if strings.TrimSpace(r.Message) == "" || len(r.Message) > 8000 {
					return errors.New("请输入不超过 8000 字节的消息")
				}
				if _, err := requestedAgents(st, m.RequestedAgent, r.Message); err != nil {
					return err
				}
				m.Content = strings.TrimSpace(r.Message)
			} else if m.Status == "queued" {
				m.Status, m.FinishedAt = "cancelled", now()
			} else if m.Status == "running" || m.Status == "stopping" {
				m.Status, st.ChatQueuePaused[r.ProjectID], shouldCancel = "stopping", true, true
			} else {
				return errors.New("消息已结束")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if shouldCancel {
		if cancel := s.agentCancels[r.ProjectID]; cancel != nil {
			cancel()
		}
	} else if action == "cancelMessage" {
		s.finishChatWaiterLocked(r.MessageID, context.Canceled)
	}
	if action == "resumeChatQueue" {
		s.startChatWorkerLocked(r.ProjectID)
	}
	return nil
}

func (s *Service) ChatErrors() map[string]string {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	out := map[string]string{}
	for pid, err := range s.chatErrors {
		out[pid] = err
	}
	return out
}
