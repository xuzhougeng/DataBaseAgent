package workbench

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type queuedModelCall struct {
	Turns []struct{ Role, Content string }
	Reply chan string
}

func chatFixture(t *testing.T) (*Service, <-chan queuedModelCall) {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(store)
	action(t, s, "saveModel", Request{Model: ModelProfile{Name: "queue test", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "test"}})
	started := make(chan queuedModelCall, 30)
	old := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		call := queuedModelCall{Turns: body.Messages, Reply: make(chan string, 1)}
		select {
		case started <- call:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		select {
		case response := <-call.Reply:
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	t.Cleanup(func() { s.Stop(); httpClient = old })
	return s, started
}

func nextModelCall(t *testing.T, calls <-chan queuedModelCall) queuedModelCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("queued model call did not start")
		return queuedModelCall{}
	}
}

func queueMessage(t *testing.T, s *Service, r Request) (MessageReceipt, <-chan error) {
	t.Helper()
	done := make(chan error, 1)
	receipt, err := s.enqueueMessage(r, done, false)
	if err != nil {
		t.Fatal(err)
	}
	return receipt, done
}

func chatFinished(t *testing.T, done <-chan error, wantError bool) {
	t.Helper()
	select {
	case err := <-done:
		if (err != nil) != wantError {
			t.Fatalf("unexpected chat result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("chat did not finish")
	}
}

func userContents(call queuedModelCall) []string {
	out := []string{}
	for _, turn := range call.Turns {
		if turn.Role == "user" {
			out = append(out, turn.Content)
		}
	}
	return out
}

func TestChatQueueFIFOEditsCancellationAndContextIsolation(t *testing.T) {
	s, started := chatFixture(t)
	first, done1 := queueMessage(t, s, Request{Message: "first", ClientID: "one"})
	call1 := nextModelCall(t, started)
	second, done2 := queueMessage(t, s, Request{ConversationID: first.ConversationID, Message: "second", ClientID: "two"})
	third, done3 := queueMessage(t, s, Request{ConversationID: first.ConversationID, Message: "remove me", ClientID: "three"})
	fourth, done4 := queueMessage(t, s, Request{Message: "separate context", ClientID: "four"})
	if _, err := s.enqueueMessage(Request{ConversationID: first.ConversationID, Message: "first", ClientID: "one"}, nil, false); err != nil {
		t.Fatal(err)
	}
	if len(s.Store.Snapshot().Messages) != 4 {
		t.Fatal("idempotent submission added a duplicate")
	}
	if err := s.manageConversation("editQueuedMessage", Request{MessageID: second.MessageID, Message: "second edited"}); err != nil {
		t.Fatal(err)
	}
	if err := s.manageConversation("editQueuedMessage", Request{MessageID: first.MessageID, Message: "bad edit"}); err == nil {
		t.Fatal("edited an executing request")
	}
	if err := s.manageConversation("cancelMessage", Request{MessageID: third.MessageID}); err != nil {
		t.Fatal(err)
	}
	chatFinished(t, done3, true)
	if got := strings.Join(userContents(call1), "|"); got != "first" {
		t.Fatalf("future messages leaked into first turn: %s", got)
	}
	call1.Reply <- modelReply()
	chatFinished(t, done1, false)
	call2 := nextModelCall(t, started)
	if got := strings.Join(userContents(call2), "|"); got != "first|second edited" {
		t.Fatalf("missing or unordered prior context: %s", got)
	}
	call2.Reply <- modelReply()
	chatFinished(t, done2, false)
	call4 := nextModelCall(t, started)
	if got := strings.Join(userContents(call4), "|"); got != "separate context" {
		t.Fatalf("cross-conversation context leak: %s", got)
	}
	call4.Reply <- modelReply()
	chatFinished(t, done4, false)
	st := s.Store.Snapshot()
	if len(st.Conversations) != 2 || first.ConversationID == fourth.ConversationID || len(st.Messages) != 7 {
		t.Fatalf("unexpected conversations: %#v", st.Conversations)
	}
	for _, m := range st.Messages {
		if m.ID == third.MessageID && m.Status != "cancelled" {
			t.Fatal("cancelled request was executed")
		}
	}
}

func TestChatQueueStopPausesFollowingMessagesAndAllowsResume(t *testing.T) {
	s, started := chatFixture(t)
	first, done1 := queueMessage(t, s, Request{Message: "stop this"})
	call := nextModelCall(t, started)
	call.Reply <- modelReply(toolCall("list_projects", map[string]any{}))
	nextModelCall(t, started) // the first tool has completed; stop during the following model request
	second, done2 := queueMessage(t, s, Request{ConversationID: first.ConversationID, Message: "next"})
	if err := s.manageConversation("cancelMessage", Request{MessageID: first.MessageID}); err != nil {
		t.Fatal(err)
	}
	chatFinished(t, done1, true)
	st := s.Store.Snapshot()
	if !st.ChatQueuePaused[""] || len(s.AgentRuns()) != 0 {
		t.Fatal("stop did not pause the queue")
	}
	msg, _ := messageByID(st.Messages, second.MessageID)
	if msg.Status != "queued" {
		t.Fatalf("following message ran after stop: %s", msg.Status)
	}
	last := st.Messages[len(st.Messages)-1]
	if last.Status != "cancelled" || len(last.Trace) != 1 || last.Trace[0].Step != 1 || last.Trace[0].Tool != "list_projects" {
		t.Fatalf("completed tool trace lost on stop: %#v", last)
	}
	// The async queue's waiter remains useful across a deliberate pause.
	select {
	case err := <-done2:
		t.Fatalf("pending request completed while paused: %v", err)
	default:
	}
	if err := s.manageConversation("resumeChatQueue", Request{}); err != nil {
		t.Fatal(err)
	}
	nextModelCall(t, started).Reply <- modelReply()
	chatFinished(t, done2, false)
}

func TestChatQueueFailureRetryAndScopeValidation(t *testing.T) {
	s, started := chatFixture(t)
	first, done := queueMessage(t, s, Request{Message: "fail first"})
	nextModelCall(t, started).Reply <- `{"error":{"message":"model unavailable"}}`
	chatFinished(t, done, true)
	if !s.Store.Snapshot().ChatQueuePaused[""] {
		t.Fatal("failure did not pause queue")
	}
	boolp := func(v bool) *bool { return &v }
	if err := s.manageConversation("updateConversation", Request{ConversationID: first.ConversationID, Archived: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.enqueueMessage(Request{RetryOf: first.MessageID}, nil, false); err == nil {
		t.Fatal("retried an archived conversation")
	}
	if err := s.manageConversation("updateConversation", Request{ConversationID: first.ConversationID, Archived: boolp(false), Pinned: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	retry, retried := queueMessage(t, s, Request{RetryOf: first.MessageID, ClientID: "retry"})
	if retry.ConversationID != first.ConversationID {
		t.Fatal("retry lost original conversation")
	}
	if err := s.manageConversation("updateConversation", Request{ConversationID: first.ConversationID, Archived: boolp(true)}); err == nil {
		t.Fatal("archived a pending conversation")
	}
	action(t, s, "createProject", Request{Name: "another project"})
	pid := s.Store.Snapshot().Projects[0].ID
	for _, r := range []Request{{ProjectID: pid, ConversationID: first.ConversationID, Message: "wrong scope"}, {ConversationID: first.ConversationID, AgentID: "scout", Message: "wrong agent"}} {
		if _, err := s.enqueueMessage(r, nil, false); err == nil {
			t.Fatal("accepted invalid conversation scope")
		}
	}
	if err := s.manageConversation("resumeChatQueue", Request{}); err != nil {
		t.Fatal(err)
	}
	call := nextModelCall(t, started)
	if got := strings.Join(userContents(call), "|"); got != "fail first" {
		t.Fatalf("failed response leaked into retry context: %s", got)
	}
	call.Reply <- modelReply()
	chatFinished(t, retried, false)
}

func TestChatQueueIndependentProjectsAndLimit(t *testing.T) {
	s, started := chatFixture(t)
	action(t, s, "createProject", Request{Name: "independent"})
	pid := s.Store.Snapshot().Projects[0].ID
	_, done1 := queueMessage(t, s, Request{Message: "workspace"})
	call1 := nextModelCall(t, started)
	_, done2 := queueMessage(t, s, Request{ProjectID: pid, Message: "project"})
	call2 := nextModelCall(t, started)
	if len(s.AgentRuns()) != 2 {
		t.Fatal("separate projects should run independently")
	}
	for i := 0; i < maxQueuedMessages; i++ {
		if _, err := s.enqueueMessage(Request{Message: "waiting"}, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.enqueueMessage(Request{Message: "overflow"}, nil, false); err == nil {
		t.Fatal("queue limit not enforced")
	}
	if err := s.manageConversation("pauseChatQueue", Request{}); err != nil {
		t.Fatal(err)
	}
	call1.Reply <- modelReply()
	call2.Reply <- modelReply()
	chatFinished(t, done1, false)
	chatFinished(t, done2, false)
}

func TestChatQueueHTTPDisconnectDoesNotCancelExecution(t *testing.T) {
	s, started := chatFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "http://localhost/api/enqueueMessage", strings.NewReader(`{"message":"durable acceptance","clientId":"http-once"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	Handler(s, fstest.MapFS{}).ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("enqueue failed: %s", w.Body.String())
	}
	call := nextModelCall(t, started)
	cancel()
	var body struct {
		Result MessageReceipt `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	s.agentMu.Lock()
	s.chatWaiters[body.Result.MessageID] = done
	s.agentMu.Unlock()
	call.Reply <- modelReply()
	chatFinished(t, done, false)
	st := s.Store.Snapshot()
	m, _ := messageByID(st.Messages, body.Result.MessageID)
	if m.Status != "completed" {
		t.Fatalf("request disconnect cancelled model: %s", m.Status)
	}
}

func TestConversationMigrationAndCrashRecovery(t *testing.T) {
	root := t.TempDir()
	legacy := `{"schema":2,"projects":[{"id":"p","messages":[{"role":"user","agent":"assistant","content":"project old","at":"2026-01-01T00:00:00Z"},{"role":"assistant","agent":"assistant","content":"project reply","at":"2026-01-01T00:00:01Z"}]}],"messages":[{"role":"user","agent":"assistant","content":"workspace old"},{"role":"assistant","agent":"assistant","content":"workspace reply"},{"role":"user","agent":"scout","content":"scout old"}],"settings":{}}`
	if err := os.WriteFile(filepath.Join(root, "workbench.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	st := store.Snapshot()
	if st.Schema != SchemaVersion || len(st.Conversations) != 3 || len(st.Messages) != 3 || st.Messages[0].Status != "completed" || st.Messages[2].Status != "interrupted" || st.Messages[1].ReplyTo != st.Messages[0].ID {
		t.Fatalf("legacy history migration lost messages: %#v", st)
	}
	cid := st.Messages[0].ConversationID
	if err := store.Update(func(st *State) error {
		st.Messages = append(st.Messages, Message{ID: "crashed", ConversationID: cid, Role: "user", Agent: "assistant", Content: "running before crash", Status: "running", Trace: []Trace{{Tool: "create_project", Result: "saved"}}}, Message{ID: "waiting", ConversationID: cid, Role: "user", Agent: "assistant", Content: "queued before crash", Status: "queued"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	recovered := reopened.Snapshot()
	running, _ := messageByID(recovered.Messages, "crashed")
	waiting, _ := messageByID(recovered.Messages, "waiting")
	if running.Status != "interrupted" || waiting.Status != "queued" || !recovered.ChatQueuePaused[""] {
		t.Fatal("crash recovery did not preserve and pause queue")
	}
	last := recovered.Messages[len(recovered.Messages)-1]
	if last.ReplyTo != "crashed" || len(last.Trace) != 1 {
		t.Fatal("crash recovery lost completed tool trace")
	}
	again, err := Open(root)
	if err != nil || len(again.Snapshot().Messages) != len(recovered.Messages) {
		t.Fatal("reopening duplicated interrupted replies")
	}
	if again.Snapshot().Conversations[0].ID != st.Conversations[0].ID {
		t.Fatal("migration IDs were not durably saved")
	}
}
