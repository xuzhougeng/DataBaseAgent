package workbench

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Store struct {
	mu      sync.RWMutex
	state   State
	root    string
	version uint64 // incremented after every committed write; lets readers skip unchanged state
	epoch   string // random per process, so versions from an earlier run never match
}

const (
	maxEvents      = 2000
	maxAgentEvents = 500 // agent activity must not push review, job and help events out of the audit trail
)

func id(prefix string) string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func Open(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	s := &Store{root: abs, epoch: id("epoch"), state: State{Schema: SchemaVersion, Projects: []Project{}, Servers: []Server{}, Jobs: []Job{}, Artifacts: []Artifact{}, Events: []Event{}, Settings: Settings{IntervalSeconds: 60, MaxRetries: 3, Models: []ModelProfile{}}}}
	b, err := os.ReadFile(filepath.Join(abs, "workbench.json"))
	changed := false
	if err == nil {
		if err = json.Unmarshal(b, &s.state); err != nil {
			return nil, fmt.Errorf("工作空间文件损坏，已保留原文件: %w", err)
		}
		if s.state.Schema == 1 { // v0.1: single flat Chat Completions model, untagged messages
			var old struct {
				Settings struct{ ModelURL, Model, APIKeyEnv string } `json:"settings"`
			}
			_ = json.Unmarshal(b, &old)
			if old.Settings.Model != "" {
				s.state.Settings.Models = []ModelProfile{{ID: "model_default", Name: "默认模型", Protocol: "openai", BaseURL: old.Settings.ModelURL, Model: old.Settings.Model, APIKeyEnv: old.Settings.APIKeyEnv}}
			}
			for i := range s.state.Projects {
				for k := range s.state.Projects[i].Messages {
					s.state.Projects[i].Messages[k].Agent = "assistant"
				}
			}
			s.state.Schema = 2
		}
		if s.state.Schema == 2 {
			migrateConversations(&s.state)
			s.state.Schema = SchemaVersion
			changed = true
		}
		if s.state.Schema != SchemaVersion {
			return nil, fmt.Errorf("不支持工作空间格式 %d", s.state.Schema)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.state.Settings.Models == nil {
		s.state.Settings.Models = []ModelProfile{}
	}
	// Only seed an absent field. An explicitly empty list means the user removed
	// all plugins and must remain empty across restarts.
	if s.state.Settings.MCPServers == nil {
		s.state.Settings.MCPServers = defaultMCPServers()
		changed = true
	}
	if s.state.Skills == nil {
		s.state.Skills = defaultSkills()
		changed = true
	}
	if upgradeBundledSkills(s.state.Skills) {
		changed = true
	}
	for i := range s.state.Projects {
		if s.state.Projects[i].Mode == "" {
			s.state.Projects[i].Mode = ModeCollect
			changed = true
		}
	}
	s.state.Settings.Agents = withPresets(s.state.Settings.Agents)
	if s.state.Conversations == nil {
		s.state.Conversations = []Conversation{}
	}
	if s.state.ChatQueuePaused == nil {
		s.state.ChatQueuePaused = map[string]bool{}
	}
	changed = recoverConversations(&s.state) || changed
	if changed {
		b, err := json.Marshal(s.state)
		if err == nil {
			err = s.writeLocked(b)
		}
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Snapshot returns a deep copy that callers may keep and modify.
// Prefer Read for short read-only access: copying the whole workspace is expensive.
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, _ := json.Marshal(s.state)
	var v State
	_ = json.Unmarshal(b, &v)
	return v
}

// Read runs fn on the committed state under the read lock, without copying it.
// fn must not modify st, keep references into it, or call back into the Store.
func (s *Store) Read(fn func(st *State) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fn(&s.state)
}

// Version identifies the committed state; it changes after every write.
func (s *Store) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *Store) Root() string { return s.root }

// Update applies fn as one transaction. On error the previous state is restored
// byte-for-byte. A transaction that changes nothing is not written to disk.
func (s *Store) Update(fn func(*State) error) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			s.state = State{}
			_ = json.Unmarshal(before, &s.state)
		}
	}()
	if err := fn(&s.state); err != nil {
		return err
	}
	trimEvents(&s.state)
	after, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return nil
	}
	if err = s.writeLocked(after); err != nil {
		return fmt.Errorf("保存失败: %w", err)
	}
	return nil
}

// writeLocked atomically replaces workbench.json: temp file, fsync, rename.
func (s *Store) writeLocked(compact []byte) error {
	var b bytes.Buffer
	if err := json.Indent(&b, compact, "", "  "); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.root, ".workbench-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b.Bytes()); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(s.root, "workbench.json"))
	}
	if err == nil {
		s.version++
	}
	return err
}

// trimEvents keeps the newest events, first dropping old agent activity beyond its own budget.
func trimEvents(st *State) {
	agent := 0
	for _, e := range st.Events {
		if e.Kind == "agent" {
			agent++
		}
	}
	if agent <= maxAgentEvents && len(st.Events) <= maxEvents {
		return
	}
	drop := agent - maxAgentEvents
	kept := st.Events[:0:0]
	for _, e := range st.Events {
		if e.Kind == "agent" && drop > 0 {
			drop--
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) > maxEvents {
		kept = kept[len(kept)-maxEvents:]
	}
	st.Events = kept
}

func projectByID(st *State, pid string) (*Project, error) {
	for i := range st.Projects {
		if st.Projects[i].ID == pid {
			return &st.Projects[i], nil
		}
	}
	return nil, errors.New("项目不存在")
}
func serverByID(st *State, sid string) (*Server, error) {
	for i := range st.Servers {
		if st.Servers[i].ID == sid {
			return &st.Servers[i], nil
		}
	}
	return nil, errors.New("请先添加执行服务器")
}
func jobByID(st *State, jid string) (*Job, error) {
	for i := range st.Jobs {
		if st.Jobs[i].ID == jid {
			return &st.Jobs[i], nil
		}
	}
	return nil, errors.New("任务不存在")
}
func event(st *State, pid, jid, kind, title, detail string) {
	st.Events = append(st.Events, Event{ID: id("event"), ProjectID: pid, JobID: jid, Kind: kind, Title: title, Detail: detail, At: now()})
}

func safeRelative(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\\r\n\t:'\"") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, p := range strings.Split(name, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-/", c)) {
			return false
		}
	}
	return true
}
