package workbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestCustomSiteUsesImportedDataAndKeepsSnapshot(t *testing.T) {
	s := skillService(t)
	action(t, s, "createProject", Request{Name: "Cells", Mode: ModeStatic})
	st := s.Store.Snapshot()
	pid := st.Projects[0].ID
	sid := st.Skills[2].ID
	if missing := inspectSiteData(&st.Projects[0], 0, 5).(map[string]string); missing["error"] == "" {
		t.Fatal("missing input not reported")
	}
	var data strings.Builder
	data.WriteString("barcode,x,y,cluster\n")
	for i := 0; i < 5025; i++ {
		fmt.Fprintf(&data, "cell%d,%d,%d,cluster%d\n", i, i, -i, i%8)
	}
	action(t, s, "importSiteData", Request{ProjectID: pid, Name: "Cells", Format: "csv", Data: data.String()})
	summary := callTool(t, s, pid, "assistant", "inspect_site_data", map[string]any{"limit": 2})
	if !strings.Contains(summary, "5025") || strings.Contains(summary, "cell5024") {
		t.Fatal("summary must be bounded", summary)
	}
	source := `<!doctype html><html><head><title>Cells</title></head><body><main id="out"></main><script>document.getElementById('out').textContent=window.DBA_DATA.rows.length;</script></body></html>`
	callTool(t, s, pid, "assistant", "build_custom_site", map[string]any{"skillId": sid, "name": "Cells", "description": "Real data", "html": source})
	site := s.Store.Snapshot().Projects[0].Site
	rendered, err := renderStaticSite(site)
	if err != nil || !strings.Contains(rendered, "cell5024") || !strings.Contains(rendered, "connect-src") {
		t.Fatal(err, "missing data or policy")
	}
	if strings.Index(rendered, "Content-Security-Policy") > strings.Index(rendered, "window.DBA_DATA") || strings.Index(rendered, "window.DBA_DATA") > strings.Index(rendered, "document.getElementById") {
		t.Fatal("policy/data must precede author script")
	}
	before, _ := json.Marshal(site)
	action(t, s, "importSiteData", Request{ProjectID: pid, Name: "New input", Format: "csv", Data: "barcode,x\nnew,1"})
	after, _ := json.Marshal(s.Store.Snapshot().Projects[0].Site)
	if string(before) != string(after) {
		t.Fatal("import changed the published snapshot")
	}
	for _, bad := range []string{"", `<script src="https://example.com/a.js"></script>`, `<meta http-equiv="refresh" content="0;url=https://example.com">`, `<iframe srcdoc="hello"></iframe>`, `<base href="https://example.com">`, `<img src="https://example.com/x">`, `<svg><use href="https://example.com/x"/></svg>`} {
		dispatchError(t, s, "buildCustomSite", Request{ProjectID: pid, SkillID: sid, Name: "Bad", HTML: bad})
	}
	after, _ = json.Marshal(s.Store.Snapshot().Projects[0].Site)
	if string(before) != string(after) {
		t.Fatal("failed rebuild changed saved site")
	}
	action(t, s, "deleteSkill", Request{SkillID: sid})
	reopened, err := Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	again, err := renderStaticSite(reopened.Snapshot().Projects[0].Site)
	if err != nil || again != rendered {
		t.Fatal("snapshot did not survive skill deletion and restart", err)
	}
	dispatchError(t, s, "buildCustomSite", Request{ProjectID: pid, SkillID: sid, Name: "Disabled", HTML: source})
	action(t, s, "createProject", Request{Name: "Collect", Mode: ModeCollect})
	collect := s.Store.Snapshot().Projects[1].ID
	dispatchError(t, s, "importSiteData", Request{ProjectID: collect, Name: "No", Format: "csv", Data: "x\n1"})
	dispatchError(t, s, "buildCustomSite", Request{ProjectID: collect, SkillID: sid, Name: "No", HTML: source})
}

func TestCustomSiteDataEscapesScriptBreakoutAndStatsMissing(t *testing.T) {
	input, err := newStaticSite("Cells", "", "json", `[{"id":"</script><script>alert(1)</script>","x":"","y":"NaN","group":"A"},{"id":"cell2","x":"2","y":"Inf","group":"A"}]`)
	if err != nil {
		t.Fatal(err)
	}
	input.HTML = `<main>Cells</main><script>const data=window.DBA_DATA;</script>`
	rendered, err := renderStaticSite(input)
	if err != nil || strings.Contains(rendered, "</script><script>alert(1)") || !strings.Contains(rendered, `\u003c/script\u003e`) {
		t.Fatal("unsafe injected data", err)
	}
	stats := inspectSiteData(&Project{SiteInput: input}, 99999, 10000)
	raw, err := json.Marshal(stats)
	if err != nil || !strings.Contains(string(raw), `"missing":1`) {
		t.Fatal("non-finite/missing statistics", err)
	}
}

func TestStaticSiteNewRowLimit(t *testing.T) {
	for _, format := range []string{"csv", "json"} {
		for _, n := range []int{10000, 10001} {
			data := "id\n" + strings.Repeat("one\n", n)
			if format == "json" {
				data = "[" + strings.TrimSuffix(strings.Repeat(`{"id":"one"},`, n), ",") + "]"
			}
			_, err := newStaticSite("Limit", "", format, data)
			if (err != nil) != (n > 10000) {
				t.Fatalf("%s %d: %v", format, n, err)
			}
		}
	}
}
