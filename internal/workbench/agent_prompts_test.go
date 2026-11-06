package workbench

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestEnglishAgentPromptsHaveNoChinese(t *testing.T) {
	verbs := regexp.MustCompile(`%[a-z]`)
	zh, en := reflect.ValueOf(agentTexts["zh"]), reflect.ValueOf(agentTexts["en"])
	for i := 0; i < zh.NumField(); i++ {
		if zh.Field(i).Kind() != reflect.String {
			continue
		}
		name, z, e := zh.Type().Field(i).Name, zh.Field(i).String(), en.Field(i).String()
		if e == "" || hasHan(e) || strings.Join(verbs.FindAllString(z, -1), "") != strings.Join(verbs.FindAllString(e, -1), "") {
			t.Fatalf("%s: English text missing, Chinese, or with different format verbs", name)
		}
	}

	st := State{Skills: defaultSkills(), Settings: Settings{Language: "en"}}
	projects := []*Project{nil, {ID: "p1", Name: "Atlas", Mode: ModeStatic}, {ID: "p2", Name: "Atlas", Mode: ModeCollect}}
	for _, a := range withPresets(nil, "en") {
		for _, p := range projects {
			prompt := systemPrompt(a, p, "en") + skillPrompt(&st) + promptText("en").subagent + promptText("en").review
			if hasHan(prompt) || !strings.Contains(prompt, "Reply in English.") {
				t.Fatalf("%s: English prompt has Chinese text or no reply-language rule", a.ID)
			}
		}
	}
	turns := conversationTurns([]Message{{Kind: "delegation", Role: "assistant", Agent: "scout", Task: "find data", Content: "done"}}, "en")
	if len(turns) != 1 || !strings.HasPrefix(turns[0].Text, "[Subagent scout, task: find data, failed: false]") {
		t.Fatalf("delegation turn: %#v", turns)
	}
}

// Default instructions follow the UI language; anything the user wrote stays as written.
func TestPresetInstructionsFollowLanguage(t *testing.T) {
	s := skillService(t)
	system := func(id string) string {
		t.Helper()
		for _, a := range s.Store.Snapshot().Settings.Agents {
			if a.ID == id {
				return a.System
			}
		}
		t.Fatalf("agent %s missing", id)
		return ""
	}
	if system("assistant") != presetSystem("assistant", "zh") {
		t.Fatal("new workspaces start with the Chinese defaults")
	}
	action(t, s, "saveAgent", Request{Agent: Agent{ID: "scout", System: "Custom scout rules.", MaxSteps: 12}})
	action(t, s, "setLanguage", Request{Lang: "en"})
	if system("assistant") != presetSystem("assistant", "en") || system("scout") != "Custom scout rules." {
		t.Fatalf("English: assistant %q, scout %q", system("assistant"), system("scout"))
	}
	action(t, s, "resetAgent", Request{AgentID: "scout"})
	if system("scout") != presetSystem("scout", "en") {
		t.Fatal("restoring defaults in English gives the English instructions")
	}

	// Saving the unchanged English default does not turn it into custom text.
	action(t, s, "saveAgent", Request{Agent: Agent{ID: "reviewer", System: presetSystem("reviewer", "en"), MaxSteps: 20}})
	action(t, s, "setLanguage", Request{Lang: "zh"})
	if system("reviewer") != presetSystem("reviewer", "zh") || system("assistant") != presetSystem("assistant", "zh") {
		t.Fatal("switching back to Chinese restores the Chinese defaults")
	}

	// The language persists, and an older default upgrades to the current language on open.
	action(t, s, "setLanguage", Request{Lang: "en"})
	if err := s.Store.Update(func(st *State) error {
		for i := range st.Settings.Agents {
			if st.Settings.Agents[i].ID == "operator" {
				st.Settings.Agents[i].System, st.Settings.Agents[i].MaxSteps = legacyAgentSystems["operator"], legacyAgentSteps["operator"]
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	st := reopened.Snapshot()
	for _, a := range st.Settings.Agents {
		if a.ID == "operator" && (a.System != presetSystem("operator", "en") || a.MaxSteps != 12) {
			t.Fatalf("legacy operator: %#v", a)
		}
	}
	if st.Settings.Language != "en" {
		t.Fatalf("language not kept: %q", st.Settings.Language)
	}
}
