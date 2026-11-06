package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
)

func hasHan(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}

// shape reduces a TSV to its header plus row and column counts, or JSON to its key paths.
func exampleShape(t *testing.T, path, text string) string {
	t.Helper()
	if strings.HasSuffix(path, ".tsv") {
		lines := strings.Split(strings.TrimSpace(text), "\n")
		out := lines[0]
		for _, line := range lines[1:] {
			out += "|" + strings.Repeat("\t", strings.Count(line, "\t"))
		}
		return out
	}
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var paths []string
	var walk func(any, string)
	walk = func(v any, at string) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				paths = append(paths, at+"."+k)
				walk(child, at+"."+k)
			}
		case []any:
			paths = append(paths, at+"["+strconv.Itoa(len(v))+"]")
			for _, child := range v {
				walk(child, at+"[]")
			}
		}
	}
	walk(v, "")
	sort.Strings(paths)
	return strings.Join(paths, ";")
}

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

		// English mode must show no Chinese data, and each copy keeps the original's shape.
		if ex.ProjectEn == nil || ex.ProjectEn.Name == "" || ex.ProjectEn.Query != ex.Project.Query {
			t.Fatalf("%s: projectEn missing or query differs", ex.ID)
		}
		if b, _ := json.Marshal(ex.ProjectEn); hasHan(string(b)) {
			t.Fatalf("%s: projectEn has Chinese text", ex.ID)
		}
		for _, f := range ex.Files {
			english := f.Text
			if f.En != "" {
				english = f.textEn
				if exampleShape(t, f.Path, f.Text) != exampleShape(t, f.En, f.textEn) {
					t.Fatalf("%s: %s and %s differ in columns, rows or keys", ex.ID, f.Path, f.En)
				}
			}
			if hasHan(english) {
				t.Fatalf("%s: %s has Chinese text but no English copy", ex.ID, f.Path)
			}
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

	rec = get("/api/examples?lang=en")
	body.Examples = nil
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &body) != nil || len(body.Examples) != 3 {
		t.Fatalf("English catalog: %d", rec.Code)
	}
	for _, ex := range body.Examples {
		if ex.ProjectEn != nil || hasHan(ex.Project.Name) || strings.Contains(rec.Body.String(), `"en":`) {
			t.Fatalf("%s: English catalog should carry only English data", ex.ID)
		}
		for _, f := range ex.Files {
			if hasHan(f.Text) {
				t.Fatalf("%s: %s is still Chinese", ex.ID, f.Path)
			}
		}
	}
	rec = get("/api/example-site?id=static-research-catalog&lang=en")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Suggest include") || strings.Contains(rec.Body.String(), "建议纳入") || !strings.Contains(rec.Body.String(), "Example · GPSA research catalogue") {
		t.Fatalf("English preview: %d", rec.Code)
	}
	if n := len(s.Store.Snapshot().Projects); n != 0 {
		t.Fatalf("previews created %d projects", n)
	}

	create := func(id string, lang ...string) map[string]any {
		t.Helper()
		b, _ := json.Marshal(Request{ExampleID: id, Lang: strings.Join(lang, "")})
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

	// Each language gets its own example project; the Chinese one stays as it was.
	en := create("collect-gpsa", "en")
	if en["created"] != true || create("collect-gpsa", "en")["projectId"] != en["projectId"] || create("collect-gpsa")["created"] != false {
		t.Fatalf("English example project: %#v", en)
	}
	p = s.Store.Snapshot().Projects[2]
	if p.ExampleLang != "en" || p.Name != "Example · GPSA virtual perturbation database" || hasHan(p.Criteria) || len(p.Candidates) != 3 || hasHan(p.Candidates[0].Abstract) || p.Candidates[0].Source != "Design demo" {
		t.Fatalf("English collect project: %#v", p)
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
	if n := len(s.Store.Snapshot().Projects); n != 3 {
		t.Fatalf("failed creates left %d projects", n)
	}
}
