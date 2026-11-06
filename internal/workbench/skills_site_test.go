package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func skillService(t *testing.T) *Service {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(store)
	t.Cleanup(s.Stop)
	return s
}
func dispatchError(t *testing.T, s *Service, action string, r Request) {
	t.Helper()
	raw, _ := json.Marshal(r)
	if _, err := s.Dispatch(context.Background(), action, raw); err == nil {
		t.Fatalf("%s should fail", action)
	}
}

func TestSkillLifecycleAndAgentAccess(t *testing.T) {
	s := skillService(t)
	content := "---\nname: custom-catalog\ndescription: >\n  Custom catalogue workflow\n  for existing data.\nmetadata:\n  dba:\n    label: Custom\n    layout: table\n    required-columns: [id]\n---\nUse real data."
	action(t, s, "importSkill", Request{SkillContent: content})
	st := s.Store.Snapshot()
	skill := st.Skills[len(st.Skills)-1]
	if skill.Template == nil || !strings.Contains(skill.Description, "existing data") {
		t.Fatalf("missing metadata: %#v", skill)
	}
	for _, aid := range []string{"assistant", "scout", "reviewer", "pipeline", "operator"} {
		out := callTool(t, s, "", aid, "read_skill", map[string]any{"name": "custom-catalog"})
		if !strings.Contains(out, "Use real data.") {
			t.Fatal(out)
		}
	}
	dispatchError(t, s, "importSkill", Request{SkillContent: content})
	disabled := false
	action(t, s, "toggleSkill", Request{SkillID: skill.ID, Enabled: &disabled})
	a, _ := agentByID(ptr(s.Store.Snapshot()), "assistant")
	if _, failed := s.runTool(context.Background(), "", *a, toolCall("read_skill", map[string]any{"name": skill.Name})); !failed {
		t.Fatal("disabled skill readable")
	}
	if strings.Contains(skillPrompt(ptr(s.Store.Snapshot())), skill.Name) {
		t.Fatal("disabled skill advertised")
	}
	reopened, err := Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Skills[len(st.Skills)-1].Enabled {
		t.Fatal("disabled state lost")
	}
	for _, v := range reopened.Snapshot().Skills {
		action(t, s, "deleteSkill", Request{SkillID: v.ID})
	}
	reopened, err = Open(s.Store.Root())
	if err != nil || len(reopened.Snapshot().Skills) != 0 {
		t.Fatal("deleted builtins returned", err)
	}
}

func TestSkillRejectsMalformedInstructionsAndTemplates(t *testing.T) {
	for _, content := range []string{
		"no frontmatter", "---\nname: Bad Name\ndescription: Test\n---\nText", "---\nname: empty\ndescription: Test\n---\n",
		"---\nname: dup\nname: dup\ndescription: Test\n---\nText", strings.Repeat("x", 12001),
		"---\nname: bad-layout\ndescription: Test\nmetadata:\n  dba:\n    label: Bad\n    layout: script\n---\nText",
		"---\nname: bad-color\ndescription: Test\nmetadata:\n  dba:\n    label: Bad\n    layout: table\n    accent: 'url(https://example.com)'\n---\nText",
	} {
		if _, err := parseSkill(content); err == nil {
			t.Errorf("accepted %q", content)
		}
	}
}

func TestProjectModesAndStaticTemplateSnapshots(t *testing.T) {
	s := skillService(t)
	callTool(t, s, "", "assistant", "create_project", map[string]any{"name": "Static", "mode": "static"})
	st := s.Store.Snapshot()
	p := st.Projects[0]
	if p.Mode != ModeStatic {
		t.Fatal(p.Mode)
	}
	for _, action := range []string{"search", "importCandidates", "reviewAgent", "review", "savePipeline", "lockPipeline", "launch"} {
		dispatchError(t, s, action, Request{ProjectID: p.ID})
	}
	skill := st.Skills[1]
	args := map[string]any{"skillId": skill.ID, "name": "<script>title</script>", "description": "<img src=x onerror=bad()>", "format": "json", "data": `[{"title":"A","summary":"</script><script>bad()</script>"},{"title":"B","summary":"Plain text"}]`}
	callTool(t, s, p.ID, "assistant", "build_static_site", args)
	st = s.Store.Snapshot()
	site := st.Projects[0].Site
	if site == nil || len(site.Rows) != 2 || site.Template.Layout != "cards" {
		t.Fatalf("site: %#v", site)
	}
	html, err := renderStaticSite(site)
	if err != nil || !strings.Contains(html, "record-card") || !strings.Contains(html, "&lt;script&gt;") || strings.Contains(html, "<script>bad()") || strings.Contains(html, "ZgotmplZ") {
		t.Fatalf("bad rendering: %v\n%s", err, html)
	}
	action(t, s, "deleteSkill", Request{SkillID: skill.ID})
	reopened, err := Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	html2, err := renderStaticSite(reopened.Snapshot().Projects[0].Site)
	if err != nil || html != html2 {
		t.Fatal("site changed when skill deleted")
	}
	a, _ := agentByID(ptr(s.Store.Snapshot()), "assistant")
	if _, failed := s.runTool(context.Background(), p.ID, *a, toolCall("build_static_site", args)); !failed {
		t.Fatal("used deleted skill")
	}
	action(t, s, "createProject", Request{Name: "Collection"})
	collect := s.Store.Snapshot().Projects[1]
	if collect.Mode != ModeCollect {
		t.Fatal("legacy default changed")
	}
	dispatchError(t, s, "buildStaticSite", Request{ProjectID: collect.ID, SkillID: st.Skills[0].ID, Name: "Bad", Format: "csv", Data: "id,title\n1,A"})
	dispatchError(t, s, "createProject", Request{Name: "Bad", Mode: "dynamic"})
}

