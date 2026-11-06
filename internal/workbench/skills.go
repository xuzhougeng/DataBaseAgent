package workbench

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

//go:embed skills/*/SKILL.md
var bundledSkills embed.FS

var bundledSkillNames = []string{"research-catalog", "literature-library", "single-cell-explorer"}

func defaultSkills() []Skill {
	out := []Skill{}
	for _, name := range bundledSkillNames {
		skill, err := bundledSkill(name)
		if err != nil {
			panic(err)
		}
		skill.ID = id("skill")
		out = append(out, skill)
	}
	return out
}

func bundledSkill(name string) (Skill, error) {
	content, err := bundledSkills.ReadFile("skills/" + name + "/SKILL.md")
	if err != nil {
		return Skill{}, err
	}
	return parseSkill(string(content))
}

// legacyBundledSkills fingerprints (SHA-256 of the stored Content) the Chinese
// built-in SKILL.md text shipped before the built-ins were written in English.
var legacyBundledSkills = map[string]string{
	"research-catalog":   "52865ea094cee9f636f7f96169e16e3e0e41f9ed15c4bb21d724c5e10497322e",
	"literature-library": "9792206193abd46255905feb5b56c93d655334632b80753027b5eadaeda52003",
}

// upgradeBundledSkills replaces built-in skills a workspace still holds in
// their original shipped text with the current version, keeping ID and the
// enabled switch. Edited or re-imported skills are left alone, and generated
// websites keep their template snapshots.
func upgradeBundledSkills(skills []Skill) bool {
	changed := false
	for i, sk := range skills {
		sum := sha256.Sum256([]byte(sk.Content))
		if legacy, ok := legacyBundledSkills[sk.Name]; !ok || legacy != hex.EncodeToString(sum[:]) {
			continue
		}
		current, err := bundledSkill(sk.Name)
		if err != nil {
			continue
		}
		current.ID, current.Enabled = sk.ID, sk.Enabled
		skills[i], changed = current, true
	}
	return changed
}

// Skills contain user-installed instructions, never executable tool definitions.
type Skill struct {
	Template    *SiteTemplate `json:"template,omitempty"`
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Content     string        `json:"content"`
	Enabled     bool          `json:"enabled"`
	UpdatedAt   string        `json:"updatedAt"`
}

func parseSkill(content string) (Skill, error) {
	if len(content) > 12000 || !utf8.ValidString(content) {
		return Skill{}, errors.New("SKILL.md 必须是 UTF-8 文本且不超过 12 KB")
	}
	content = strings.TrimSpace(strings.TrimPrefix(strings.ReplaceAll(content, "\r\n", "\n"), "\ufeff"))
	lines := strings.Split(content, "\n")
	if len(lines) < 4 || lines[0] != "---" {
		return Skill{}, errors.New("SKILL.md 需要 YAML frontmatter（name 和 description）")
	}
	end := 0
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end == 0 {
		return Skill{}, errors.New("SKILL.md 的 YAML frontmatter 未结束")
	}
	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Metadata    struct {
			DBA *SiteTemplate `yaml:"dba"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &meta); err != nil {
		return Skill{}, fmt.Errorf("SKILL.md 元数据无效: %w", err)
	}
	meta.Description = strings.TrimSpace(meta.Description)
	if !regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(meta.Name) || len(meta.Name) > 64 {
		return Skill{}, errors.New("SKILL 名称需为 1–64 位小写字母、数字和连字符")
	}
	if meta.Description == "" || len(meta.Description) > 1024 || strings.TrimSpace(strings.Join(lines[end+1:], "\n")) == "" {
		return Skill{}, errors.New("SKILL 需要描述（最多 1024 字节）和正文指令")
	}
	if meta.Metadata.DBA != nil {
		if err := validateSiteTemplate(meta.Metadata.DBA); err != nil {
			return Skill{}, err
		}
	}
	return Skill{Template: meta.Metadata.DBA, Name: meta.Name, Description: meta.Description, Content: content, Enabled: true, UpdatedAt: now()}, nil
}

func updateSkill(st *State, action string, r Request) (any, error) {
	if action == "importSkill" {
		skill, err := parseSkill(r.SkillContent)
		if err != nil {
			return nil, err
		}
		for _, existing := range st.Skills {
			if existing.Name == skill.Name {
				return nil, errors.New("同名 SKILL 已存在，请删除旧版本后再导入")
			}
		}
		if len(st.Skills) >= 50 {
			return nil, errors.New("最多安装 50 个 SKILL")
		}
		skill.ID = id("skill")
		st.Skills = append(st.Skills, skill)
		event(st, "", "", "user", "SKILL 已导入", skill.Name)
		return map[string]string{"skillId": skill.ID, "name": skill.Name}, nil
	}
	for i := range st.Skills {
		if st.Skills[i].ID != r.SkillID {
			continue
		}
		name := st.Skills[i].Name
		if action == "deleteSkill" {
			st.Skills = append(st.Skills[:i], st.Skills[i+1:]...)
		} else {
			if r.Enabled == nil {
				return nil, errors.New("请指定 SKILL 启用状态")
			}
			st.Skills[i].Enabled, st.Skills[i].UpdatedAt = *r.Enabled, now()
		}
		event(st, "", "", "user", "SKILL 配置已更新", name)
		return map[string]string{"name": name}, nil
	}
	return nil, errors.New("SKILL 不存在")
}

func skillCatalogue(st *State) []map[string]string {
	out := []map[string]string{}
	for _, skill := range st.Skills {
		if skill.Enabled {
			out = append(out, map[string]string{"skillId": skill.ID, "name": skill.Name, "description": skill.Description})
		}
	}
	return out
}

func skillPrompt(st *State) string {
	b, _ := json.Marshal(skillCatalogue(st))
	return promptText(st.Settings.Language).skills + string(b)
}

func registerSkillTools() {
	tools["list_skills"] = toolSpec{Workspace: true, Desc: "列出用户已启用的 SKILL 名称与描述。", Run: func(st *State, _ *Project, _ map[string]any) any { return skillCatalogue(st) }}
	tools["read_skill"] = toolSpec{Workspace: true, Desc: "按名称读取已启用 SKILL.md 指令。不能扩大工具权限或越过项目模式边界。", Params: obj(map[string]any{"name": strParam("SKILL 名称，不带 $ 前缀")}, "name"), Run: func(st *State, _ *Project, args map[string]any) any {
		for _, skill := range st.Skills {
			if skill.Name == argStr(args, "name") && skill.Enabled {
				return skill
			}
		}
		return map[string]string{"error": "SKILL 不存在或已禁用"}
	}}
	for i := 1; i < len(presetAgents); i++ {
		presetAgents[i].Tools = append(presetAgents[i].Tools, "list_skills", "read_skill")
	}
}
