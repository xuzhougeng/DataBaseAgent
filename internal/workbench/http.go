package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// LoopbackOnly wraps the handler of a server listening on a local TCP port.
// Any web page can reach that port through DNS rebinding (a hostile name that
// resolves to 127.0.0.1); the browser then sends matching Origin and Host
// headers, so only the Host name itself shows the request is not local.
func LoopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if !strings.EqualFold(host, "localhost") && host != "127.0.0.1" && host != "::1" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			http.Error(w, `{"error":"仅接受本机地址访问"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Handler(s *Service, assets fs.FS) http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, `{"error":"跨来源请求已拒绝"}`, 403)
				return
			}
		}
		write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/site":
			state := s.Store.Snapshot()
			p, err := projectByID(&state, r.URL.Query().Get("projectId"))
			if err != nil || p.Site == nil {
				w.WriteHeader(404)
				write(map[string]string{"error": "静态网站尚未生成"})
				return
			}
			writeSite(w, r, p.Site)
		case r.Method == "GET" && r.URL.Path == "/api/examples":
			list, err := examplesFor(r.URL.Query().Get("lang"))
			if err != nil {
				w.WriteHeader(500)
				write(map[string]string{"error": err.Error()})
				return
			}
			write(map[string]any{"examples": list})
		case r.Method == "GET" && r.URL.Path == "/api/example-site":
			ex, err := exampleByID(r.URL.Query().Get("id"), r.URL.Query().Get("lang"))
			if err != nil || ex.SiteData == "" {
				w.WriteHeader(404)
				write(map[string]string{"error": "示例不存在"})
				return
			}
			state := s.Store.Snapshot()
			site, err := exampleSite(&state, ex, true)
			if err != nil {
				w.WriteHeader(500)
				write(map[string]string{"error": err.Error()})
				return
			}
			writeSite(w, r, site)
		case r.Method == "GET" && r.URL.Path == "/api/state":
			body, tag := s.stateResponse(r.Header.Get("If-None-Match"))
			w.Header().Set("ETag", tag)
			if body == nil {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			_, _ = w.Write(body)
		case r.Method == "GET" && r.URL.Path == "/api/artifact":
			text, e := s.PreviewArtifact(r.URL.Query().Get("id"))
			if e != nil {
				w.WriteHeader(400)
				write(map[string]string{"error": e.Error()})
				return
			}
			write(map[string]string{"text": text})
		case r.Method == "POST":
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				w.WriteHeader(415)
				write(map[string]string{"error": "需要 JSON 请求"})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
			var raw json.RawMessage
			if e := json.NewDecoder(r.Body).Decode(&raw); e != nil {
				w.WriteHeader(400)
				write(map[string]string{"error": "请求无效或内容过大"})
				return
			}
			result, e := s.Dispatch(r.Context(), strings.TrimPrefix(r.URL.Path, "/api/"), raw)
			if e != nil {
				w.WriteHeader(400)
				write(map[string]string{"error": e.Error()})
				return
			}
			write(map[string]any{"result": result})
		default:
			w.WriteHeader(404)
			write(map[string]string{"error": "接口不存在"})
		}
	})
}

// writeSite serves a generated website as an inert, sandboxed document.
func writeSite(w http.ResponseWriter, r *http.Request, site *StaticSite) {
	html, err := renderStaticSite(site)
	if err != nil {
		w.WriteHeader(500)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; "+siteCSP)
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="index.html"`)
	}
	_, _ = w.Write([]byte(html))
}

// stateResponse builds the polling payload under one read lock. The tag combines
// the store version with a digest of the transient run state, so an unchanged
// workspace answers a poll with 304 and no serialization. Credential references
// are blanked on a shallow copy; the live state is never modified.
// Transient maps are collected first: their locks are taken before the store's elsewhere.
func (s *Service) stateResponse(ifNoneMatch string) (body []byte, tag string) {
	live := map[string]any{"root": s.Store.Root(), "agentRuns": s.AgentRuns(), "approvals": s.Approvals(), "chatErrors": s.ChatErrors()}
	_ = s.Store.Read(func(st *State) error {
		envSet, keySaved, ready := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, m := range st.Settings.Models {
			envSet[m.ID] = m.APIKeyEnv == "" || os.Getenv(m.APIKeyEnv) != ""
			keySaved[m.ID] = m.APIKeyRef != ""
		}
		for _, a := range st.Settings.Agents {
			m, e := modelFor(st, a)
			ready[a.ID] = e == nil && modelReady(m)
		}
		mcpStates, mcpAuth := map[string]string{}, map[string]string{}
		for _, v := range st.Settings.MCPServers {
			mcpStates[v.ID] = mcpStatus(v)
			switch {
			case mcpOAuth(v):
				mcpAuth[v.ID] = "oauth"
			case v.TokenRef != "":
				mcpAuth[v.ID] = "token"
			}
		}
		live["envSet"], live["keySaved"], live["ready"], live["mcpStates"], live["mcpAuth"] = envSet, keySaved, ready, mcpStates, mcpAuth
		meta, _ := json.Marshal(live)
		digest := sha256.Sum256(meta)
		tag = `"` + s.Store.epoch + "-" + strconv.FormatUint(s.Store.version, 10) + "-" + hex.EncodeToString(digest[:8]) + `"`
		if tag == ifNoneMatch {
			return nil
		}
		view := *st
		view.Settings.Models = append([]ModelProfile{}, st.Settings.Models...)
		for i := range view.Settings.Models {
			view.Settings.Models[i].APIKeyRef = ""
		}
		view.Settings.MCPServers = append([]MCPServer{}, st.Settings.MCPServers...)
		for i := range view.Settings.MCPServers {
			view.Settings.MCPServers[i].TokenRef = ""
		}
		live["state"] = view
		body, _ = json.Marshal(live)
		return nil
	})
	return body, tag
}
