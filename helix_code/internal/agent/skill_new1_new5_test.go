package agent

import (
	"os"
	"path/filepath"
	"testing"

	"dev.helix.code/internal/commands"
)

// writeExternalSkillRaw writes dir/name/SKILL.md with EXACTLY the raw body
// given — no helper-side escaping/formatting — so a test can control the
// on-disk YAML bytes precisely (needed to reproduce NEW-1's escaping
// defect, which only manifests for a SPECIFIC on-disk byte sequence).
func writeExternalSkillRaw(t *testing.T, dir, name, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadExternalTierIntoRegistry_TriggerRegexEscapingMatchesProductionParser
// is the HXC-159 T-P9.01 round-5 finding NEW-1 reproduction (2026-09-23,
// found during the round-4 N1/N2 fix's OWN required independent review):
// commands.NewSkill-based construction from pkg/skills' own already-parsed
// struct (the N1/N2 fix's mechanism) does not process YAML string-escaping
// the same way the production yaml.v3 parser (used by every OTHER tier)
// does, because pkg/skills' own front-matter reader
// (submodules/skills/pkg/skills/frontmatter.go) never un-quotes/un-escapes
// LIST items (only scalar values go through its unquote() helper) — a
// double-quoted trigger pattern with an escaped backslash survives on the
// wire with its literal quote characters AND its doubled backslash intact,
// so the compiled regex requires literal `"` characters around the match
// and a literal backslash before `w`, and can never match real input.
//
// This fixture reproduces the exact shape the independent reviewer found on
// the real corpus: `zapper`'s trigger
// `"(?i)^zap (?P<what>\w+) now$"` (written here with the double-escaped
// backslash a real YAML author writes to represent one literal backslash
// in the regex — `\\w+` on the wire, `\w+` after correct YAML decoding).
//
// RED on the pre-fix code (commands.NewSkill from pkg/skills'
// XHelixCode.Triggers): the compiled trigger pattern carries the raw,
// still-quoted, still-double-escaped string, and "zap foo now" does not
// match it. GREEN post-fix (commands.ParseSkillFile re-parsing the
// manifest through yaml.v3): the trigger compiles to the CORRECT regex and
// matches real input, with the named capture group populated.
func TestLoadExternalTierIntoRegistry_TriggerRegexEscapingMatchesProductionParser(t *testing.T) {
	dir := t.TempDir()
	// The double backslash below is DELIBERATE and on-the-wire literal (this
	// is a Go raw/backtick string, so no Go-level escaping applies): a YAML
	// double-quoted scalar's OWN escape rules require "\\" to represent one
	// literal backslash, exactly the shape a real SKILL.md author writes to
	// embed a `\w+` regex inside a double-quoted trigger pattern. A single
	// backslash here (`\w`) is not a valid YAML escape sequence at all, which
	// is a DIFFERENT defect than the one under test.
	body := `---
name: zapper
description: zap something now
triggers:
  - "(?i)^zap (?P<what>\\w+) now$"
---
Zapping {{ARG.what}}.
`
	writeExternalSkillRaw(t, dir, "zapper", body)

	reg := commands.NewSkillRegistry()
	if err := LoadExternalTierIntoRegistry(reg, dir); err != nil {
		t.Fatalf("LoadExternalTierIntoRegistry returned an unexpected error: %v", err)
	}

	s, ok := reg.Get("zapper")
	if !ok {
		t.Fatal("zapper must be registered (it is the only, policy-compliant skill in this fixture)")
	}
	patterns := s.TriggerPatterns()
	if len(patterns) != 1 {
		t.Fatalf("expected exactly 1 trigger pattern, got %d: %+v", len(patterns), patterns)
	}
	const wantPattern = `(?i)^zap (?P<what>\w+) now$`
	if patterns[0] != wantPattern {
		t.Fatalf("NEW-1: trigger pattern was not decoded through the production YAML parser — "+
			"want %q, got %q (quote characters and/or a doubled backslash leaked through unescaped)",
			wantPattern, patterns[0])
	}

	matched, caps, ok := reg.FindMatching("zap foo now")
	if !ok {
		t.Fatalf("NEW-1: the zapper trigger must match real input \"zap foo now\" — "+
			"it compiled from pattern %q, which never matches real text if the YAML escaping "+
			"was not correctly decoded", patterns[0])
	}
	if matched.Name() != "zapper" {
		t.Fatalf("expected zapper to match, got %q", matched.Name())
	}
	if caps["what"] != "foo" {
		t.Fatalf("expected named capture group what=foo, got %q (captures=%+v)", caps["what"], caps)
	}
}

// TestLoadExternalTierIntoRegistry_RequiresIsolationHonoursAllYAMLBooleanForms
// is the HXC-159 T-P9.01 round-5 finding NEW-5 reproduction (2026-09-23):
// pkg/skills' front-matter reader reads requires_isolation via an EXACT,
// case-sensitive string comparison against the literal "true"
// (frontmatter.go: `scalars["requires_isolation"] == "true"`), so
// "True", "TRUE", or "true" with a trailing comment all silently parse to
// false — a fail-open on the isolation flag. commands' production
// yaml.v3-based parser correctly recognises every YAML 1.1 boolean form
// for a bool-typed struct field.
//
// No production code currently routes on this flag (it is latent, not yet
// exploitable per the round-5 finding), but it must not ship broken: this
// is a standing regression guard against RequiresIsolation() ever silently
// defaulting to false for a skill that explicitly declared it true.
//
// RED on the pre-fix code (commands.NewSkill from pkg/skills'
// XHelixCode.RequiresIsolation): RequiresIsolation() is false for a skill
// whose manifest reads `requires_isolation: True`. GREEN post-fix
// (commands.ParseSkillFile / yaml.v3): RequiresIsolation() is true.
func TestLoadExternalTierIntoRegistry_RequiresIsolationHonoursAllYAMLBooleanForms(t *testing.T) {
	dir := t.TempDir()
	body := `---
name: needs-sandbox
description: a skill that must not run outside a sandbox
requires_isolation: True
---
Body for needs-sandbox.
`
	writeExternalSkillRaw(t, dir, "needs-sandbox", body)

	reg := commands.NewSkillRegistry()
	if err := LoadExternalTierIntoRegistry(reg, dir); err != nil {
		t.Fatalf("LoadExternalTierIntoRegistry returned an unexpected error: %v", err)
	}

	s, ok := reg.Get("needs-sandbox")
	if !ok {
		t.Fatal("needs-sandbox must be registered (it is the only, policy-compliant skill in this fixture)")
	}
	if !s.RequiresIsolation() {
		t.Fatalf("NEW-5: requires_isolation: True must be honoured as true " +
			"(pkg/skills' own front-matter reader is an exact case-sensitive comparison " +
			"against the literal \"true\" and silently reads \"True\" as false)")
	}
}
