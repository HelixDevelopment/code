package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// admission end-to-end. Name retained across the N1/N2 fix (2026-09-23) for
// history/searchability; the MECHANISM it proves changed: this function no
// longer loads the whole directory and prunes exclusions back out — it
// converts ONLY the pkg/skills-admitted set directly into registry entries
// (see LoadExternalTierIntoRegistry's doc comment), so an excluded skill is
// simply never Added in the first place. This test still proves the
// end-to-end OUTCOME (exactly the admitted pair registered, the excluded
// member absent) regardless of which mechanism produces it.
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
			"a policy-excluded skill must never be added to the registry")
	}
}

// writeExternalFlatSkill writes dir/<name>.md — the LEGACY FLAT skill form
// commands.SkillLoader has always recognised (markdown_skills.go's
// skillManifestName doc comment), directly under dir (no subdirectory, no
// SKILL.md manifest). pkg/skills' Loader (loader.go's walkForManifests) only
// ever discovers an exact-case SKILL.md — a flat "<name>.md" file is
// STRUCTURALLY INVISIBLE to it, at any directory depth, by construction.
func writeExternalFlatSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("---\ndescription: flat fixture skill %s\n---\nBody for flat %s.\n", name, name)
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadExternalTierIntoRegistry_FlatMdFilesNeverBypassPolicyCeiling is the
// HXC-159 T-P9.01 round-4 finding N1 reproduction (2026-09-23, found during
// the F-21 independent review): commands.SkillLoader (the loader
// LoadExternalTierIntoRegistry used to hand the WHOLE external-tier directory
// to unconditionally) registers both packaged `*/SKILL.md` skills AND legacy
// flat `<name>.md` files from the same directory — but pkg/skills' policy
// engine (ceiling/similarity/capability/provenance/C5 sandbox) only ever
// scans `*/SKILL.md`, so a flat file was NEVER presented to it at all. Once
// at least one packaged skill is admitted through the gate, this let EVERY
// flat `.md` file in the directory register with ZERO ceiling or similarity
// check — the exact reproduction the independent reviewer captured on a real
// binary with fixture `fx4` (one packaged skill + 25 flat files, all 25
// registered, DefaultCeiling=20 exceeded, 0 refusal log lines).
//
// RED on the pre-fix code: every flat-N file below is present in reg
// alongside the packaged skill, with LoadExternalTierIntoRegistry returning
// no error and PolicyGateExternalTier never having refused anything (its own
// ceiling count was 1, since pkg/skills never saw the flat files to count
// them). GREEN post-fix: the packaged skill (genuinely policy-evaluated,
// genuinely within ceiling) is admitted; NOT ONE flat file — of any count,
// including well past DefaultCeiling — is EVER registered from the external
// tier, because commands.SkillLoader's directory scan is no longer used to
// populate the external tier at all; only pkg/skills' own admitted set is
// (see LoadExternalTierIntoRegistry's rewritten doc comment).
func TestLoadExternalTierIntoRegistry_FlatMdFilesNeverBypassPolicyCeiling(t *testing.T) {
	dir := t.TempDir()
	writeExternalSkill(t, dir, "packaged-skill")
	const flatCount = skillslib.DefaultCeiling + 5
	for i := 0; i < flatCount; i++ {
		writeExternalFlatSkill(t, dir, fmt.Sprintf("flat-%02d", i))
	}

	reg := commands.NewSkillRegistry()
	if err := LoadExternalTierIntoRegistry(reg, dir); err != nil {
		t.Fatalf("LoadExternalTierIntoRegistry returned an unexpected error: %v", err)
	}

	if _, ok := reg.Get("packaged-skill"); !ok {
		t.Fatal("packaged-skill (the one genuinely policy-evaluated skill, within ceiling) must be registered")
	}
	var leaked []string
	for i := 0; i < flatCount; i++ {
		name := fmt.Sprintf("flat-%02d", i)
		if _, ok := reg.Get(name); ok {
			leaked = append(leaked, name)
		}
	}
	if len(leaked) > 0 {
		t.Fatalf("N1: %d/%d flat-file skills bypassed the external-tier policy gate entirely and were registered "+
			"without ever being ceiling/similarity/capability/sandbox-checked by pkg/skills: %v", len(leaked), flatCount, leaked)
	}
}

