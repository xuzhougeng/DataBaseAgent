package workbench

import (
	"fmt"
	"strings"
	"testing"
)

// largeWorkspace approximates a workspace after months of use: three projects with
// 2,000 candidates each (abstracts ≈ 1.5 KB) and one long conversation whose tool
// traces dominate the document (≈ 17 MB serialized).
func largeWorkspace(b *testing.B) *Store {
	b.Helper()
	store, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	abstract, result := strings.Repeat("x", 1500), strings.Repeat("y", 4000)
	err = store.Update(func(st *State) error {
		for p := 0; p < 3; p++ {
			project := newProject(ModeCollect, fmt.Sprintf("project %d", p), "")
			for c := 0; c < 2000; c++ {
				project.Candidates = append(project.Candidates, Candidate{ID: id("cand"), Title: fmt.Sprintf("candidate %d", c), Abstract: abstract, Decision: "pending"})
			}
			if p == 0 {
				for m := 0; m < 400; m++ {
					msg := Message{ID: id("msg"), Role: "assistant", Agent: "assistant", Content: "reply", Status: "completed"}
					for t := 0; t < 5; t++ {
						msg.Trace = append(msg.Trace, Trace{Tool: "list_candidates", Args: "{}", Result: result})
					}
					project.Messages = append(project.Messages, msg)
				}
			}
			st.Projects = append(st.Projects, project)
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return store
}

func BenchmarkStoreUpdateWrite(b *testing.B) {
	store := largeWorkspace(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Update(func(st *State) error { st.LastTick = fmt.Sprint(i); return nil })
	}
}

func BenchmarkStoreUpdateNoop(b *testing.B) {
	store := largeWorkspace(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Update(func(*State) error { return nil })
	}
}

func BenchmarkStoreSnapshot(b *testing.B) {
	store := largeWorkspace(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Snapshot()
	}
}

func BenchmarkStateResponse(b *testing.B) {
	s := NewService(largeWorkspace(b))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.stateResponse("")
	}
}
