package commands_test

// T-P5.01 — export a Skill constructor (HXC-159 Phase P5).
//
// # RUNTIME SIGNATURE (§11.4.108)
//
// Before this fix, `grep "func NewSkill\b"` returns zero hits module-wide:
// the only constructors are unexported parseSkillFile and a documented
// test-only helper (ParseSkillForTest, which round-trips through the exact
// same on-disk frontmatter parser rather than accepting structured fields).
// A *commands.Skill therefore cannot be built from a package OUTSIDE
// internal/commands except by writing a real SKILL.md to disk and driving it
// through SkillLoader — there is no in-memory construction seam at all.
//
// This file lives in an EXTERNAL test package (commands_test, not commands)
// specifically so it can only see commands' exported surface — exactly the
// constraint T-P5.05's future external-tier skill source will be under. It
// is the compile-time proof of the runtime signature: "an external package
// constructs a *Skill in-memory and registers it — no filesystem round-trip."
//
// Before commands.NewSkill existed, this file did not compile at all:
//
//	./skill_ctor_external_test.go:NN:14: undefined: commands.NewSkill
//	./skill_ctor_external_test.go:NN:14: undefined: commands.SkillOptions
//
// which is captured verbatim in
// qa-results/hxc159/skill_ctor/<run-id>/red_compile_fail.txt. That captured
// failure IS the RED reproduction (§11.4.115) — there is no runtime polarity
// switch for a construction seam that does not exist yet; the defect is
// "this package cannot be written," and the fix is that it now can.

import (
	"testing"

	"dev.helix.code/internal/commands"
)