// TestLoadExternalTierIntoRegistry_NestedSkillThatWinsSimilarityBecomesLoadable
// is the HXC-159 T-P9.01 round-4 finding N2 reproduction (2026-09-23, found
// during the F-21 independent review, fixture `fx2`): pkg/skills' Loader
// (loader.go's walkForManifests) recurses arbitrarily deep and gates
// `<dir>/<group>/<skill>/SKILL.md` exactly like a top-level one, but
// commands.SkillLoader only ever reads ONE level (`<dir>/<name>/SKILL.md`)
// and never loads a nested skill at all. A nested skill can still WIN a
// similarity conflict against a real top-level skill — pruning the top-level
// loser — while the nested winner itself was, pre-fix, never actually
// registered (commands.SkillLoader's shallow directory scan never found it),
// stranding BOTH: neither skill ends up available.
//
// This fixture reproduces exactly that shape: "aa-nested" (two levels deep,
// under group/) and "bb-toplevel" (one level deep) share an identical
// description, so pkg/skills' deterministic ascending-Qualified() tie-break
// (external.aa-nested < external.bb-toplevel) admits aa-nested and excludes
// bb-toplevel.
//
// RED on the pre-fix code: reg.Get("aa-nested") is NOT ok (commands.SkillLoader
// never discovered the nested manifest) AND reg.Get("bb-toplevel") is also
// NOT ok (pruned as the similarity loser) — both unavailable. GREEN post-fix:
// aa-nested (the genuine policy-admitted winner) is registered with its real
// rendered body; bb-toplevel (the correctly-excluded loser) stays absent —
// exactly one of the confusable pair is ever available, never zero.
func TestLoadExternalTierIntoRegistry_NestedSkillThatWinsSimilarityBecomesLoadable(t *testing.T) {
	dir := t.TempDir()
	const clique = "reconcile ledger balance entries nightly batch"
	writeExternalSkillWithDescription(t, dir, "bb-toplevel", clique)

	nestedDir := filepath.Join(dir, "group", "aa-nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("---\ndescription: %s\n---\nBody for aa-nested.\n", clique)
	if err := os.WriteFile(filepath.Join(nestedDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := commands.NewSkillRegistry()
	if err := LoadExternalTierIntoRegistry(reg, dir); err != nil {
		t.Fatalf("LoadExternalTierIntoRegistry returned an unexpected error: %v", err)
	}

	s, ok := reg.Get("aa-nested")
	if !ok {
		t.Fatal("N2: aa-nested (the similarity tie-break WINNER, a nested skill) must be registered end-to-end — " +
			"a policy-admitted skill must never be structurally unloadable")
	}
	if s.Body() == "" {
		t.Fatal("aa-nested must carry its real rendered body, not an empty stand-in")
	}
	if _, ok := reg.Get("bb-toplevel"); ok {
		t.Fatal("N2: bb-toplevel (the similarity tie-break LOSER) must NOT be registered — " +
			"exactly one of a confusable pair may ever be available")
	}
}

// TestLoadSkillsAndDispatcher_ExcludedExternalSkillSharingBuiltinNameNeverDeletesBuiltin
// answers the F-21 reviewer's related, unverified concern from round-4
// (2026-09-23): "if an excluded nested skill's directory name COINCIDES
// with a builtin skill's name, the F-21-fixed prune-by-Dir step could
// delete the builtin's registry entry instead of (or as well as) the
// intended nested one — order-dependent on whether the external tier is
// the LAST entry in skillsDirs when LoadSkillsAndDispatcher runs".
//
// Under the OLD (pre-N1/N2-fix) implementation this concern was REAL in the
// worst case: LoadExternalTierIntoRegistry's OWN internal sequence loaded
// the WHOLE external directory via a fresh commands.SkillLoader (which
// unconditionally re-Added every builtin into the SAME reg), then pruned
// every policy-excluded skill back out via reg.Remove(dirName) — so an
// excluded external skill sharing its directory name with a real builtin
// left reg[name] DELETED at the end of that one function call, with
// nothing to restore it if external was the LAST (or only) directory
// LoadSkillsAndDispatcher processed (AllSkillsDirs' production ordering
// always puts projectDir after external, which happened to paper over
// this in the one call site that used it, but the two hand-rolled
// sequences in cmd/cli/main.go and any caller passing external alone were
// exposed).
//
// This test drives the worst case directly: an external-tier skill whose
// DIRECTORY NAME is a real builtin name ("conventional-commit"), made to
// LOSE a similarity conflict (so policy excludes it), with the external
// tier as the ONLY (hence trivially "last") directory passed to
// LoadSkillsAndDispatcher. Post N1/N2 fix, LoadExternalTierIntoRegistry
// never re-scans the directory via commands.SkillLoader and never calls
// reg.Remove at all — a policy-excluded skill was simply never Added — so
// the real builtin's registry entry is structurally untouched regardless
// of directory-list ordering. GREEN: the builtin survives with its ORIGINAL
// embedded body, not the excluded external skill's content and not deleted.
func TestLoadSkillsAndDispatcher_ExcludedExternalSkillSharingBuiltinNameNeverDeletesBuiltin(t *testing.T) {
	isolateHomeAndConfig(t) // keep legacy/canonical dirs empty and irrelevant here

	dir := t.TempDir()
	const clique = "reconcile ledger balance entries nightly batch"
	// Admitted first alphabetically: "aaa-winner" < "conventional-commit".
	writeExternalSkillWithDescription(t, dir, "aaa-winner", clique)
	// This directory's NAME COLLIDES with a real embedded builtin
	// ("conventional-commit") and is made to LOSE the similarity conflict
	// against aaa-winner, so policy excludes it.
	writeExternalSkillWithDescription(t, dir, "conventional-commit", clique)

	// dir must be recognised as THE (policy-gated) external tier for this
	// scenario to test what it claims to test — without this, dir would be
	// treated as an ORDINARY directory (bypassing the policy gate entirely,
	// a different bug class this test is not about).
	t.Setenv(externalSkillsDirEnvVar, dir)

	reg, _, err := LoadSkillsAndDispatcher([]string{dir})
	if err != nil {
		t.Fatalf("LoadSkillsAndDispatcher returned an unexpected error: %v", err)
	}

	s, ok := reg.Get("conventional-commit")
	if !ok {
		t.Fatal("the real builtin \"conventional-commit\" must still be registered — " +
			"an excluded external skill sharing its directory name must never delete the builtin's registry entry")
	}
	if !strings.HasPrefix(s.SourcePath(), "builtin:") {
		t.Fatalf("conventional-commit must remain the REAL builtin (source %q), never the excluded external skill's content", s.SourcePath())
	}
	if s.Description() == clique {
		t.Fatal("the excluded external skill's description must never have replaced the builtin's own")
	}

	// Sanity: the tie-break winner is genuinely admitted alongside it.
	if _, wok := reg.Get("aaa-winner"); !wok {
		t.Fatal("aaa-winner (the similarity tie-break winner) must be registered")
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
// function internal/clientcore's LoadSkillsAndPlugins and
// applications/terminal_ui/main.go call. (cmd/cli/main.go does NOT call
// LoadSkillsAndDispatcher — it hand-rolls an equivalent [external, legacy,
// project+canonicalUser] sequence directly in buildSubsystems and the
// "skills" subcommand dispatcher; both go through
// agent.LoadExternalTierIntoRegistry for their external-tier step and are
// covered by the same policy engine and the same builtin-shadow fix — see
// commands.SkillLoader.SkipBuiltins's call sites in cmd/cli/main.go.)
// Before the F-01 fix this function loaded every directory (including the
// external tier) unconditionally via commands.SkillLoader — this test
// drives THAT function, not just the policy-gate helper, so a regression
// that re-introduces implied activation at the real call site (rather than
// merely in the helper) is caught.
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

// writeExternalSkillNamed writes dir/<dirName>/SKILL.md whose front-matter
// `name:` is frontMatterName — deliberately DIFFERENT from dirName — so a
// test can reproduce the real constitution/skills corpus shape (e.g. the
// directory `reporting-workable-items` carries `name: Reporting Workable
// Items`). writeExternalSkillWithDescription always writes name == dirName,
// which is exactly the fixture coincidence that let HXC-159 T-P9.01 F-21
// slip past TestLoadExternalTierIntoRegistry_SimilarityConflictPrunesOnlyTheExcluded.
func writeExternalSkillNamed(t *testing.T, dir, dirName, frontMatterName, description string) {
	t.Helper()
	skillDir := filepath.Join(dir, dirName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\nBody for %s.\n", frontMatterName, description, dirName)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadExternalTierIntoRegistry_DuplicateLeafDirNameAcrossDepthsFailsClosed
// closes the HXC-159 T-P9.01 round-5 minor finding (2026-09-23): a
// duplicate leaf-directory-name across nesting depths (skillsFromPolicySet's
// Dir-collision check) drops the WHOLE external tier including unrelated
// skills, but the path had NO test coverage at all — the round-4/round-5
// independent review's paired mutation m4 (removing the check) SURVIVED,
// proving the fail-closed refusal was genuinely untested.
//
// Two skills at DIFFERENT nesting depths sharing the same leaf directory
// basename ("shared") but with DISTINCT, non-conflicting descriptions
// (deliberately, so this test proves the Dir-collision check fires
// specifically — not merely a similarity-conflict exclusion, a different
// code path already covered elsewhere) are both genuinely admitted by the
// policy engine (pkg/skills' Dir is a bare basename, not a full relative
// path, so two skills at different depths CAN collide on it — see
// skillsFromPolicySet's doc comment). commands.SkillRegistry keys every
// packaged skill by directory basename, so this consumer cannot represent
// both — LoadExternalTierIntoRegistry MUST fail closed (return an error,
// register NEITHER skill, including a genuinely-unrelated THIRD skill in
// the same directory) rather than let one silently clobber the other.
//
// This test deliberately does NOT change that whole-tier-refusal behaviour
// itself (a genuine design choice with its own trade-offs — see the task
// report for why it was kept as-is for this remediation pass); it only
// closes the coverage gap the independent review's paired mutation
// surfaced.
func TestLoadExternalTierIntoRegistry_DuplicateLeafDirNameAcrossDepthsFailsClosed(t *testing.T) {
	dir := t.TempDir()
	writeExternalSkillWithDescription(t, dir, "unrelated-third-skill",
		"translate legal contracts between spanish and portuguese dialects")

	// Distinct front-matter `name:` values (hence distinct pkg/skills
	// Qualified() identities — "external.Shared Skill A" /
	// "external.Shared Skill B" — so pkg/skills' own load-time duplicate-
	// qualified-identity check does NOT fire first) but the SAME directory
	// BASENAME ("shared") at two different nesting depths — pkg/skills' Dir
	// is a bare basename, not a full relative path (Skill.Dir's doc
	// comment), so this is exactly the shape that collides on the identity
	// commands.SkillRegistry actually keys on, downstream of pkg/skills'
	// own (Qualified()-based) admission.
	groupADir := filepath.Join(dir, "groupA", "shared")
	if err := os.MkdirAll(groupADir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(groupADir, "SKILL.md"),
		[]byte("---\nname: Shared Skill A\ndescription: compute the exact freight cost for an inbound shipping container\n---\nBody A.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	groupBDir := filepath.Join(dir, "groupB", "shared")
	if err := os.MkdirAll(groupBDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(groupBDir, "SKILL.md"),
		[]byte("---\nname: Shared Skill B\ndescription: reconcile ledger balance entries in a nightly batch job\n---\nBody B.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := commands.NewSkillRegistry()
	err := LoadExternalTierIntoRegistry(reg, dir)
	if err == nil {
		t.Fatal("expected LoadExternalTierIntoRegistry to fail closed on the Dir collision between " +
			"groupA/shared and groupB/shared, got nil error")
	}
	if !strings.Contains(err.Error(), "share directory identity") {
		t.Fatalf("expected the Dir-collision error message, got: %v", err)
	}

	// Fail-closed means the WHOLE tier is refused — not even the genuinely
	// unrelated third skill (in a different, non-colliding directory) is
	// registered.
	if _, ok := reg.Get("unrelated-third-skill"); ok {
		t.Fatal("a Dir-collision anywhere in the tier must refuse the WHOLE tier, " +
			"including a genuinely unrelated skill in a different directory")
	}
	if _, ok := reg.Get("shared"); ok {
		t.Fatal("neither colliding skill may be registered under the collided identity")
	}
}

// TestLoadExternalTierIntoRegistry_PrunesByDirectoryIdentityNotFrontMatterName
// is the HXC-159 T-P9.01 round-4 finding F-21 reproduction (2026-09-23):
// pkg/skills' DialectHelixCode Qualified() identity is the front-matter
// `name:` when present, while commands.SkillRegistry keys every packaged
// skill by its DIRECTORY basename. The F-17 call-site prune keyed
// reg.Remove on the front-matter name, so an excluded skill whose `name:`
// differs from its directory name (4 of the 9 real corpus skills, incl. the
// genuinely-excluded `reporting-workable-items` / `Reporting Workable
// Items`) stayed silently registered and invocable while the log said
// "excluded" — reproduced on the real binary in
// qa-results/hxc159/review/20260923T_review4/22_RED_prune_name_mismatch_real_cli.txt.
//
// RED on the F-21-era pre-fix code: zz-beta (name "Zeta Beta Title") was
// present. GREEN: every policy-excluded skill stays absent under the
// identity the registry actually uses, regardless of what its front-matter
// says, including the registrar's symlinked-directory shape (`zz-link`).
// Since the N1/N2 fix (2026-09-23) this is no longer a PRUNE (LoadExternalTierIntoRegistry
// never loads-then-removes) — it is a direct-conversion NON-ADD of the
// policy-excluded set — but the observable end state this test proves is
// identical: an excluded name is never present in the registry.
func TestLoadExternalTierIntoRegistry_PrunesByDirectoryIdentityNotFrontMatterName(t *testing.T) {
	dir := t.TempDir()
	const clique = "reconcile ledger balance entries nightly batch"
	writeExternalSkillNamed(t, dir, "zz-alpha", "Zeta Alpha Title", clique)
	writeExternalSkillNamed(t, dir, "zz-beta", "Zeta Beta Title", clique)
	writeExternalSkillNamed(t, dir, "zz-gamma", "zz-gamma", clique) // name == dir control
	writeExternalSkillNamed(t, dir, "zz-delta", "zz-delta", "compress seismic waveform telemetry for archival storage")

	// The registrar's real output shape (scripts/register_skills.sh, F-03):
	// a symlinked directory whose target carries a title-cased name.
	target := filepath.Join(t.TempDir(), "linked-target")
	writeExternalSkillNamed(t, filepath.Dir(target), filepath.Base(target), "Linked Title", clique)
	if err := os.Symlink(target, filepath.Join(dir, "zz-link")); err != nil {
		t.Fatal(err)
	}

	reg := commands.NewSkillRegistry()
	if err := LoadExternalTierIntoRegistry(reg, dir); err != nil {
		t.Fatalf("LoadExternalTierIntoRegistry returned an unexpected error: %v", err)
	}
	// Sorted Qualified() order: external.Linked Title, external.Zeta Alpha
	// Title, external.Zeta Beta Title, external.zz-delta, external.zz-gamma
	// -> Linked Title admitted first; Zeta Alpha, Zeta Beta, zz-gamma all
	// conflict with it (score 1.0); zz-delta is unrelated and admits.
	if _, ok := reg.Get("zz-link"); !ok {
		t.Fatal("zz-link (the tie-break winner, symlinked dir) must be registered under its directory identity")
	}
	if _, ok := reg.Get("zz-delta"); !ok {
		t.Fatal("zz-delta (unrelated, non-conflicting) must be registered")
	}
	for _, excluded := range []string{"zz-alpha", "zz-beta", "zz-gamma"} {
		if s, ok := reg.Get(excluded); ok {
			t.Fatalf("F-21: %q was EXCLUDED by the similarity gate but is still registered (source %q) — "+
				"the prune keyed on the front-matter name, not the directory identity the registry uses",
				excluded, s.SourcePath())
		}
	}
	// Of the fixture's own skills, exactly the admitted pair may remain.
	// (LoadExternalTierIntoRegistry only ever Adds the pkg/skills-admitted
	// set directly — since the N1/N2 fix it no longer re-registers the
	// embedded builtin tier as a side effect — so filtering by the "zz-"
	// prefix below is this test's own defensive convention, not a
	// necessity against builtin-name noise.)
	var fixtureNames []string
	for _, s := range reg.List() {
		if len(s.Name()) >= 3 && s.Name()[:3] == "zz-" {
			fixtureNames = append(fixtureNames, s.Name())
		}
	}
	if len(fixtureNames) != 2 {
		t.Fatalf("expected exactly 2 fixture skills registered (zz-link, zz-delta), got %d: %v", len(fixtureNames), fixtureNames)
	}
}
