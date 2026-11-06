package workbench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesignDemoExamplesImport(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	s := skillService(t)
	st := s.Store.Snapshot()
	skills := map[string]string{}
	for _, sk := range st.Skills {
		skills[sk.Name] = sk.ID
	}
	if skills["research-catalog"] == "" || skills["literature-library"] == "" {
		t.Fatalf("built-in skills missing: %#v", st.Skills)
	}

	catalog := string(mustRead(t, filepath.Join(root, "static", "research-catalog.tsv")))
	literature := string(mustRead(t, filepath.Join(root, "static", "literature-library.tsv")))
	action(t, s, "createProject", Request{Mode: ModeStatic, Name: "GPSA 虚拟扰动数据库", Description: "设计稿示例"})
	staticID := s.Store.Snapshot().Projects[0].ID
	action(t, s, "buildStaticSite", Request{ProjectID: staticID, SkillID: skills["research-catalog"], Name: "GPSA 虚拟扰动数据库", Description: "设计稿示例", Format: "tsv", Data: catalog})
	site := s.Store.Snapshot().Projects[0].Site
	if site.SkillName != "research-catalog" || len(site.Rows) != 3 || site.Columns[0] != "id" || site.Columns[1] != "title" {
		t.Fatalf("catalog site: %#v", site)
	}
	action(t, s, "buildStaticSite", Request{ProjectID: staticID, SkillID: skills["literature-library"], Name: "GPSA 虚拟扰动数据库", Description: "设计稿示例", Format: "tsv", Data: literature})
	site = s.Store.Snapshot().Projects[0].Site
	if site.SkillName != "literature-library" || site.Template.Layout != "cards" || len(site.Rows) != 3 || site.Columns[0] != "title" || site.Columns[1] != "summary" {
		t.Fatalf("literature site: %#v", site)
	}

	raw := mustRead(t, filepath.Join(root, "collect", "candidates.json"))
	var candidates []Candidate
	if err := json.Unmarshal(raw, &candidates); err != nil {
		t.Fatal(err)
	}
	action(t, s, "createProject", Request{Mode: ModeCollect, Name: "GPSA 虚拟扰动数据库", Description: "设计稿示例"})
	collectID := s.Store.Snapshot().Projects[1].ID
	action(t, s, "importCandidates", Request{ProjectID: collectID, Candidates: candidates})
	got := s.Store.Snapshot().Projects[1].Candidates
	if len(got) != 3 {
		t.Fatalf("imported %d", len(got))
	}
	for _, c := range got {
		if c.Decision != "pending" || c.Title == "" || !strings.HasPrefix(c.Accession, "DEMO-") || !strings.Contains(c.Title+"\n"+c.Abstract, c.Abstract) || c.DOI != "" || c.Year != "" || c.URL != "" {
			t.Fatalf("candidate: %#v", c)
		}
	}
	action(t, s, "importCandidates", Request{ProjectID: collectID, Candidates: candidates})
	if n := len(s.Store.Snapshot().Projects[1].Candidates); n != 3 {
		t.Fatalf("reimport grew the list to %d", n)
	}

	groups := string(mustRead(t, filepath.Join(root, "collect", "DEMO-0198-sample-groups.txt")))
	if err := validateGroups(groups); err != nil {
		t.Fatal(err)
	}
	action(t, s, "review", Request{ProjectID: collectID, CandidateID: got[1].ID, Decision: "pending", Note: "检测到基因敲低实验，但对照样本的分组信息不完整。", Groups: groups, Accession: "DEMO-0198"})

	var manifest map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(root, "collect", "dba_demo_manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["demo"] != true || manifest["project"] != "GPSA 虚拟扰动数据库" || manifest["note"] != "DBA 设计稿示例，不包含真实分析数据" {
		t.Fatalf("manifest: %#v", manifest)
	}
	var contract struct {
		DataContract map[string]json.RawMessage `json:"data_contract"`
	}
	if err := json.Unmarshal(mustRead(t, filepath.Join(root, "collect", "data-contract.json")), &contract); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"dataset_id", "source_accession", "organism", "perturbed_gene", "perturbation", "sample_groups", "pipeline_version", "output_files", "review_decision", "run_provenance"} {
		if _, ok := contract.DataContract[key]; !ok {
			t.Fatalf("missing contract field %s", key)
		}
	}
	preview := string(mustRead(t, filepath.Join(root, "collect", "artifacts", "expression_matrix.tsv")))
	if preview != "gene_id\tcontrol_01\tcontrol_02\ttreatment_01\ttreatment_02\nENSG_DEMO_001\t128\t135\t264\t278\nENSG_DEMO_002\t562\t548\t201\t189\n" {
		t.Fatalf("expression preview mismatch: %q", preview)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
