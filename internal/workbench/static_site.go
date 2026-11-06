package workbench

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxSiteRows = 10000

const ModeStatic = "static"
const ModeCollect = "collect"

func projectMode(p *Project) string {
	if p.Mode == "" {
		return ModeCollect
	}
	return p.Mode
}

// SiteTemplate is a declarative SKILL template; it cannot introduce scripts or new tools.
type SiteTemplate struct {
	Label           string   `json:"label" yaml:"label"`
	Layout          string   `json:"layout" yaml:"layout"`
	Accent          string   `json:"accent" yaml:"accent"`
	RequiredColumns []string `json:"requiredColumns" yaml:"required-columns"`
	TitleColumn     string   `json:"titleColumn" yaml:"title-column"`
}

func validateSiteTemplate(t *SiteTemplate) error {
	if t.Layout != "table" && t.Layout != "cards" {
		return errors.New("SKILL 模板 layout 必须为 table 或 cards")
	}
	if t.Accent == "" {
		t.Accent = "#237c63"
	}
	if !regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(t.Accent) {
		return errors.New("SKILL 模板 accent 必须为六位十六进制颜色")
	}
	if strings.TrimSpace(t.Label) == "" || len(t.Label) > 120 || len(t.RequiredColumns) > 100 {
		return errors.New("SKILL 模板需要名称，必需字段最多 100 个")
	}
	seen := map[string]bool{}
	for _, col := range t.RequiredColumns {
		if strings.TrimSpace(col) != col || col == "" || seen[col] {
			return errors.New("SKILL 模板必需字段不能为空、重复或包含首尾空格")
		}
		seen[col] = true
	}
	if t.Layout == "cards" && t.TitleColumn == "" {
		return errors.New("卡片模板需要 title-column")
	}
	return nil
}

func applySiteTemplate(st *State, skillID string, site *StaticSite) error {
	for _, skill := range st.Skills {
		if skill.ID != skillID || !skill.Enabled || skill.Template == nil {
			continue
		}
		cols := map[string]bool{}
		for _, col := range site.Columns {
			cols[col] = true
		}
		for _, col := range skill.Template.RequiredColumns {
			if !cols[col] {
				return fmt.Errorf("模板缺少必需字段 %s，请检查数据列名", col)
			}
		}
		if skill.Template.TitleColumn != "" && !cols[skill.Template.TitleColumn] {
			return fmt.Errorf("模板缺少标题字段 %s", skill.Template.TitleColumn)
		}
		snapshot := *skill.Template
		snapshot.RequiredColumns = append([]string{}, skill.Template.RequiredColumns...)
		site.Template, site.SkillName = &snapshot, skill.Name
		return nil
	}
	return errors.New("请选择一个已启用的静态网站 SKILL 模板")
}