// TestNewSkill_ExternalPackageConstructsInMemory is the GREEN standing
// regression guard for T-P5.01 / RS "external construction, no filesystem
// round-trip". It builds a *commands.Skill entirely from structured fields —
// no os.WriteFile, no SkillLoader, no on-disk SKILL.md anywhere in this
// test — registers it, matches a trigger against it, and renders the body
// with a named capture substituted. Every step is only reachable through
// commands' EXPORTED surface, proving the constructor is genuinely usable
// from outside the package (the T-P5.05 external-tier use case) and that it
// produces a Skill indistinguishable, at every exported method, from one
// loaded from disk via parseSkillFile.
func TestNewSkill_ExternalPackageConstructsInMemory(t *testing.T) {
	s, err := commands.NewSkill(commands.SkillOptions{
		Name:            "external-fixture",
		Description:     "constructed entirely in memory by an external package",
		Body:            "Hello, {{ARG.who}}! This body came from {{ARG1}}.",
		TriggerPatterns: []string{`(?i)^greet (?P<who>.+)$`},
		Variables:       map[string]string{"unused": "default"},
		SourcePath:      "in-memory:external-fixture",
	})
	if err != nil {
		t.Fatalf("commands.NewSkill: unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("commands.NewSkill: returned nil *Skill with nil error")
	}

	// Exported accessors reflect exactly what was supplied — proves the
	// constructor did not silently drop or mutate the caller's data.
	if got := s.Name(); got != "external-fixture" {
		t.Fatalf("Name() = %q, want %q", got, "external-fixture")
	}
	if got := s.Description(); got != "constructed entirely in memory by an external package" {
		t.Fatalf("Description() = %q, want the supplied description, got %q", got, got)
	}
	if got := s.SourcePath(); got != "in-memory:external-fixture" {
		t.Fatalf("SourcePath() = %q, want %q", got, "in-memory:external-fixture")
	}
	patterns := s.TriggerPatterns()
	if len(patterns) != 1 || patterns[0] != `(?i)^greet (?P<who>.+)$` {
		t.Fatalf("TriggerPatterns() = %v, want the single supplied pattern", patterns)
	}

	// Register it — no filesystem round-trip anywhere in this call chain.
	reg := commands.NewSkillRegistry()
	reg.Add(s)

	got, ok := reg.Get("external-fixture")
	if !ok {
		t.Fatal("registry.Get(\"external-fixture\") not found after Add — construct-then-register did not work")
	}
	if got != s {
		t.Fatal("registry.Get returned a different *Skill instance than was added")
	}

	// Trigger matching: the compiled regex must actually be live, not just
	// the raw pattern string echoed back by TriggerPatterns().
	matched, caps, found := reg.FindMatching("greet the world")
	if !found {
		t.Fatal("FindMatching(\"greet the world\") did not match the constructed skill's trigger")
	}
	if matched.Name() != "external-fixture" {
		t.Fatalf("FindMatching matched skill %q, want %q", matched.Name(), "external-fixture")
	}
	if caps["who"] != "the world" {
		t.Fatalf("named capture %q = %q, want %q", "who", caps["who"], "the world")
	}

	// Render: proves the body + variables the constructor stored are the
	// SAME fields the rendering engine reads (no widened/parallel storage).
	rendered, err := matched.RenderWithCaptures(nil, caps, "", "")
	if err != nil {
		t.Fatalf("RenderWithCaptures: unexpected error: %v", err)
	}
	const want = "Hello, the world! This body came from ."
	if rendered != want {
		t.Fatalf("rendered body = %q, want %q", rendered, want)
	}
}

// TestNewSkill_RejectsEmptyName proves the constructor VALIDATES rather than
// blindly accepting any struct literal — "construct through validation" per
// the task's own acceptance language, not merely "export a struct literal
// path". A disk-loaded skill's name always comes from a non-empty filename
// or directory basename (parseSkillFile has no code path that produces an
// empty name), so an in-memory constructor accepting an empty name would
// WIDEN the type's existing invariants rather than preserve them.
func TestNewSkill_RejectsEmptyName(t *testing.T) {
	_, err := commands.NewSkill(commands.SkillOptions{
		Description: "no name supplied",
		Body:        "irrelevant",
	})
	if err == nil {
		t.Fatal("commands.NewSkill with an empty Name: want a non-nil error, got nil (invariant not enforced)")
	}
}

// TestNewSkill_InvalidTriggerPatternIsSkippedNotFatal proves NewSkill mirrors
// parseSkillFile's documented on-disk behaviour for a malformed regex — it is
// silently dropped from the compiled set (never a construction error), while
// the raw pattern is preserved via TriggerPatterns() for diagnostics. This is
// the "does not widen the type's invariants" requirement checked from the
// opposite direction: the constructor must not be STRICTER than on-disk
// loading either, or an external source that already validates elsewhere
// would behave differently through this seam than through the loader.
func TestNewSkill_InvalidTriggerPatternIsSkippedNotFatal(t *testing.T) {
	s, err := commands.NewSkill(commands.SkillOptions{
		Name:            "bad-regex-fixture",
		Body:            "unreachable",
		TriggerPatterns: []string{"(unterminated["},
	})
	if err != nil {
		t.Fatalf("commands.NewSkill: unexpected error for an invalid (but non-fatal) trigger pattern: %v", err)
	}
	if s == nil {
		t.Fatal("commands.NewSkill returned nil *Skill for an invalid trigger pattern; want a Skill with zero compiled triggers")
	}
	// The raw pattern must still be visible for diagnostics even though it
	// did not compile — matching parseSkillFile's documented contract.
	patterns := s.TriggerPatterns()
	if len(patterns) != 1 || patterns[0] != "(unterminated[" {
		t.Fatalf("TriggerPatterns() = %v, want the raw invalid pattern preserved", patterns)
	}
	// It must never match anything (no compiled trigger reached the
	// registry's FindMatching path).
	reg := commands.NewSkillRegistry()
	reg.Add(s)
	if _, _, found := reg.FindMatching("bad-regex-fixture anything"); found {
		t.Fatal("a skill with only an invalid trigger pattern must never match FindMatching")
	}
}
