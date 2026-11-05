package workbench

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
)

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
		case r.Method == "GET" && r.URL.Path == "/api/state":
			state := s.Store.Snapshot()
			write(map[string]any{"state": state, "root": s.Store.Root(), "modelReady": state.Settings.ModelURL != "" && state.Settings.Model != "" && os.Getenv(state.Settings.APIKeyEnv) != ""})
		case r.Method == "GET" && r.URL.Path == "/api/artifact":
			text, e := s.PreviewArtifact(r.URL.Query().Get("id"))
			if e != nil {
				w.WriteHeader(400)
				write(map[string]string{"error": e.Error()})
				return
			}
			write(map[string]string{"text": text})
		case r.Method == "GET" && r.URL.Path == "/api/manifest":
			state := s.Store.Snapshot()
			pid := r.URL.Query().Get("projectId")
			p, e := projectByID(&state, pid)
			if e != nil {
				w.WriteHeader(404)
				write(map[string]string{"error": e.Error()})
				return
			}
			jobs := []Job{}
			artifacts := []Artifact{}
			for _, j := range state.Jobs {
				if j.ProjectID == pid {
					j.Server.KeyPath = ""
					jobs = append(jobs, j)
				}
			}
			for _, a := range state.Artifacts {
				if a.ProjectID == pid {
					artifacts = append(artifacts, a)
				}
			}
			w.Header().Set("Content-Disposition", `attachment; filename="dba_manifest.json"`)
			write(map[string]any{"schema": SchemaVersion, "project": p, "jobs": jobs, "artifacts": artifacts, "exportedAt": now()})
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
