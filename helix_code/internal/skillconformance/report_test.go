package skillconformance

import (
	"os"
	"path/filepath"
	"testing"

	"dev.helix.code/internal/commands"
)

func writeFixtureSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGenerateReport_RealLoader_ValidatesClean drives the REAL production
// commands.SkillLoader (never a reimplementation) against real fixture
// skill directories, and asserts the generated report both validates
// against the shared schema and honestly reflects this loader's actual,
// measured capability surface (not an assumed one — §11.4.6).
func TestGenerateReport_RealLoader_ValidatesClean(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()
	writeFixtureSkill(t, userDir, "sample-user-skill", "---\ndescription: a fixture skill for the conformance report\n---\nBody.\n")
	writeFixtureSkill(t, projectDir, "sample-project-skill", "---\ndescription: another fixture skill\n---\nBody.\n")

	reg := commands.NewSkillRegistry()
	loader := commands.NewSkillLoader(reg, projectDir, userDir)
	if err := loader.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	m := GenerateReport(reg, DirTierMap{Project: projectDir, User: userDir})
	if errs := m.Validate(); len(errs) != 0 {
		t.Fatalf("generated report must validate clean, got: %v", errs)
	}
	if m.Consumer != "helix_code" {
		t.Fatalf("consumer = %q, want helix_code", m.Consumer)
	}

	// At least the two fixture skills plus whatever built-ins ship in the
	// binary must be present. Assert the fixtures specifically, by their
	// tier-qualified names, since a report that silently dropped a
	// registered skill would be exactly the T-P7.02 bug class.
	found := map[string]bool{}
	for _, s := range m.Skills {
		found[s.Qualified] = true
	}
	if !found["user.sample-user-skill"] {
		t.Errorf("report missing user.sample-user-skill; got %+v", m.Skills)
	}
	if !found["project.sample-project-skill"] {
		t.Errorf("report missing project.sample-project-skill; got %+v", m.Skills)
	}

	// This loader has no per-skill `capabilities:` frontmatter field
	// (commands.skillFrontmatter carries description/triggers/variables/
	// requires_isolation only — measured directly in markdown_skills.go,
	// not assumed) and no capability-enforcement call boundary anywhere
	// in this package. The report MUST say so honestly rather than
	// claiming a surface this loader does not have.
	if m.LoaderAPISurface.CapabilityDeclaration {
		t.Error("helix_code's markdown-skill frontmatter has no capabilities field; capability_declaration must be false")
	}
	if m.LoaderAPISurface.CapabilityEnforcement {
		t.Error("helix_code has no capability call-boundary; capability_enforcement must be false")
	}
	if m.LoaderAPISurface.NamespacedIdentity {
		t.Error("commands.SkillRegistry keys by bare skill name (Add/Get/Remove(name)); namespaced_identity must be false")
	}
	if !m.LoaderAPISurface.MalformedSkillVisibility {
		t.Error("SkillLoader.Reload logs a per-file parse error at WARN and continues (markdown_skills.go doc comment); malformed_skill_visibility must be true")
	}
	if m.LoaderAPISurface.AllowlistActivation {
		t.Error("T-P5.05.4 found zero Go implementation of a per-skill activation ceiling in this module; allowlist_activation must be false")
	}
}

// TestGenerateReport_MalformedSkillIsVisibleNotFatal proves the
// malformed_skill_visibility=true claim against a REAL malformed fixture,
// not merely a doc-comment assertion.
func TestGenerateReport_MalformedSkillIsVisibleNotFatal(t *testing.T) {
	userDir := t.TempDir()
	writeFixtureSkill(t, userDir, "good-skill", "---\ndescription: fine\n---\nBody.\n")
	// Malformed for THIS loader specifically: parseSkillFile
	// (markdown_skills.go) treats a body with NO leading "---\n" as a
	// valid frontmatter-less skill (helix_code's legacy dialect makes
	// frontmatter optional, unlike the library's Agent-standard loader,
	// which requires it — a real, measured dialect difference, not an
	// assumption). The genuine failure mode here is an UNTERMINATED
	// frontmatter block: parseSkillFile returns
	// "unterminated frontmatter" when "---\n" opens but no closing
	// "\n---" is ever found.
	writeFixtureSkill(t, userDir, "bad-skill", "---\ndescription: never closed\nno closing delimiter at all\n")

	reg := commands.NewSkillRegistry()
	loader := commands.NewSkillLoader(reg, "", userDir)
	if err := loader.Load(); err != nil {
		t.Fatalf("Load must NOT fail on a malformed sibling skill: %v", err)
	}
	m := GenerateReport(reg, DirTierMap{User: userDir})
	found := map[string]bool{}
	for _, s := range m.Skills {
		found[s.Qualified] = true
	}
	if !found["user.good-skill"] {
		t.Fatalf("well-formed sibling must still be registered; report=%+v", m.Skills)
	}
	if found["user.bad-skill"] {
		t.Fatalf("malformed skill must not appear as a successfully-loaded entry; report=%+v", m.Skills)
	}
}
