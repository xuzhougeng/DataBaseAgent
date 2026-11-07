package workbench

import (
	"context"
	"errors"
	"sort"
)

// Deletion is reversible and only removes idle projects from the active state.
// Queue acceptance/worker transitions take agentMu before the store lock too.
func (s *Service) deleteProject(_ context.Context, r Request) (any, error) {
	if !r.Approve {
		return nil, errors.New("请确认将项目移入回收站")
	}
	if !s.projectMu.TryLock() {
		return nil, errors.New("工作空间正在检索或复核，请等待完成后再删除项目")
	}
	defer s.projectMu.Unlock()
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if len(s.agentRuns) != 0 || s.chatWorkers[r.ProjectID] {
		return nil, errors.New("AI 正在执行，请等待完成或停止后再删除项目")
	}
	err := s.Store.Update(func(st *State) error {
		p, err := projectByID(st, r.ProjectID)
		if err != nil {
			return err
		}
		for _, j := range st.Jobs {
			if j.ProjectID == p.ID && j.Status != "succeeded" {
				return errors.New("项目有未完成任务，不能删除；请先处理排队、运行、重试或需要协助的任务")
			}
		}
		for _, m := range p.Messages {
			if pendingMessage(m) {
				return errors.New("项目有未完成的 AI 消息，请先停止执行并取消排队消息")
			}
		}
		deleted := DeletedProject{Project: *p, DeletedAt: now(), ChatQueuePaused: st.ChatQueuePaused[p.ID]}
		pid, name := p.ID, p.Name
		st.Projects, _ = splitProjectItems(st.Projects, func(v Project) bool { return v.ID == pid })
		st.Jobs, deleted.Jobs = splitProjectItems(st.Jobs, func(v Job) bool { return v.ProjectID == pid })
		st.Artifacts, deleted.Artifacts = splitProjectItems(st.Artifacts, func(v Artifact) bool { return v.ProjectID == pid })
		st.Events, deleted.Events = splitProjectItems(st.Events, func(v Event) bool { return v.ProjectID == pid })
		st.Conversations, deleted.Conversations = splitProjectItems(st.Conversations, func(v Conversation) bool { return v.ProjectID == pid })
		delete(st.ChatQueuePaused, pid)
		st.DeletedProjects = append(st.DeletedProjects, deleted)
		event(st, "", "", "user", "项目已移入回收站", name)
		return nil
	})
	if err == nil {
		delete(s.chatErrors, r.ProjectID)
	}
	return nil, err
}

func restoreProject(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	for i, deleted := range st.DeletedProjects {
		p := deleted.Project
		if p.ID != r.ProjectID {
			continue
		}
		if _, err := projectByID(st, p.ID); err == nil {
			return nil, errors.New("项目已存在，无法恢复")
		}
		// A new copy of an example may have been created while this one was in
		// the trash. Restore it as an ordinary project, keeping example lookup unique.
		for _, active := range st.Projects {
			if p.Example != "" && active.Example == p.Example && uiLang(active.ExampleLang) == uiLang(p.ExampleLang) {
				p.Example, p.ExampleLang = "", ""
				break
			}
		}
		st.Projects = append(st.Projects, p)
		st.Jobs = append(st.Jobs, deleted.Jobs...)
		st.Artifacts = append(st.Artifacts, deleted.Artifacts...)
		st.Events = append(st.Events, deleted.Events...)
		sort.SliceStable(st.Events, func(i, j int) bool { return st.Events[i].At < st.Events[j].At })
		st.Conversations = append(st.Conversations, deleted.Conversations...)
		if st.ChatQueuePaused == nil {
			st.ChatQueuePaused = map[string]bool{}
		}
		st.ChatQueuePaused[p.ID] = deleted.ChatQueuePaused
		st.DeletedProjects = append(st.DeletedProjects[:i], st.DeletedProjects[i+1:]...)
		event(st, p.ID, "", "user", "项目已恢复", p.Name)
		return map[string]string{"projectId": p.ID}, nil
	}
	return nil, errors.New("回收站中没有该项目")
}

func splitProjectItems[T any](items []T, matches func(T) bool) (kept, removed []T) {
	kept, removed = make([]T, 0, len(items)), make([]T, 0)
	for _, item := range items {
		if matches(item) {
			removed = append(removed, item)
		} else {
			kept = append(kept, item)
		}
	}
	return
}
