package workbench

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 90 * time.Second}
var htmlTag = regexp.MustCompile(`<[^>]*>`)

func getJSON(ctx context.Context, raw string, out any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return e
	}
	req.Header.Set("User-Agent", "DataBaseAgent/0.1 (local research workbench)")
	res, e := httpClient.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("检索服务返回 HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 20<<20)).Decode(out)
}

func (s *Service) Search(ctx context.Context, r Request) (any, error) {
	s.projectMu.RLock()
	defer s.projectMu.RUnlock()
	if !s.searchMu.TryLock() {
		return nil, errors.New("已有检索进行中，请等待完成")
	}
	defer s.searchMu.Unlock()
	pid := r.ProjectID
	e := s.Store.Read(func(st *State) error { _, e := projectByID(st, pid); return e })
	if e != nil {
		return nil, e
	}
	query := strings.TrimSpace(r.Query)
	if query == "" {
		return nil, errors.New("请先填写检索表达式")
	}
	limit := r.Limit
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	var rows []Candidate
	total := 0
	switch r.Source {
	case "Europe PMC":
		rows, total, e = searchEuropePMC(ctx, query, limit)
	case "PubMed", "GEO":
		rows, total, e = searchEntrez(ctx, r.Source, query, limit)
	default:
		return nil, errors.New("当前支持 Europe PMC、PubMed 和 GEO；其他来源可导入元数据")
	}
	if e != nil {
		_ = s.Store.Update(func(st *State) error {
			event(st, pid, "", "error", "检索未完成", r.Source+" · "+e.Error())
			return nil
		})
		return nil, e
	}
	record := SearchRecord{ID: id("search"), Query: query, Source: r.Source, Total: total, Fetched: len(rows), At: now()}
	e = s.Store.Update(func(st *State) error {
		cur, err := projectByID(st, pid)
		if err != nil {
			return err
		}
		record.Added = mergeCandidates(cur, rows)
		cur.Query = query
		cur.Source = r.Source
		cur.Searches = append(cur.Searches, record)
		cur.UpdatedAt = now()
		event(st, pid, "", "search", "检索完成", fmt.Sprintf("%s · 命中 %d，获取 %d，新增 %d（上限 %d）", r.Source, total, len(rows), record.Added, limit))
		return nil
	})
	return record, e
}

func searchEuropePMC(ctx context.Context, query string, limit int) ([]Candidate, int, error) {
	rows := []Candidate{}
	cursor := "*"
	total := 0
	for len(rows) < limit {
		pageSize := min(100, limit-len(rows))
		v := url.Values{"query": {query}, "format": {"json"}, "resultType": {"core"}, "pageSize": {strconv.Itoa(pageSize)}, "cursorMark": {cursor}}
		var body struct {
			HitCount int    `json:"hitCount"`
			Next     string `json:"nextCursorMark"`
			Results  struct {
				Rows []struct {
					ID       string `json:"id"`
					Source   string `json:"source"`
					Title    string `json:"title"`
					Abstract string `json:"abstractText"`
					DOI      string `json:"doi"`
					PMID     string `json:"pmid"`
					Year     string `json:"pubYear"`
				} `json:"result"`
			} `json:"resultList"`
		}
		if e := getJSON(ctx, "https://www.ebi.ac.uk/europepmc/webservices/rest/search?"+v.Encode(), &body); e != nil {
			return nil, 0, e
		}
		total = body.HitCount
		for _, a := range body.Results.Rows {
			link := "https://europepmc.org/article/" + url.PathEscape(a.Source) + "/" + url.PathEscape(a.ID)
			acc := a.ID
			if a.PMID != "" {
				acc = "PMID:" + a.PMID
			}
			rows = append(rows, Candidate{Title: htmlTag.ReplaceAllString(a.Title, ""), Abstract: htmlTag.ReplaceAllString(a.Abstract, ""), Source: "Europe PMC", DOI: a.DOI, Accession: acc, Year: a.Year, URL: link})
		}
		if len(body.Results.Rows) == 0 || body.Next == "" || body.Next == cursor {
			break
		}
		cursor = body.Next
	}
	return rows, total, nil
}

