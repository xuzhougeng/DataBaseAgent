package workbench

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zalando/go-keyring"
)

func TestModelKeyLifecycle(t *testing.T) {
	keyring.MockInit()
	s, pid := fixture(t)
	t.Setenv("DBA_TEST_KEY", "legacy-token")
	profile := ModelProfile{Name: "test", Protocol: "openai", BaseURL: "https://model.example/v1", Model: "test-model", APIKeyEnv: "DBA_TEST_KEY"}
	action(t, s, "saveModel", Request{Model: profile})
	profile = s.Store.Snapshot().Settings.Models[0]
	agent := Agent{ModelID: profile.ID}
	checkKey := func(store *Store, want string) ModelProfile {
		t.Helper()
		m, key, err := store.modelWithKey(agent)
		if key != want || err != nil {
			t.Fatalf("wrong model credential, error: %v", err)
		}
		return m
	}
	checkKey(s.Store, "legacy-token")

	handler := Handler(s, fstest.MapFS{})
	save := func(key *string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(Request{Model: profile, APIKey: key})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/saveModel", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	const directKey = "test-direct-api-key"
	// Force the workspace rename to fail after the new key was stored.
	path := filepath.Join(s.Store.Root(), "workbench.json")
	if err := os.Rename(path, path+".bak"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if res := save(new(directKey)); res.Code != 400 {
		t.Fatalf("expected workspace write failure, got %d", res.Code)
	}
	checkKey(s.Store, "legacy-token")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".bak", path); err != nil {
		t.Fatal(err)
	}

	if res := save(new(directKey)); res.Code != 200 || strings.Contains(res.Body.String(), directKey) {
		t.Fatalf("could not save key safely, got %d", res.Code)
	}
	reopened, err := Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	profile = checkKey(reopened, directKey)
	if profile.APIKeyEnv != "" {
		t.Fatal("direct key did not replace the environment reference")
	}
	disk, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(disk), directKey) {
		t.Fatal("workspace file should contain only the key reference")
	}
	state := httptest.NewRecorder()
	handler.ServeHTTP(state, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	var status struct {
		KeySaved map[string]bool `json:"keySaved"`
		Ready    map[string]bool `json:"ready"`
	}
	if err := json.Unmarshal(state.Body.Bytes(), &status); err != nil || !status.KeySaved[profile.ID] || !status.Ready["assistant"] {
		t.Fatal("state API did not show the configured key")
	}
	if strings.Contains(state.Body.String(), directKey) || strings.Contains(state.Body.String(), profile.APIKeyRef) {
		t.Fatal("state API exposed a credential or its reference")
	}

	profile.Name = "renamed"
	if res := save(nil); res.Code != 200 {
		t.Fatalf("metadata edit failed, got %d", res.Code)
	}
	checkKey(s.Store, directKey)
	reply := `{"choices":[{"message":{"content":"ok"}}]}`
	seen := fakeModel(t, reply, reply)
	action(t, s, "message", Request{ProjectID: pid, Message: "hello"})
	if len(*seen) != 1 || (*seen)[0]["_auth"] != "Bearer "+directKey {
		t.Fatal("model request did not use the saved credential")
	}

	checkRemoved := func(ref string) {
		t.Helper()
		if _, err := keyring.Get(modelKeyService, s.Store.modelKeyAccount(ref)); !errors.Is(err, keyring.ErrNotFound) {
			t.Fatalf("obsolete key was not removed, error: %v", err)
		}
	}
	if res := save(new("replacement-key")); res.Code != 200 {
		t.Fatalf("key replacement failed, got %d", res.Code)
	}
	checkRemoved(profile.APIKeyRef)
	profile = checkKey(s.Store, "replacement-key")
	if res := save(new("")); res.Code != 200 {
		t.Fatalf("key removal failed, got %d", res.Code)
	}
	checkRemoved(profile.APIKeyRef)
	checkKey(s.Store, "")

	if res := save(new(directKey)); res.Code != 200 {
		t.Fatalf("key re-entry failed, got %d", res.Code)
	}
	profile = checkKey(s.Store, directKey)
	action(t, s, "deleteModel", Request{ModelID: profile.ID})
	checkRemoved(profile.APIKeyRef)
}
