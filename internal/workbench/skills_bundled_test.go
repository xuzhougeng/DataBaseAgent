package workbench

import (
	"crypto/sha256"
	"encoding/hex"
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
		if name == "single-cell-explorer" {
			if sk.Template != nil {
				t.Fatal("authoring skill must not impose a fixed template")
			}
			continue
		}
		if sk.Template == nil || !reflect.DeepEqual(*sk.Template, want[name]) {
			t.Fatalf("%s template changed: %#v", name, sk.Template)
		}
	}
}

// Synthetic legacy content exercises migration without retaining obsolete SKILL
// documents. Production fingerprints remain unchanged; this test substitutes
// known hashes for its generated inputs and restores them before returning.
func TestLegacyBundledSkillsUpgradeOnOpen(t *testing.T) {
	s := skillService(t)
	originalFingerprints := legacyBundledSkills
	legacyBundledSkills = map[string]string{}
	t.Cleanup(func() { legacyBundledSkills = originalFingerprints })
	legacy := map[string]Skill{}
	for _, name := range []string{"research-catalog", "literature-library"} {
		sk, err := parseSkill("---\nname: " + name + "\ndescription: Legacy test skill\n---\n\nOriginal test instructions.")
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(sk.Content))
		legacyBundledSkills[name] = hex.EncodeToString(sum[:])
		legacy[name] = sk
	}
	edited := legacy["literature-library"]
	edited.Content += "\n\nLocal note."
	if err := s.Store.Update(func(st *State) error {
		for i := range st.Skills {
			old, ok := legacy[st.Skills[i].Name]
			if !ok {
				continue
			}
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
