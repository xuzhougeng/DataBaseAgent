package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

const siteCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; font-src data:; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-src 'none'; object-src 'none'"

func importSiteData(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	p, err := projectByID(st, r.ProjectID)
	if err != nil {
		return nil, err
	}
	if projectMode(p) != ModeStatic {
		return nil, errors.New("收集模式目前止于数据收集与分析，不支持建站")
	}
	input, err := newStaticSite(r.Name, r.Description, r.Format, r.Data)
	if err != nil {
		return nil, err
	}
	p.SiteInput, p.UpdatedAt = input, now()
	event(st, p.ID, "", "user", "建站数据已保存", input.Title)
	return map[string]any{"projectId": p.ID, "rows": len(input.Rows), "columns": input.Columns}, nil
}

func siteInput(p *Project) *StaticSite {
	if p.SiteInput != nil {
		return p.SiteInput
	}
	return p.Site
}

func inspectSiteData(p *Project, offset, limit int) any {
	data := siteInput(p)
	if data == nil {
		return map[string]string{"error": "请先在静态网站页导入并保存数据"}
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(data.Rows) {
		offset = len(data.Rows)
	}
	end := min(offset+limit, len(data.Rows))
	stats := make([]map[string]any, 0, len(data.Columns))
	for c, name := range data.Columns {
		missing, numeric := 0, 0
		lo, hi := math.Inf(1), math.Inf(-1)
		counts := map[string]int{}
		for _, row := range data.Rows {
			v := row[c]
			if strings.TrimSpace(v) == "" {
				missing++
				continue
			}
			counts[v]++
			n, err := strconv.ParseFloat(v, 64)
			if err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
				numeric++
				lo = math.Min(lo, n)
				hi = math.Max(hi, n)
			}
		}
		stat := map[string]any{"name": name, "missing": missing, "distinct": len(counts), "numericCount": numeric}
		if numeric > 0 {
			stat["min"], stat["max"] = lo, hi
		}
		// Category counts stay bounded; barcodes and continuous values never fill model context.
		if len(counts) <= 30 {
			stat["counts"] = counts
		}
		stats = append(stats, stat)
	}
	return map[string]any{"title": data.Title, "description": data.Description, "rows": len(data.Rows), "columns": data.Columns, "statistics": stats, "offset": offset, "sampleRows": data.Rows[offset:end], "runtime": "window.DBA_DATA = {title, description, columns: string[], rows: string[][]}; all imported rows are injected automatically before your scripts"}
}

func buildCustomSite(_ *Service, _ context.Context, st *State, r Request) (any, error) {
	p, err := projectByID(st, r.ProjectID)
	if err != nil {
		return nil, err
	}
	if projectMode(p) != ModeStatic {
		return nil, errors.New("收集模式目前止于数据收集与分析，不支持建站")
	}
	input := siteInput(p)
	if input == nil {
		return nil, errors.New("请先在静态网站页导入并保存数据")
	}
	var skillName string
	for _, skill := range st.Skills {
		if skill.ID == r.SkillID && skill.Enabled {
			skillName = skill.Name
			break
		}
	}
	if skillName == "" {
		return nil, errors.New("请选择一个已启用的 SKILL")
	}
	title := strings.TrimSpace(r.Name)
	if title == "" || len(title) > 200 || len(r.Description) > 4000 {
		return nil, errors.New("请填写网站标题（最多 200 字节），简介最多 4000 字节")
	}
	site := &StaticSite{Title: title, Description: r.Description, Columns: append([]string{}, input.Columns...), Rows: input.Rows, SkillName: skillName, BuiltAt: now(), HTML: r.HTML, Template: &SiteTemplate{Label: "Agent-designed website", Layout: "custom", Accent: "#237c63"}}
	if _, err := renderCustomSite(site); err != nil {
		return nil, err
	}
	p.Site, p.UpdatedAt = site, now()
	event(st, p.ID, "", "user", "静态网站已生成", site.Title)
	return map[string]any{"projectId": p.ID, "rows": len(site.Rows), "builtAt": site.BuiltAt, "previewURL": "/api/site?projectId=" + p.ID, "downloadURL": "/api/site?projectId=" + p.ID + "&download=1", "validation": "HTML parsed; data injected; external resource attributes rejected; CSP applied. Visual behavior still requires browser testing."}, nil
}

