package workbench

import (
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
	mu    sync.RWMutex
	state State
	root  string
}

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
	s := &Store{root: abs, state: State{Schema: SchemaVersion, Projects: []Project{}, Servers: []Server{}, Jobs: []Job{}, Artifacts: []Artifact{}, Events: []Event{}, Settings: Settings{IntervalSeconds: 60, MaxRetries: 3, Models: []ModelProfile{}}}}
	b, err := os.ReadFile(filepath.Join(abs, "workbench.json"))
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
			s.state.Schema = SchemaVersion
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
	s.state.Settings.Agents = withPresets(s.state.Settings.Agents)
	return s, nil
}

func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, _ := json.Marshal(s.state)
	var v State
	_ = json.Unmarshal(b, &v)
	return v
}
func (s *Store) Root() string { return s.root }

func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, _ := json.Marshal(s.state)
	if err := fn(&s.state); err != nil {
		_ = json.Unmarshal(before, &s.state)
		return err
	}
	if len(s.state.Events) > 2000 {
		s.state.Events = s.state.Events[len(s.state.Events)-2000:]
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		_ = json.Unmarshal(before, &s.state)
		return err
	}
	tmp, err := os.CreateTemp(s.root, ".workbench-*.tmp")
	if err != nil {
		_ = json.Unmarshal(before, &s.state)
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	cerr := tmp.Close()
	if err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(s.root, "workbench.json"))
	}
	if err != nil {
		_ = json.Unmarshal(before, &s.state)
		return fmt.Errorf("保存失败: %w", err)
	}
	return nil
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