type StaticSite struct {
	HTML        string        `json:"html,omitempty"`
	Template    *SiteTemplate `json:"template"`
	SkillName   string        `json:"skillName"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Columns     []string      `json:"columns"`
	Rows        [][]string    `json:"rows"`
	BuiltAt     string        `json:"builtAt"`
}

func newStaticSite(title, description, format, data string) (*StaticSite, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 200 || len(description) > 4000 {
		return nil, errors.New("请填写网站标题（最多 200 字节），简介最多 4000 字节")
	}
	if len(data) > 2<<20 || !utf8.ValidString(data) {
		return nil, errors.New("数据必须为 UTF-8 文本且不超过 2 MB")
	}
	data = strings.TrimPrefix(data, "\ufeff")
	site := &StaticSite{Template: &SiteTemplate{Layout: "table", Accent: "#237c63"}, Title: title, Description: description, BuiltAt: now()}
	switch format {
	case "csv", "tsv":
		reader := csv.NewReader(strings.NewReader(data))
		if format == "tsv" {
			reader.Comma = '\t'
		}
		header, err := reader.Read()
		if err != nil {
			return nil, errors.New("无法读取表头")
		}
		site.Columns = header
		for {
			row, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("表格列数或格式错误: %w", err)
			}
			site.Rows = append(site.Rows, row)
			if len(site.Rows) > maxSiteRows {
				return nil, errors.New("最多支持 10000 行数据")
			}
		}
	case "json":
		var records []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(data), &records); err != nil || len(records) == 0 || len(records) > maxSiteRows {
			return nil, errors.New("JSON 必须是包含 1–10000 个对象的数组")
		}
		keys := map[string]bool{}
		for _, record := range records {
			for k := range record {
				keys[k] = true
			}
		}
		if len(keys) > 100 {
			return nil, errors.New("最多支持 100 列数据")
		}
		for k := range keys {
			site.Columns = append(site.Columns, k)
		}
		sort.Strings(site.Columns)
		for _, record := range records {
			if record == nil {
				return nil, errors.New("JSON 中的每一行必须为对象")
			}
			row := make([]string, len(site.Columns))
			for i, k := range site.Columns {
				raw := record[k]
				if len(raw) == 0 || string(raw) == "null" {
					continue
				}
				if json.Unmarshal(raw, &row[i]) != nil {
					row[i] = string(raw)
				}
			}
			site.Rows = append(site.Rows, row)
		}
	default:
		return nil, errors.New("数据格式必须为 csv、tsv 或 json")
	}
	if len(site.Rows) == 0 || len(site.Columns) == 0 || len(site.Columns) > 100 {
		return nil, errors.New("需要 1–10000 行数据和 1–100 列")
	}
	seen := map[string]bool{}
	for i, col := range site.Columns {
		col = strings.TrimSpace(col)
		if col == "" || seen[col] {
			return nil, errors.New("列名不能为空或重复")
		}
		site.Columns[i], seen[col] = col, true
	}
	return site, nil
}

// html/template escapes every imported value. Only the fixed local search script runs.
var staticSiteTemplate = template.Must(template.New("site").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><title>{{.Title}}</title><style>
:root{font-family:system-ui,sans-serif;color:#172c30;background:#f3f7f6}body{max-width:1200px;margin:auto;padding:48px 24px}header{border-top:4px solid {{.Template.Accent}};padding:24px 0}h1{font-size:36px;margin:12px 0}p{line-height:1.7;white-space:pre-wrap}input{box-sizing:border-box;width:100%;padding:14px;border:1px solid #bccdc8;border-radius:8px;font:inherit}section{overflow:auto;background:white;border:1px solid #dce5e2;border-radius:12px;margin-top:24px}table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:14px 18px;border-bottom:1px solid #e9efed;white-space:pre-wrap;overflow-wrap:anywhere;max-width:480px}th{background:#eaf3ef}small,footer{color:#60736c}footer{margin-top:24px;font-size:13px}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:20px;background:transparent;border:0}.record-card{background:white;border:1px solid #dce5e2;border-top:3px solid {{.Template.Accent}};border-radius:12px;padding:24px}.record-card h2{margin:0 0 20px;font-size:22px}.record-card dt{font-size:12px;color:#60736c;margin-top:14px}.record-card dd{margin:4px 0;white-space:pre-wrap;overflow-wrap:anywhere}[hidden]{display:none!important}
</style></head><body><header><small>DATA COLLECTION · {{len .Rows}} records</small><h1>{{.Title}}</h1><p>{{.Description}}</p></header><label for="search">搜索数据 / Search records</label><input id="search" type="search" placeholder="输入关键词 / Search…"><p id="count" aria-live="polite">{{len .Rows}} records</p>{{if eq .Template.Layout "cards"}}<section class="cards">{{range .Rows}}{{$row := .}}<article class="record-card" data-record><h2>{{index . $.TitleIndex}}</h2><dl>{{range $i, $col := $.Columns}}{{if ne $i $.TitleIndex}}<dt>{{$col}}</dt><dd>{{index $row $i}}</dd>{{end}}{{end}}</dl></article>{{end}}</section>{{else}}<section><table><thead><tr>{{range .Columns}}<th scope="col">{{.}}</th>{{end}}</tr></thead><tbody>{{range .Rows}}<tr data-record>{{range .}}<td>{{.}}</td>{{end}}</tr>{{end}}</tbody></table></section>{{end}}<footer>Built with DataBaseAgent · {{.BuiltAt}}</footer><script>
const rows=Array.from(document.querySelectorAll('[data-record]'));const searchable=rows.map(row=>row.textContent.toLowerCase());document.getElementById('search').addEventListener('input',function(){const query=this.value.toLowerCase();let visible=0;rows.forEach((row,index)=>{row.hidden=!searchable[index].includes(query);if(!row.hidden)visible++});document.getElementById('count').textContent=visible+' / '+rows.length+' records';});
</script></body></html>`))

func renderStaticSite(site *StaticSite) (string, error) {
	if site.HTML != "" {
		return renderCustomSite(site)
	}
	var out bytes.Buffer
	titleIndex := 0
	for i, col := range site.Columns {
		if col == site.Template.TitleColumn {
			titleIndex = i
		}
	}
	err := staticSiteTemplate.Execute(&out, struct {
		*StaticSite
		TitleIndex int
	}{site, titleIndex})
	return out.String(), err
}

func registerSiteTools() {
	registerCustomSiteTools()
	tools["build_static_site"] = toolSpec{Desc: "仅已有数据（static）模式：将用户提供的真实 CSV/TSV/JSON 数据保存为可预览、下载的独立静态网站，带搜索表格。不需要服务器、数据库或模型生成代码。不自动部署。", Action: "buildStaticSite", Params: obj(map[string]any{"skillId": strParam("已启用 SKILL 模板的 ID，先 read_skill 获取"), "name": strParam("网站标题"), "description": strParam("网站简介"), "format": enumParam("csv", "tsv", "json"), "data": strParam("完整数据文本，最多 2 MB、10000 行、100 列；不得编造数据")}, "skillId", "name", "format", "data")}
	tools["get_static_site"] = toolSpec{Desc: "读取已生成静态网站的标题、列名、行数和构建时间。", Run: func(_ *State, p *Project, _ map[string]any) any {
		if p.Site == nil {
			return map[string]string{"status": "not_built"}
		}
		return map[string]any{"skillName": p.Site.SkillName, "title": p.Site.Title, "description": p.Site.Description, "columns": p.Site.Columns, "rows": len(p.Site.Rows), "builtAt": p.Site.BuiltAt, "layout": p.Site.Template.Layout, "htmlBytes": len(p.Site.HTML)}
	}}
}