func TestStaticDataValidationAndAtomicRebuild(t *testing.T) {
	s := skillService(t)
	action(t, s, "createProject", Request{Name: "Static", Mode: ModeStatic})
	st := s.Store.Snapshot()
	pid := st.Projects[0].ID
	sid := st.Skills[0].ID
	valid := Request{ProjectID: pid, SkillID: sid, Name: "Table", Format: "csv", Data: "\ufeffid,title,optional\n1,\"Two, words\",\n"}
	action(t, s, "buildStaticSite", valid)
	before, _ := json.Marshal(s.Store.Snapshot().Projects[0].Site)
	for _, r := range []Request{
		{Format: "csv", Data: "id,title\n1"}, {Format: "csv", Data: "id,id\n1,2"}, {Format: "csv", Data: "id,title\n"},
		{Format: "json", Data: `[null]`}, {Format: "json", Data: `[{"title":"Missing id"}]`}, {Format: "json", Data: `[]`},
		{Format: "json", Data: strings.Repeat("x", 2<<20+1)}, {Format: "xml", Data: "x"},
	} {
		r.ProjectID, r.SkillID, r.Name = pid, sid, "Changed"
		dispatchError(t, s, "buildStaticSite", r)
	}
	after, _ := json.Marshal(s.Store.Snapshot().Projects[0].Site)
	if string(before) != string(after) {
		t.Fatal("invalid rebuild changed saved site")
	}
	site, err := newStaticSite("TSV", "", "tsv", "id\ttitle\toptional\n1\tA\t\n")
	if err != nil || site.Rows[0][2] != "" {
		t.Fatal("trailing TSV empty cell", err)
	}
	site, err = newStaticSite("JSON", "", "json", `[{"id":9007199254740993,"title":"A"},{"id":2,"title":"B","extra":true}]`)
	if err != nil || len(site.Columns) != 3 || !strings.Contains(strings.Join(site.Rows[0], "|"), "9007199254740993") {
		t.Fatal("JSON values lost", err)
	}
}

func TestStaticSiteHTTPPreviewAndDownload(t *testing.T) {
	s := skillService(t)
	action(t, s, "createProject", Request{Name: "Static", Mode: ModeStatic})
	st := s.Store.Snapshot()
	pid := st.Projects[0].ID
	action(t, s, "buildStaticSite", Request{ProjectID: pid, SkillID: st.Skills[0].ID, Name: "Test", Format: "csv", Data: "id,title\n1,One"})
	h := Handler(s, fstest.MapFS{})
	for _, download := range []string{"", "&download=1"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/site?projectId="+pid+download, nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") || !strings.Contains(w.Body.String(), "One") {
			t.Fatalf("preview failed: %v", w)
		}
		if download != "" && !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
			t.Fatal("not downloadable")
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/site?projectId="+pid, nil)
	r.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("cross-origin site exposed")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/site?projectId=missing", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestLegacyWorkspaceGetsCollectionModeAndTemplatesOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "workbench.json"), []byte(`{"schema":3,"projects":[{"id":"legacy","name":"Old"}],"settings":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	st := store.Snapshot()
	if st.Projects[0].Mode != ModeCollect || len(st.Skills) != 2 {
		t.Fatal("migration missing")
	}
	second, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if second.Snapshot().Skills[0].ID != st.Skills[0].ID {
		t.Fatal("template IDs changed")
	}
}
