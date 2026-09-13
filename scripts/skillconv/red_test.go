// T-P1.08.1 RED: each parser is blind to the other's dialect file.
//
// These tests do NOT modify either production parser. They transcribe (read-only)
// the two frontmatter shapes and prove, against REAL in-tree files, that a naive
// cross-dialect parse loses load-bearing fields:
//
//   - HelixCode shape (helix_code/internal/commands/markdown_skills.go):
//     skillFrontmatter{description, triggers, variables, requires_isolation},
//     name comes from the FILENAME, never the frontmatter.
//   - HelixAgent shape (submodules/helix_agent/internal/skills/types.go):
//     Skill{Name, Description, AllowedTools, Version, License, Author,
//     Category, Tags, ...}, name carried IN the frontmatter.
//
// A passing test here documents a real interop failure that T-P1.08.3/4 must fix
// via the canonical model — it is RED evidence, not a regression guard for the
// parsers themselves.
package skillconv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// hcFrontmatter mirrors HelixCode's skillFrontmatter (markdown_skills.go).
// Unknown keys (e.g. an Agent-style `name:`) are silently dropped by yaml into
// nothing — that silent drop is exactly what TestRed_HelixCodeDropsAgentName proves.
type hcFrontmatter struct {
	Description       string            `yaml:"description"`
	Triggers          []string          `yaml:"triggers"`
	Variables         map[string]string `yaml:"variables"`
	RequiresIsolation bool              `yaml:"requires_isolation"`
}

// haFrontmatter mirrors the frontmatter-mapped subset of HelixAgent's Skill
// (submodules/helix_agent/internal/skills/types.go).
type haFrontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	AllowedTools string   `yaml:"allowed-tools"`
	Version      string   `yaml:"version"`
	License      string   `yaml:"license"`
	Author       string   `yaml:"author"`
	Category     string   `yaml:"category"`
	Tags         []string `yaml:"tags"`
}

// splitDoc splits a SKILL.md document into frontmatter and body using the same
// rule both production parsers share: the file must open with `---` and the
// frontmatter ends at the next line that is exactly `---`.
func splitDoc(t *testing.T, path string) (fm, body string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		t.Fatalf("%s has no frontmatter, cannot serve as dialect fixture", path)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		t.Fatalf("%s: unterminated frontmatter", path)
	}
	return strings.Join(lines[1:end], "\n"), strings.Join(lines[end+1:], "\n")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	// Tests run with cwd = the package directory (scripts/skillconv).
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

// TestRed_HelixCodeParserFailsOnAgentFile feeds a real HelixAgent-shaped skill
// through the HelixCode frontmatter shape and proves load-bearing data is lost:
// `triggers` is empty (Agent files carry no regex triggers) and the `name:` key
// the Agent file depends on is silently dropped (no such field exists).
func TestRed_HelixCodeParserFailsOnAgentFile(t *testing.T) {
	agentFile := filepath.Join(repoRoot(t), "submodules", "helix_agent", "skills",
		"postgres-mcp", "design-postgres-tables", "SKILL.md")
	fm, _ := splitDoc(t, agentFile)

	var hc hcFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &hc); err != nil {
		t.Fatalf("unmarshal agent fm into hc shape: %v", err)
	}

	// The Agent file carries its identity in `name:` — the HC shape has nowhere
	// to put it, so it vanishes.
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fm), &raw); err != nil {
		t.Fatalf("unmarshal agent fm into map: %v", err)
	}
	nameInFile, hasName := raw["name"]
	if !hasName || nameInFile == "" {
		t.Fatalf("fixture %s has no name: key, not an Agent-shaped file", agentFile)
	}
	t.Logf("RED evidence: agent file carries name=%q", nameInFile)
	t.Logf("RED evidence: hc shape parsed triggers=%v (empty — Agent files have no regex triggers)", hc.Triggers)

	if len(hc.Triggers) != 0 {
		t.Fatalf("expected HC parse of Agent file to yield zero triggers, got %v", hc.Triggers)
	}
	// The struct literally cannot represent the name: proving the blindness.
	// (Compile-time fact, asserted here as documentation; the loss is silent.)
	t.Logf("RED evidence: hcFrontmatter has no Name field — name=%q is SILENTLY DROPPED", nameInFile)
}

// TestRed_HelixAgentParserFailsOnCodeFile feeds a real HelixCode-shaped skill
// through the HelixAgent frontmatter shape and proves load-bearing data is lost:
// `name` is empty (HC takes it from the filename) and `allowed-tools` is empty
// (HC has no such concept), while HC's `triggers`/`variables`/`requires_isolation`
// keys fall on the floor.
func TestRed_HelixAgentParserFailsOnCodeFile(t *testing.T) {
	codeFile := filepath.Join(repoRoot(t), "helix_code", "internal", "commands",
		"builtin_skills", "conventional-commit", "SKILL.md")
	fm, _ := splitDoc(t, codeFile)

	var ha haFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &ha); err != nil {
		t.Fatalf("unmarshal hc fm into ha shape: %v", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(fm), &raw); err != nil {
		t.Fatalf("unmarshal hc fm into map: %v", err)
	}
	if _, ok := raw["triggers"]; !ok {
		t.Fatalf("fixture %s has no triggers: key, not a HelixCode-shaped file", codeFile)
	}

	t.Logf("RED evidence: ha shape parsed name=%q allowed-tools=%q (both empty — HC takes name from filename, has no allowed-tools)", ha.Name, ha.AllowedTools)
	if ha.Name != "" {
		t.Fatalf("expected HA parse of HC file to yield empty name, got %q", ha.Name)
	}
	if ha.AllowedTools != "" {
		t.Fatalf("expected HA parse of HC file to yield empty allowed-tools, got %q", ha.AllowedTools)
	}
	for _, k := range []string{"triggers", "variables", "requires_isolation"} {
		if _, ok := raw[k]; ok {
			t.Logf("RED evidence: hc key %q present in file, SILENTLY DROPPED by ha shape", k)
		}
	}
}
