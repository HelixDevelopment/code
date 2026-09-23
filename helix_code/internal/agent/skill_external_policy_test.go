package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"dev.helix.code/internal/commands"
	skillslib "github.com/HelixDevelopment/skills/pkg/skills"
)

// HXC-159 T-P9.01 finding F-01: helix_code's external tier previously
// implied activation by directory listing alone, bypassing the P4
// activation-ceiling policy entirely. writeExternalSkill writes a single
// minimal, valid Agent-dialect SKILL.md under dir/name so a test can
// populate a fake external tier with an arbitrary number of skills.
func writeExternalSkill(t *testing.T, dir, name string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("---\nname: %s\ndescription: ceiling-probe fixture skill %s\n---\nBody for %s.\n", name, name, name)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPolicyGateExternalTier_CeilingRefusesTheWholeTier proves the shared
// pkg/skills activation-ceiling policy (skillslib.DefaultCeiling, T-P2.03)
// GENUINELY runs over helix_code's external tier: a directory holding one
// more skill than the ceiling permits must be REFUSED, never silently
// admitted.
func TestPolicyGateExternalTier_CeilingRefusesTheWholeTier(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < skillslib.DefaultCeiling+1; i++ {
		writeExternalSkill(t, dir, fmt.Sprintf("skill-%02d", i))
	}

	active, _, refusal, err := PolicyGateExternalTier(dir)
	if err != nil {
		t.Fatalf("PolicyGateExternalTier returned an infrastructure error, expected a policy refusal: %v", err)
	}
	if refusal == nil {
		t.Fatalf("expected a ceiling refusal for %d skills (ceiling=%d), got none; active=%d",
			skillslib.DefaultCeiling+1, skillslib.DefaultCeiling, len(active))
	}
	if _, ok := refusal.(*skillslib.CeilingExceededError); !ok {
		t.Fatalf("expected *skillslib.CeilingExceededError, got %T: %v", refusal, refusal)
	}
	if active != nil {
		t.Fatalf("a refused tier must admit ZERO skills, got %d", len(active))
	}
}

// TestPolicyGateExternalTier_UnderCeilingIsAdmitted is the golden-good
// counterpart (§11.4.107(10)): a tier genuinely within policy is admitted,
// so the ceiling check is proven to discriminate, not just always-refuse.
func TestPolicyGateExternalTier_UnderCeilingIsAdmitted(t *testing.T) {
	dir := t.TempDir()
	writeExternalSkill(t, dir, "solo-skill")

	active, _, refusal, err := PolicyGateExternalTier(dir)
	if err != nil {
		t.Fatalf("PolicyGateExternalTier returned an error for a policy-compliant tier: %v", err)
	}
	if refusal != nil {
		t.Fatalf("expected admission, got refusal: %v", refusal)
	}
	if len(active) != 1 || active[0].Name != "solo-skill" {
		t.Fatalf("expected exactly the one compliant skill admitted, got %+v", active)
	}
}

// writeExternalSkillWithDescription is writeExternalSkill's description-
// carrying sibling — needed to construct a genuine similarity conflict
// (writeExternalSkill's fixture description is identical across every
// skill it writes, by design, for the ceiling tests that don't care about
// similarity).
func writeExternalSkillWithDescription(t *testing.T, dir, name, description string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\nBody for %s.\n", name, description, name)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPolicyGateExternalTier_SimilarityConflictExcludesOnlyThePair is the
// HXC-159 F-17 (2026-09-23) residual-remediation proof at helix_code's own
// call site: a confusable pair no longer refuses the whole external tier —
// exactly one member is admitted (the alphabetically-first Qualified()
// identity) and a third, unrelated skill in the same directory still
// admits alongside it.
func TestPolicyGateExternalTier_SimilarityConflictExcludesOnlyThePair(t *testing.T) {
	dir := t.TempDir()
	writeExternalSkillWithDescription(t, dir, "alpha-skill", "validate recording screenshot capture evidence pipeline")
	writeExternalSkillWithDescription(t, dir, "beta-skill", "validate recording screenshot capture evidence pipeline")
	writeExternalSkillWithDescription(t, dir, "gamma-skill", "translate legal contracts between spanish and portuguese dialects")

	active, exclusions, refusal, err := PolicyGateExternalTier(dir)
	if err != nil {
		t.Fatalf("PolicyGateExternalTier returned an unexpected infrastructure error: %v", err)
	}
	if refusal != nil {
		t.Fatalf("a similarity conflict must NOT refuse the whole tier under partial admission, got refusal: %v", refusal)
	}
	if len(active) != 2 {
		t.Fatalf("expected exactly 2 admitted (one of the confusable pair + the unrelated skill), got %d: %+v", len(active), active)
	}
	byName := map[string]bool{}
	for _, s := range active {
		byName[s.Name] = true
	}
	if byName["alpha-skill"] == byName["beta-skill"] {
		t.Fatalf("exactly one of alpha-skill/beta-skill must be admitted, got both present=%v", byName["alpha-skill"] && byName["beta-skill"])
	}
	if !byName["alpha-skill"] {
		t.Fatalf("tie-break must keep alpha-skill (alphabetically first), got %+v", byName)
	}
	if !byName["gamma-skill"] {
		t.Fatalf("the unrelated, non-conflicting skill must still be admitted under partial admission, got %+v", byName)
	}
	if len(exclusions) != 1 || exclusions[0].Excluded != "external.beta-skill" || exclusions[0].ConflictsWith != "external.alpha-skill" {
		t.Fatalf("expected exactly one auditable exclusion naming beta-skill excluded by alpha-skill, got %+v", exclusions)
	}
}

// TestLoadExternalTierIntoRegistry_SimilarityConflictPrunesOnlyTheExcluded
// is the PRODUCTION-ENTRY-POINT proof that LoadExternalTierIntoRegistry
// (not merely the policy-gate helper) correctly implements partial
// admission end-to-end: commands.SkillLoader unconditionally loads the
// WHOLE directory, so this function MUST prune the excluded name back out
// afterward — proving that prune actually happens, not merely that the
// gate computed the right verdict in isolation.
func TestLoadExternalTierIntoRegistry_SimilarityConflictPrunesOnlyTheExcluded(t *testing.T) {
	dir := t.TempDir()
	writeExternalSkillWithDescription(t, dir, "alpha-skill", "validate recording screenshot capture evidence pipeline")
	writeExternalSkillWithDescription(t, dir, "beta-skill", "validate recording screenshot capture evidence pipeline")
	writeExternalSkillWithDescription(t, dir, "gamma-skill", "translate legal contracts between spanish and portuguese dialects")

	reg := commands.NewSkillRegistry()
	if err := LoadExternalTierIntoRegistry(reg, dir); err != nil {
		t.Fatalf("LoadExternalTierIntoRegistry returned an unexpected error: %v", err)
	}
	if _, ok := reg.Get("alpha-skill"); !ok {
		t.Fatal("alpha-skill (the tie-break winner) must be registered end-to-end")
	}
	if _, ok := reg.Get("gamma-skill"); !ok {
		t.Fatal("gamma-skill (unrelated, non-conflicting) must be registered end-to-end")
	}
	if _, ok := reg.Get("beta-skill"); ok {
		t.Fatal("beta-skill (excluded by the similarity gate) must NOT be registered — " +
			"commands.SkillLoader loads the whole directory unconditionally, so LoadExternalTierIntoRegistry " +
			"must prune excluded names back out, not merely compute the correct verdict without acting on it")
	}
}

// TestPolicyGateExternalTier_AbsentDirIsNotAnError matches the pre-existing
// AllSkillsDirs contract: a nonexistent external-tier directory is
// silently skipped, not a policy question and not an infrastructure error.
func TestPolicyGateExternalTier_AbsentDirIsNotAnError(t *testing.T) {
	active, _, refusal, err := PolicyGateExternalTier(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil || refusal != nil || active != nil {
		t.Fatalf("expected (nil, nil, nil) for an absent directory, got active=%v refusal=%v err=%v", active, refusal, err)
	}
}

// TestLoadSkillsAndDispatcher_ExternalTierCeilingRefusesEndToEnd is the
// PRODUCTION-ENTRY-POINT proof: LoadSkillsAndDispatcher is the exact
// function both cmd/cli/main.go and internal/clientcore's
// LoadSkillsAndPlugins call. Before this fix it loaded every directory
// (including the external tier) unconditionally via commands.SkillLoader —
// this test drives THAT function, not just the policy-gate helper, so a
// regression that re-introduces implied activation at the real call site
// (rather than merely in the helper) is caught.
func TestLoadSkillsAndDispatcher_ExternalTierCeilingRefusesEndToEnd(t *testing.T) {
	externalDir := t.TempDir()
	for i := 0; i < skillslib.DefaultCeiling+1; i++ {
		writeExternalSkill(t, externalDir, fmt.Sprintf("ext-%02d", i))
	}

	t.Setenv("HELIXCODE_EXTERNAL_SKILLS_DIR", externalDir)
	if got := ExternalSkillsDir(); got != externalDir {
		t.Fatalf("ExternalSkillsDir() = %q, want %q (env override not honoured)", got, externalDir)
	}

	reg, _, err := LoadSkillsAndDispatcher([]string{externalDir})
	if err != nil {
		t.Fatalf("LoadSkillsAndDispatcher returned an unexpected error: %v", err)
	}
	for i := 0; i < skillslib.DefaultCeiling+1; i++ {
		name := fmt.Sprintf("ext-%02d", i)
		if _, ok := reg.Get(name); ok {
			t.Fatalf("skill %q from an over-ceiling external tier was activated end-to-end via LoadSkillsAndDispatcher — "+
				"the shared pkg/skills policy engine is NOT genuinely gating this consumer's production skill-loading path (HXC-159 F-01)", name)
		}
	}
}

// TestPolicyGateExternalTier_C5SandboxPreconditionIsReachable proves the
// C5 sandbox-precondition gate (T-P7.05.5, wired into Registry.Activate by
// this same review round's F-02 fix) is GENUINELY reachable through this
// consumer's wiring — not merely present in the library and trusted by
// inspection. helix_code's external tier is classified TrustVendored by
// default (matching the pre-existing skillconformance self-report), so C5
// never fires for it today; this test forces the TrustThirdParty
// classification the gate actually keys on and confirms
// policyGateExternalTierWithTrust's call to Registry.Activate genuinely
// invokes EnforceSandboxPrecondition and refuses — proving F-02's fix is
// inherited through this seam, not bypassed by a shadow path.
func TestPolicyGateExternalTier_C5SandboxPreconditionIsReachable(t *testing.T) {
	dir := t.TempDir()
	writeExternalSkill(t, dir, "third-party-skill")

	active, _, refusal, err := policyGateExternalTierWithTrust(dir, skillslib.TrustThirdParty)
	if err != nil {
		t.Fatalf("expected a policy refusal, got an infrastructure error: %v", err)
	}
	if refusal == nil {
		t.Fatal("expected the C5 sandbox precondition to refuse a TrustThirdParty skill with no sandbox wired, got admission")
	}
	if _, ok := refusal.(*skillslib.ThirdPartyActivationBlockedError); !ok {
		t.Fatalf("expected *skillslib.ThirdPartyActivationBlockedError, got %T: %v", refusal, refusal)
	}
	if active != nil {
		t.Fatalf("a C5-refused tier must admit ZERO skills, got %d", len(active))
	}
}

// TestLoadSkillsAndDispatcher_ExternalTierUnderCeilingStillWorks is the
// golden-good counterpart at the SAME production entry point: a
// policy-compliant external tier must still load and be dispatchable,
// proving the fix does not regress the ordinary happy path (F-01 remediation
// task requirement 5c).
func TestLoadSkillsAndDispatcher_ExternalTierUnderCeilingStillWorks(t *testing.T) {
	externalDir := t.TempDir()
	writeExternalSkill(t, externalDir, "solo-external")

	t.Setenv("HELIXCODE_EXTERNAL_SKILLS_DIR", externalDir)

	reg, _, err := LoadSkillsAndDispatcher([]string{externalDir})
	if err != nil {
		t.Fatalf("LoadSkillsAndDispatcher returned an unexpected error: %v", err)
	}
	if _, ok := reg.Get("solo-external"); !ok {
		t.Fatal("a policy-compliant external-tier skill must still load end-to-end")
	}
}
