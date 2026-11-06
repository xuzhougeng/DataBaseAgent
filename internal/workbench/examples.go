package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"databaseagent/examples"
)

// Example is one entry of the built-in gallery (examples/catalog.json). Its
// files are design-demo data shipped in the binary; previews never write the
// workspace, and createExampleProject adds at most one project per example and
// language. The catalog keeps Chinese data with English copies beside it
// (projectEn, and each file's en path); localized picks one language.
type Example struct {
	ID         string          `json:"id"`
	Mode       string          `json:"mode"`
	Title      string          `json:"title"`
	Summary    string          `json:"summary"`
	Skill      string          `json:"skill,omitempty"`
	SiteData   string          `json:"siteData,omitempty"`
	Candidates string          `json:"candidates,omitempty"`
	Template   *SiteTemplate   `json:"template,omitempty"`
	Project    exampleProject  `json:"project"`
	ProjectEn  *exampleProject `json:"projectEn,omitempty"`
	Groups     []exampleGroup  `json:"groups"`
	Files      []exampleFile   `json:"files"`
}

type exampleProject struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Organism    string `json:"organism,omitempty"`
	Assay       string `json:"assay,omitempty"`
	Criteria    string `json:"criteria,omitempty"`
	Query       string `json:"query,omitempty"`
	Source      string `json:"source,omitempty"`
}

type exampleGroup struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note"`
}

type exampleFile struct {
	Path   string `json:"path"`
	En     string `json:"en,omitempty"` // English copy; language-neutral files have none
	Group  string `json:"group"`
	Title  string `json:"title"`
	Note   string `json:"note,omitempty"`
	Text   string `json:"text"`
	textEn string
}

// loadExamples reads the catalog once and attaches each file's text and, for
// website examples, the bundled template the preview falls back to.
var loadExamples = sync.OnceValues(func() ([]Example, error) {
	raw, err := fs.ReadFile(examples.FS, "catalog.json")
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Examples []Example `json:"examples"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("examples/catalog.json: %w", err)
	}
	for i := range catalog.Examples {
		ex := &catalog.Examples[i]
		for j := range ex.Files {
			text, err := fs.ReadFile(examples.FS, ex.Files[j].Path)
			if err != nil {
				return nil, fmt.Errorf("example %s: %w", ex.ID, err)
			}
			ex.Files[j].Text = string(text)
			if ex.Files[j].En != "" {
				if text, err = fs.ReadFile(examples.FS, ex.Files[j].En); err != nil {
					return nil, fmt.Errorf("example %s: %w", ex.ID, err)
				}
				ex.Files[j].textEn = string(text)
			}
		}
		if ex.Skill != "" {
			skill, err := bundledSkill(ex.Skill)
			if err != nil {
				return nil, fmt.Errorf("example %s: %w", ex.ID, err)
			}
			ex.Template = skill.Template
		}
	}
	return catalog.Examples, nil
})

// localized returns a copy holding only one language's project fields and file text.
func (ex Example) localized(lang string) Example {
	files := make([]exampleFile, len(ex.Files))
	for i, f := range ex.Files {
		if lang == "en" && f.textEn != "" {
			f.Text = f.textEn
		}
		f.En, f.textEn = "", ""
		files[i] = f
	}
	if lang == "en" && ex.ProjectEn != nil {
		ex.Project = *ex.ProjectEn
	}
	ex.ProjectEn, ex.Files = nil, files
	return ex
}

func examplesFor(lang string) ([]Example, error) {
	list, err := loadExamples()
	if err != nil {
		return nil, err
	}
	out := make([]Example, len(list))
	for i := range list {
		out[i] = list[i].localized(uiLang(lang))
	}
	return out, nil
}

func exampleByID(id, lang string) (*Example, error) {
	list, err := loadExamples()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			ex := list[i].localized(uiLang(lang))
			return &ex, nil
		}
	}
	return nil, errors.New("示例不存在")
}

func (ex *Example) file(p string) string {
	for _, f := range ex.Files {
		if f.Path == p {
			return f.Text
		}
	}
	return ""
}

// exampleSite builds the example website with the workspace SKILL of the same
// name, as a real project would. A preview falls back to the bundled template
// when that SKILL is disabled, deleted or replaced by an incompatible one.
func exampleSite(st *State, ex *Example, preview bool) (*StaticSite, error) {
	site, err := newStaticSite(ex.Project.Name, ex.Project.Description, strings.TrimPrefix(path.Ext(ex.SiteData), "."), ex.file(ex.SiteData))
	if err != nil {
		return nil, err
	}
	err = fmt.Errorf("示例需要已启用的 SKILL 模板 %s，请在 SKILL 模板页启用或重新导入", ex.Skill)
	for _, skill := range st.Skills {
		if skill.Name == ex.Skill && skill.Enabled && skill.Template != nil {
			if err = applySiteTemplate(st, skill.ID, site); err == nil {
				return site, nil
			}
		}
	}
	if !preview {
		return nil, err
	}
	skill, err := bundledSkill(ex.Skill)
	if err != nil {
		return nil, err
	}
	site.Template, site.SkillName = skill.Template, skill.Name
	return site, nil
}

func createExampleProject(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	lang := uiLang(r.Lang)
	ex, err := exampleByID(r.ExampleID, lang)
	if err != nil {
		return nil, err
	}
	for _, p := range st.Projects {
		if p.Example == ex.ID && uiLang(p.ExampleLang) == lang {
			return map[string]any{"projectId": p.ID, "created": false}, nil
		}
	}
	p := newProject(ex.Mode, ex.Project.Name, ex.Project.Description)
	p.Example, p.ExampleLang, p.Organism, p.Assay, p.Criteria, p.Query = ex.ID, lang, ex.Project.Organism, ex.Project.Assay, ex.Project.Criteria, ex.Project.Query
	if ex.Project.Source != "" {
		p.Source = ex.Project.Source
	}
	if ex.SiteData != "" {
		if p.Site, err = exampleSite(st, ex, false); err != nil {
			return nil, err
		}
	}
	if ex.Candidates != "" {
		var rows []Candidate
		if err := json.Unmarshal([]byte(ex.file(ex.Candidates)), &rows); err != nil {
			return nil, fmt.Errorf("示例候选数据无效: %w", err)
		}
		mergeCandidates(&p, rows)
	}
	st.Projects = append(st.Projects, p)
	event(st, p.ID, "", "user", "示例项目已创建", p.Name)
	return map[string]any{"projectId": p.ID, "created": true}, nil
}
