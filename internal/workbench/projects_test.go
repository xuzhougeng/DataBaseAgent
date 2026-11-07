package workbench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestProjectTrashRoundTrip(t *testing.T) {
	s, pid := fixture(t)
	action(t, s, "createProject", Request{Name: "unrelated"})
	other := s.Store.Snapshot().Projects[1]
	file := filepath.Join(s.Store.Root(), "retained-result.tsv")
	if err := os.WriteFile(file, []byte("gene\tcount\nA\t1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Update(func(st *State) error {
		st.Jobs = append(st.Jobs, Job{ID: "completed", ProjectID: pid, Status: "succeeded"})
		st.Artifacts = append(st.Artifacts, Artifact{ID: "result", ProjectID: pid, JobID: "completed", Path: file})
		st.Conversations = append(st.Conversations, Conversation{ID: "chat", ProjectID: pid, Title: "Saved chat"})
		st.Projects[0].Messages = append(st.Projects[0].Messages, Message{ID: "message", ConversationID: "chat", Role: "user", Status: "completed", Content: "Keep me"})
		st.ChatQueuePaused[pid] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := s.Store.Snapshot()
	action(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})
	after := s.Store.Snapshot()
	if len(after.Projects) != 1 || !reflect.DeepEqual(after.Projects[0], other) || len(after.Jobs) != 0 || len(after.Artifacts) != 0 || len(after.Conversations) != 0 || len(after.DeletedProjects) != 1 {
		t.Fatal("deletion did not isolate project state")
	}
	if _, ok := after.ChatQueuePaused[pid]; ok {
		t.Fatal("active queue state retained")
	}
	for _, e := range after.Events {
		if e.ProjectID == pid {
			t.Fatal("active project event retained")
		}
	}
	if !reflect.DeepEqual(before.Projects[0], after.DeletedProjects[0].Project) {
		t.Fatal("project contents changed in trash")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("local result was removed", err)
	}
	dispatchError(t, s, "saveProject", Request{ProjectID: pid, Name: "cannot edit deleted project"})
	dispatchError(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})

	store, err := Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	reopened := NewService(store)
	t.Cleanup(reopened.Stop)
	action(t, reopened, "restoreProject", Request{ProjectID: pid})
	restored := reopened.Store.Snapshot()
	for i := 1; i < len(restored.Events); i++ {
		if restored.Events[i-1].At > restored.Events[i].At {
			t.Fatal("restored events are out of order")
		}
	}
	p, err := projectByID(&restored, pid)
	if err != nil || !reflect.DeepEqual(*p, before.Projects[0]) || !reflect.DeepEqual(restored.Jobs, before.Jobs) || !reflect.DeepEqual(restored.Artifacts, before.Artifacts) || !reflect.DeepEqual(restored.Conversations, before.Conversations) || !restored.ChatQueuePaused[pid] || len(restored.DeletedProjects) != 0 {
		t.Fatal("restore lost project data")
	}
	dispatchError(t, reopened, "restoreProject", Request{ProjectID: pid})
}

func TestProjectDeletionGuards(t *testing.T) {
	for _, status := range []string{"queued", "starting", "running", "retry_wait", "syncing", "needs_help", "unknown"} {
		t.Run("job_"+status, func(t *testing.T) {
			s, pid := fixture(t)
			if err := s.Store.Update(func(st *State) error { st.Jobs = append(st.Jobs, Job{ProjectID: pid, Status: status}); return nil }); err != nil {
				t.Fatal(err)
			}
			before := s.Store.Snapshot()
			dispatchError(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})
			if !reflect.DeepEqual(before, s.Store.Snapshot()) {
				t.Fatal("blocked deletion mutated state")
			}
		})
	}
	for _, status := range []string{"queued", "running", "stopping"} {
		t.Run("message_"+status, func(t *testing.T) {
			s, pid := fixture(t)
			if err := s.Store.Update(func(st *State) error { st.Projects[0].Messages = []Message{{Role: "user", Status: status}}; return nil }); err != nil {
				t.Fatal(err)
			}
			dispatchError(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})
		})
	}
	s, pid := fixture(t)
	dispatchError(t, s, "deleteProject", Request{ProjectID: pid})
	dispatchError(t, s, "deleteProject", Request{ProjectID: "missing", Approve: true})
	s.projectMu.RLock()
	dispatchError(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})
	s.projectMu.RUnlock()
	s.agentRuns[""] = AgentRun{ProjectID: ""}
	dispatchError(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})
	delete(s.agentRuns, "")
	s.chatWorkers[pid] = true
	dispatchError(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})
	delete(s.chatWorkers, pid)
	action(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})
}

func TestProjectDeletionSerializesWithLaunch(t *testing.T) {
	s, pid := fixture(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, name := range []string{"deleteProject", "launch"} {
		wg.Go(func() {
			raw, _ := json.Marshal(Request{ProjectID: pid, Approve: true})
			_, errs[i] = s.Dispatch(context.Background(), name, raw)
		})
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("exactly one operation should succeed: %v", errs)
	}
	st := s.Store.Snapshot()
	if len(st.Projects) == 0 && len(st.Jobs) != 0 {
		t.Fatal("orphan job after deletion")
	}
}

func TestProjectTrashWriteFailureRollsBack(t *testing.T) {
	s, pid := fixture(t)
	file := filepath.Join(s.Store.Root(), "workbench.json")
	if err := os.Rename(file, file+".bak"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	before := s.Store.Snapshot()
	dispatchError(t, s, "deleteProject", Request{ProjectID: pid, Approve: true})
	if !reflect.DeepEqual(before, s.Store.Snapshot()) {
		t.Fatal("failed save lost project")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".bak", file); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreExampleAfterCreatingReplacement(t *testing.T) {
	s := skillService(t)
	r := Request{ExampleID: "static-research-catalog", Lang: "zh"}
	action(t, s, "createExampleProject", r)
	original := s.Store.Snapshot().Projects[0]
	action(t, s, "deleteProject", Request{ProjectID: original.ID, Approve: true})
	action(t, s, "createExampleProject", r)
	replacement := s.Store.Snapshot().Projects[0]
	action(t, s, "restoreProject", Request{ProjectID: original.ID})
	st := s.Store.Snapshot()
	restored, _ := projectByID(&st, original.ID)
	if restored.Example != "" || !reflect.DeepEqual(restored.Site, original.Site) {
		t.Fatal("restored example contents or identity incorrect")
	}
	action(t, s, "createExampleProject", r)
	if st = s.Store.Snapshot(); len(st.Projects) != 2 || st.Projects[0].ID != replacement.ID {
		t.Fatal("duplicate example created")
	}
}
