package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"testing"
	"unicode"
)

func TestBundledSkillsAreEnglish(t *testing.T) {
	want := map[string]SiteTemplate{
		"research-catalog":   {Label: "Research data catalogue", Layout: "table", Accent: "#237c63", RequiredColumns: []string{"id", "title"}, TitleColumn: "title"},
		"literature-library": {Label: "Literature library", Layout: "cards", Accent: "#466b9c", RequiredColumns: []string{"title", "summary"}, TitleColumn: "title"},
	}
	for _, name := range bundledSkillNames {
		sk, err := bundledSkill(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range sk.Content {
			if unicode.Is(unicode.Han, r) {
				t.Fatalf("%s: SKILL.md still contains Chinese text", name)
			}
		}
		if sk.Template == nil || !reflect.DeepEqual(*sk.Template, want[name]) {
			t.Fatalf("%s template changed: %#v", name, sk.Template)
		}
	}
}

// Workspaces created before the built-ins were English hold the original
// Chinese SKILL.md text; opening them switches unmodified copies to English.
func TestLegacyBundledSkillsUpgradeOnOpen(t *testing.T) {
	s := skillService(t)
	legacy := map[string]Skill{}
	for _, name := range bundledSkillNames {
		raw, err := os.ReadFile("testdata/legacy-skills/" + name + ".md")
		if err != nil {
			t.Fatal(err)
		}
		sk, err := parseSkill(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(sk.Content))
		if hex.EncodeToString(sum[:]) != legacyBundledSkills[name] {
			t.Fatalf("%s: legacy fingerprint does not match the shipped text", name)
		}
		legacy[name] = sk
	}
	edited := legacy["literature-library"]
	edited.Content += "\n\nLocal note."
	if err := s.Store.Update(func(st *State) error {
		for i := range st.Skills {
			old := legacy[st.Skills[i].Name]
			if st.Skills[i].Name == "literature-library" {
				old = edited
			}
			old.ID, old.Enabled = st.Skills[i].ID, st.Skills[i].Name != "research-catalog"
			st.Skills[i] = old
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	before, after := s.Store.Snapshot().Skills, reopened.Snapshot().Skills
	upgraded, kept := after[0], after[1]
	if upgraded.Name != "research-catalog" || upgraded.ID != before[0].ID || upgraded.Enabled || upgraded.Template.Label != "Research data catalogue" || upgraded.Description == legacy["research-catalog"].Description {
		t.Fatalf("unmodified legacy skill should become English and keep ID and switch: %#v", upgraded)
	}
	if kept.Content != edited.Content || kept.ID != before[1].ID {
		t.Fatalf("edited skill must be left alone: %#v", kept)
	}
}