// Render a model-authored document with a policy and real data before any author
// script. Preview has an additional HTTP sandbox; exported HTML retains the CSP.
// This is not a JS correctness checker: generated UI still needs visual review.
func renderCustomSite(site *StaticSite) (string, error) {
	if strings.TrimSpace(site.HTML) == "" || len(site.HTML) > 512<<10 || !utf8.ValidString(site.HTML) {
		return "", errors.New("HTML 必须是非空 UTF-8 文本且不超过 512 KB")
	}
	doc, err := html.Parse(strings.NewReader(site.HTML))
	if err != nil {
		return "", err
	}
	var head *html.Node
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "head":
				head = n
			case "base", "iframe", "frame", "frameset", "object", "embed", "form":
				return fmt.Errorf("静态网站不支持 %s 元素", n.Data)
			}
			for _, a := range n.Attr {
				key, val := strings.ToLower(a.Key), strings.TrimSpace(a.Val)
				if n.Data == "meta" && key == "http-equiv" {
					return errors.New("请勿添加 http-equiv；DBA 自动设置 CSP")
				}
				if key == "srcdoc" || key == "srcset" || key == "action" || key == "formaction" || key == "ping" {
					return fmt.Errorf("静态网站不支持 %s 属性", key)
				}
				if key == "src" || key == "href" || key == "data" || key == "poster" {
					// Source citations may be ordinary HTTPS links. No fetched assets.
					allowed := val == "" || strings.HasPrefix(val, "#") || n.Data == "img" && strings.HasPrefix(val, "data:image/") || n.Data == "a" && (strings.HasPrefix(val, "https://") || strings.HasPrefix(val, "http://"))
					if !allowed {
						return fmt.Errorf("静态网站需内嵌资源，不允许 %s=%q", key, val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(doc); err != nil {
		return "", err
	}
	if head == nil {
		return "", errors.New("HTML 缺少 head")
	}
	payload, err := json.Marshal(map[string]any{"title": site.Title, "description": site.Description, "columns": site.Columns, "rows": site.Rows})
	if err != nil {
		return "", err
	}
	policy := &html.Node{Type: html.ElementNode, Data: "meta", Attr: []html.Attribute{{Key: "http-equiv", Val: "Content-Security-Policy"}, {Key: "content", Val: siteCSP}}}
	data := &html.Node{Type: html.ElementNode, Data: "script", Attr: []html.Attribute{{Key: "id", Val: "dba-data"}}}
	data.AppendChild(&html.Node{Type: html.TextNode, Data: "window.DBA_DATA = " + string(payload) + ";"})
	first := head.FirstChild
	head.InsertBefore(policy, first)
	head.InsertBefore(data, first)
	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return "", err
	}
	return out.String(), nil
}

func registerCustomSiteTools() {
	tools["inspect_site_data"] = toolSpec{Desc: "查看已导入的真实建站数据：字段、总行数、缺失数、数值范围、分类计数及少量样例。完整数据由 build_custom_site 自动注入网站，不必读取或复制整表。", Params: obj(map[string]any{"offset": intParam(0, maxSiteRows), "limit": intParam(1, 100)}), Run: func(_ *State, p *Project, args map[string]any) any {
		offset, _ := args["offset"].(float64)
		limit, _ := args["limit"].(float64)
		return inspectSiteData(p, int(offset), int(limit))
	}}
	tools["build_custom_site"] = toolSpec{Desc: "仅 static 模式：按数据与 SKILL 自主编写完整 HTML/CSS/JavaScript 网站并保存预览。先 inspect_site_data 和 read_skill。HTML 内从 window.DBA_DATA 读取真实全量 columns/rows（字符串），无需复制数据。所有代码、样式内嵌，无 CDN、fetch、外链资源、iframe 或表单。不自动部署。生成后须实际预览核对交互。", Action: "buildCustomSite", Params: obj(map[string]any{"skillId": strParam("已读取且启用的 SKILL ID"), "name": strParam("网站标题"), "description": strParam("网站简介与来源"), "html": strParam("完整 HTML 文档，最多 512 KB；自选布局和交互，用 window.DBA_DATA 访问全量数据。不要添加 CSP/http-equiv，DBA 自动注入。")}, "skillId", "name", "description", "html")}
}