func searchEntrez(ctx context.Context, source, query string, limit int) ([]Candidate, int, error) {
	db := "pubmed"
	if source == "GEO" {
		db = "gds"
	}
	base := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/"
	v := url.Values{"db": {db}, "term": {query}, "retmode": {"json"}, "retmax": {strconv.Itoa(limit)}, "tool": {"DataBaseAgent"}}
	var body struct {
		Result struct {
			Count string   `json:"count"`
			IDs   []string `json:"idlist"`
			Error string   `json:"error"`
		} `json:"esearchresult"`
		Error string `json:"error"`
	}
	if e := getJSON(ctx, base+"esearch.fcgi?"+v.Encode(), &body); e != nil {
		return nil, 0, e
	}
	if body.Error != "" || body.Result.Error != "" {
		return nil, 0, errors.New("NCBI 无法解析检索请求")
	}
	total, _ := strconv.Atoi(body.Result.Count)
	rows := []Candidate{}
	for offset := 0; offset < len(body.Result.IDs); offset += 100 {
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-entrezDelay():
		}
		ids := body.Result.IDs[offset:min(offset+100, len(body.Result.IDs))]
		q := url.Values{"db": {db}, "id": {strings.Join(ids, ",")}, "tool": {"DataBaseAgent"}}
		if source == "GEO" {
			q.Set("retmode", "json")
			var summaries struct {
				Result map[string]json.RawMessage `json:"result"`
			}
			if e := getJSON(ctx, base+"esummary.fcgi?"+q.Encode(), &summaries); e != nil {
				return nil, 0, e
			}
			for _, uid := range ids {
				var a struct {
					Title     string `json:"title"`
					Summary   string `json:"summary"`
					Accession string `json:"accession"`
					Taxon     string `json:"taxon"`
				}
				if e := json.Unmarshal(summaries.Result[uid], &a); e != nil {
					return nil, 0, e
				}
				rows = append(rows, Candidate{Title: a.Title, Abstract: a.Summary, Accession: a.Accession, Organism: a.Taxon, Source: "GEO", URL: "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=" + url.QueryEscape(a.Accession)})
			}
		} else {
			q.Set("retmode", "xml")
			req, _ := http.NewRequestWithContext(ctx, "GET", base+"efetch.fcgi?"+q.Encode(), nil)
			res, e := httpClient.Do(req)
			if e != nil {
				return nil, 0, e
			}
			var data struct {
				Articles []struct {
					Citation struct {
						PMID    string `xml:"PMID"`
						Article struct {
							Title    string `xml:"ArticleTitle"`
							Abstract struct {
								Texts []string `xml:"AbstractText"`
							} `xml:"Abstract"`
							Journal struct {
								Issue struct {
									Date struct {
										Year string `xml:"Year"`
									} `xml:"PubDate"`
								} `xml:"JournalIssue"`
							} `xml:"Journal"`
						} `xml:"Article"`
					} `xml:"MedlineCitation"`
					Data struct {
						IDs []struct {
							Type  string `xml:"IdType,attr"`
							Value string `xml:",chardata"`
						} `xml:"ArticleIdList>ArticleId"`
					} `xml:"PubmedData"`
				} `xml:"PubmedArticle"`
			}
			if res.StatusCode != 200 {
				res.Body.Close()
				return nil, 0, fmt.Errorf("NCBI HTTP %d", res.StatusCode)
			}
			e = xml.NewDecoder(io.LimitReader(res.Body, 20<<20)).Decode(&data)
			res.Body.Close()
			if e != nil {
				return nil, 0, e
			}
			for _, a := range data.Articles {
				doi := ""
				for _, v := range a.Data.IDs {
					if v.Type == "doi" {
						doi = v.Value
					}
				}
				rows = append(rows, Candidate{Title: a.Citation.Article.Title, Abstract: strings.Join(a.Citation.Article.Abstract.Texts, "\n"), Accession: "PMID:" + a.Citation.PMID, Source: "PubMed", DOI: doi, Year: a.Citation.Article.Journal.Issue.Date.Year, URL: "https://pubmed.ncbi.nlm.nih.gov/" + a.Citation.PMID + "/"})
			}
		}
	}
	return rows, total, nil
}

func mergeCandidates(p *Project, rows []Candidate) int {
	keys := map[string]bool{}
	for _, c := range p.Candidates {
		for _, k := range candidateKeys(c) {
			keys[k] = true
		}
	}
	added := 0
	for _, c := range rows {
		if strings.TrimSpace(c.Title) == "" {
			continue
		}
		duplicate := false
		for _, k := range candidateKeys(c) {
			if keys[k] {
				duplicate = true
			}
		}
		if duplicate {
			continue
		}
		c.ID = id("candidate")
		c.Decision = "pending"
		c.Suggestion = ""
		c.ReviewedAt = ""
		c.ReviewNote = ""
		c.Evidence = ""
		c.Reason = ""
		c.Confidence = ""
		p.Candidates = append(p.Candidates, c)
		for _, k := range candidateKeys(c) {
			keys[k] = true
		}
		added++
	}
	return added
}
func candidateKeys(c Candidate) []string {
	keys := []string{"title:" + strings.ToLower(strings.Join(strings.Fields(c.Title), " "))}
	if c.DOI != "" {
		keys = append(keys, "doi:"+strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(c.DOI), "https://doi.org/"), "doi:"))
	}
	if c.Accession != "" {
		keys = append(keys, "acc:"+strings.ToLower(c.Accession))
	}
	return keys
}
