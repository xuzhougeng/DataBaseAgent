package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestExampleCatalogIsComplete(t *testing.T) {
	list, err := loadExamples()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("examples: %d", len(list))
	}
	seen := map[string]bool{}
	for _, ex := range list {
		if ex.ID == "" || seen[ex.ID] || (ex.Mode != ModeStatic && ex.Mode != ModeCollect) || ex.Title == "" || ex.Summary == "" || ex.Project.Name == "" {
			t.Fatalf("bad example %#v", ex)
		}
		seen[ex.ID] = true
		groups := map[string]bool{}
		for _, g := range ex.Groups {
			groups[g.ID] = true
		}
		for _, f := range ex.Files {
			if f.Text == "" || f.Title == "" || !groups[f.Group] {
				t.Fatalf("%s: file %s has no text, title or known group", ex.ID, f.Path)
			}
		}
		if ex.Mode == ModeStatic && (ex.Template == nil || ex.file(ex.SiteData) == "") {
			t.Fatalf("%s: website example needs a bundled template and a listed data file", ex.ID)
		}
		if ex.Candidates != "" && ex.file(ex.Candidates) == "" {
			t.Fatalf("%s: candidates file is not listed", ex.ID)
		}
	}
}

func TestExamplePreviewAndProjects(t *testing.T) {
	s := skillService(t)
	h := Handler(s, fstest.MapFS{})
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/api/examples")
	var body struct {
		Examples []Example `json:"examples"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &body) != nil || len(body.Examples) != 3 || body.Examples[0].Files[0].Text == "" {
		t.Fatalf("catalog: %d %s", rec.Code, rec.Body.String()[:min(200, rec.Body.Len())])
	}
	rec = get("/api/example-site?id=static-research-catalog")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") || !strings.Contains(rec.Body.String(), "DEMO-0142") || !strings.Contains(rec.Body.String(), "<table>") {
		t.Fatalf("table preview: %d %s", rec.Code, rec.Header())
	}
	rec = get("/api/example-site?id=static-literature-library&download=1")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "record-card") || !strings.Contains(rec.Header().Get("Content-Disposition"), "index.html") {
		t.Fatalf("cards preview: %d", rec.Code)
	}
	if get("/api/example-site?id=collect-gpsa").Code != 404 || get("/api/example-site?id=nope").Code != 404 {
		t.Fatal("non-website examples must not render")
	}
	if n := len(s.Store.Snapshot().Projects); n != 0 {
		t.Fatalf("previews created %d projects", n)
	}

	create := func(id string) map[string]any {
		t.Helper()
		b, _ := json.Marshal(Request{ExampleID: id})
		out, err := s.Dispatch(context.Background(), "createExampleProject", b)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return out.(map[string]any)
	}
	first := create("static-research-catalog")
	again := create("static-research-catalog")
	if first["created"] != true || again["created"] != false || again["projectId"] != first["projectId"] {
		t.Fatalf("second create should reopen: %#v %#v", first, again)
	}
	p := s.Store.Snapshot().Projects[0]
	if p.Mode != ModeStatic || p.Example != "static-research-catalog" || p.Site == nil || p.Site.SkillName != "research-catalog" || len(p.Site.Rows) != 3 {
		t.Fatalf("static example project: %#v", p)
	}

	create("collect-gpsa")
	p = s.Store.Snapshot().Projects[1]
	if p.Mode != ModeCollect || p.Site != nil || len(p.Candidates) != 3 || p.Query == "" || p.Criteria == "" || p.Source != "Europe PMC" || p.Draft.Name == "" {
		t.Fatalf("collect example project: %#v", p)
	}
	for _, c := range p.Candidates {
		if c.Decision != "pending" || !strings.HasPrefix(c.Accession, "DEMO-") {
			t.Fatalf("candidate %#v", c)
		}
	}

	// A disabled template blocks a new example project but the preview keeps working.
	var skillID string
	for _, sk := range s.Store.Snapshot().Skills {
		if sk.Name == "literature-library" {
			skillID = sk.ID
		}
	}
	off := false
	action(t, s, "toggleSkill", Request{SkillID: skillID, Enabled: &off})
	dispatchError(t, s, "createExampleProject", Request{ExampleID: "static-literature-library"})
	if get("/api/example-site?id=static-literature-library").Code != 200 {
		t.Fatal("preview should fall back to the bundled template")
	}
	dispatchError(t, s, "createExampleProject", Request{ExampleID: "missing"})
	if n := len(s.Store.Snapshot().Projects); n != 2 {
		t.Fatalf("failed creates left %d projects", n)
	}
}
