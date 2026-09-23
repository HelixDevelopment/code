package agent

import (
	"fmt"
	"log"
	"os"

	"dev.helix.code/internal/commands"
	skillslib "github.com/HelixDevelopment/skills/pkg/skills"
)

// HXC-159 T-P9.01 finding F-01 remediation.
//
// The review found that neither consumer imported pkg/skills: the P4
// activation policy — allowlist resolution (FR-015), the activation
// ceiling (FR-016, RS-05, T-P2.03's measured DefaultCeiling), the
// description-similarity gate (FR-017, RS-06, T-P2.04's calibrated
// threshold) and the C5 sandbox precondition (T-P7.05.5, wired into
// Registry.Activate by this same review round's F-02 fix) all existed
// ONLY inside pkg/skills — a library nothing linked. helix_code's
// external tier (ExternalSkillsDir, T-P5.05) IMPLIED activation by
// listing whatever scripts/register_skills.sh (or any other installer)
// happened to have linked into .claude/skills/: every file present was
// activated, unconditionally, with none of the above policy ever
// consulted.
//
// This file wires the external tier through a REAL pkg/skills Registry so
// that policy genuinely runs before an external-tier skill reaches this
// consumer's SkillRegistry/dispatcher. Only the EXTERNAL tier is gated
// here — the legacy/canonical user directories and the project directory
// are this consumer's OWN authored/installed skills, not the
// "provisioned by something else" surface F-01 is about.

// externalTierSandboxReadyEnvVar declares whether a REAL (non-skip)
// isolated executor backs this consumer's third-party-tier skill
// execution path (C5 precondition, T-P7.05.5). Config-injected, never
// hardcoded true: helix_code has not wired a real IsolatedExecutor as of
// this fix, so this MUST default to false — Registry.Activate's sandbox
// gate keeps correctly refusing third-party-tier skills until one lands
// (§11.4.6: a hardcoded true here would be exactly the bluff F-02
// closed inside the library).
const externalTierSandboxReadyEnvVar = "HELIXCODE_EXTERNAL_SKILLS_SANDBOX_READY"

// externalTierTrust classifies the external tier for the shared policy
// engine. It is TrustVendored — matching the PRE-EXISTING classification
// this same consumer already reports for the tier in
// internal/skillconformance/report.go's trustForTier("external") — rather
// than TrustThirdParty: the tier's primary provisioner
// (scripts/register_skills.sh, T-P1.04) links constitution-owned
// governance skills by reference, not arbitrary untrusted third-party
// code. A caller that discovers the tier now also carries genuinely
// third-party content (e.g. via a different installer) should register
// its OWN Source with TrustThirdParty explicitly; this function's default
// is deliberately the conservative match to the existing self-report, not
// a blanket "everything here is safe" claim.
const externalTierTrust = skillslib.TrustVendored

// externalTierDialect is DialectHelixCode — the permissive superset this
// consumer's OWN commands.SkillLoader already uses for every tier
// (markdown_skills.go): front-matter `name:` is used when present
// (constitution-owned skills registered by scripts/register_skills.sh
// carry it, e.g. `name: Action Prefix System`) and falls back to the
// directory basename when absent (the T-P5.05 test-fixture convention this
// package's own tests already use, and — measured live 2026-09-23 —
// EVERY existing external-tier test fixture in this package). DialectAgent
// (front-matter `name:` mandatory) was tried first and broke
// TestAllSkillsDirs_ExternalTierIsVisibleAndOverridableByProject's
// pre-existing, still-valid fixture — a real compatibility regression, not
// a test bug: this consumer's external tier legitimately carries both
// shapes and must accept both, exactly as commands.SkillLoader itself does.
const externalTierDialect = skillslib.DialectHelixCode

// PolicyGateExternalTier runs the shared pkg/skills policy engine — source
// registration, Load (now symlink-aware after this same fix round's
// loader.go correction), and Activate's allowlist + activation-ceiling +
// description-similarity (HXC-159 F-17, 2026-09-23: now PER-SKILL partial
// admission, not whole-tier fail-closed) + C5-sandbox-precondition checks —
// over dir and reports the outcome as one of three DISTINCT cases, never
// collapsed:
//
//  1. dir does not exist (or is empty/""): (nil, nil, nil, nil) — matches
//     the pre-existing "AllSkillsDirs entries are silently skipped when
//     absent" contract; this is not a policy question.
//  2. the policy GENUINELY REFUSES THE WHOLE TIER (ceiling exceeded, or
//     the C5 sandbox precondition failing for a third-party-tier skill):
//     (nil, nil, <refusal>, nil). These two checks remain whole-set
//     fail-closed (unchanged by F-17 — see pkg/skills.Registry.Activate's
//     doc comment): a ceiling breach is a resource-budget concern over the
//     WHOLE candidate set, and the sandbox precondition is a readiness gate
//     on the WHOLE admitted set, neither is a pairwise conflict eligible
//     for partial admission.
//  3. a genuine load/registration error (malformed front-matter, an
//     unreadable file, a duplicate qualified identity): (nil, nil, nil, err).
//
// On success (case where policy does not whole-tier-refuse), active holds
// the PARTIAL, policy-approved skill set — a description-similarity
// conflict between two skills in dir no longer refuses the whole tier; it
// excludes only the conflicting pair's alphabetically-later member (per
// pkg/skills.admitBySimilarity's deterministic tie-break), and exclusions
// names every excluded skill + which already-admitted skill it conflicted
// with + the score, for the caller to log. Callers that want the RENDERED
// bodies (not just the policy verdict) still load the directory through
// their own existing skill loader afterward; this function's job is
// solely the ADMIT/EXCLUDE/REFUSE decision.
func PolicyGateExternalTier(dir string) (active []skillslib.Skill, exclusions []skillslib.SimilarityExclusion, refusal error, err error) {
	return policyGateExternalTierWithTrust(dir, externalTierTrust)
}

// policyGateExternalTierWithTrust is PolicyGateExternalTier's implementation,
// parameterised on trust so the C5 sandbox-precondition path (which only
// fires for TrustThirdParty skills) is independently, concretely testable
// from this package — proving the mechanism Registry.Activate invokes is
// genuinely REACHABLE through this consumer's wiring, not merely present in
// the library and trusted by inspection. PolicyGateExternalTier's exported
// contract always uses externalTierTrust; this parameterisation is not
// itself part of the public seam.
func policyGateExternalTierWithTrust(dir string, trust skillslib.TrustTier) (active []skillslib.Skill, exclusions []skillslib.SimilarityExclusion, refusal error, err error) {
	active, exclusions, _, refusal, err = policyGateExternalTierDetailed(dir, trust)
	return active, exclusions, refusal, err
}

// policyGateExternalTierDetailed is policyGateExternalTierWithTrust plus the
// one extra fact LoadExternalTierIntoRegistry needs to ACT on an exclusion:
// excludedDirs[i] is the DIRECTORY basename of exclusions[i]'s skill —
// pkg/skills' Skill.Dir, resolved from the loaded set by Qualified()
// identity.
//
// HXC-159 T-P9.01 round-4 finding F-21 (2026-09-23): the two identity
// schemes differ. pkg/skills' DialectHelixCode Qualified() is
// "external.<front-matter name>" whenever the manifest carries `name:`
// (only falling back to the directory when it does not, loader.go), while
// commands.SkillRegistry keys every packaged skill by its DIRECTORY
// basename (markdown_skills.go: "name = subdir name"). On the real
// constitution/skills corpus 4 of 9 manifests carry a title-cased `name:`
// (`reporting-workable-items` -> "Reporting Workable Items"), so a prune
// keyed on the front-matter name was a silent no-op for exactly those
// skills — reproduced on the real binary and in
// TestLoadExternalTierIntoRegistry_PrunesByDirectoryIdentityNotFrontMatterName.
// Resolving the directory HERE, from the same loaded set the verdict was
// computed over, is the only place both identities are known at once.
func policyGateExternalTierDetailed(dir string, trust skillslib.TrustTier) (active []skillslib.Skill, exclusions []skillslib.SimilarityExclusion, excludedDirs []string, refusal error, err error) {
	if dir == "" {
		return nil, nil, nil, nil, nil
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		return nil, nil, nil, nil, nil
	}

	reg := skillslib.NewRegistry()
	src := skillslib.Source{
		Name:       "external",
		Root:       dir,
		Dialect:    externalTierDialect,
		Precedence: 0,
		Trust:      trust,
	}
	if regErr := reg.RegisterSource(src); regErr != nil {
		return nil, nil, nil, nil, fmt.Errorf("external skill tier %q: register source: %w", dir, regErr)
	}
	loaded, loadErr := reg.Load()
	if loadErr != nil {
		return nil, nil, nil, nil, fmt.Errorf("external skill tier %q: load: %w", dir, loadErr)
	}
	if len(loaded) == 0 {
		return nil, nil, nil, nil, nil
	}

	reg.SetSandboxReady(os.Getenv(externalTierSandboxReadyEnvVar) == "true")

	allow := make([]string, 0, len(loaded))
	dirByQualified := make(map[string]string, len(loaded))
	for _, s := range loaded {
		allow = append(allow, s.Qualified())
		dirByQualified[s.Qualified()] = s.Dir
	}
	activated, excl, actErr := reg.Activate(allow, 0)
	if actErr != nil {
		// A whole-tier policy refusal (CeilingExceededError,
		// ThirdPartyActivationBlockedError, UnknownAllowEntryError) is NOT a
		// load error: the external tier is genuinely, correctly refused —
		// report it as the refusal return, not the err return, so callers
		// can log/skip rather than treating it as an infrastructure fault.
		// (SimilarityConflictError no longer exists per F-17: a similarity
		// conflict is reported via excl below, never actErr.)
		return nil, nil, nil, actErr, nil
	}
	excludedDirs = make([]string, 0, len(excl))
	for _, ex := range excl {
		d, ok := dirByQualified[ex.Excluded]
		if !ok || d == "" {
			// Every exclusion names a member of the allowlist we just built
			// from `loaded`, so this cannot happen unless pkg/skills changes
			// its identity contract — fail closed rather than prune the
			// wrong (or no) skill (§11.4.6, §11.4.201).
			return nil, nil, nil, nil, fmt.Errorf("external skill tier %q: exclusion %q resolves to no loaded skill directory", dir, ex.Excluded)
		}
		excludedDirs = append(excludedDirs, d)
	}
	return activated, excl, excludedDirs, nil, nil
}

// LoadExternalTierIntoRegistry is the SINGLE production seam every front-end
// call site (interactive REPL bootstrap, `skills` CLI subcommand,
// LoadSkillsAndDispatcher) MUST use to load the external tier — it
// centralises the F-01 fix so a future call site cannot re-introduce implied
// activation by hand-rolling its own commands.NewSkillLoader(reg, dir, "")
// call the way cmd/cli/main.go's two REPL/CLI bootstraps independently did
// before this fix (measured live 2026-09-23: `helixcode skills list` showed
// all 21 skills of a synthetic 21-skill external tier — one over
// skillslib.DefaultCeiling — completely unrefused, because those two call
// sites loaded the external tier directly and never went through
// LoadSkillsAndDispatcher/PolicyGateExternalTier at all).
//
// dir is expected to be ExternalSkillsDir() (or a test override of it) —
// callers pass whatever they resolved so a fresh env-var read is not forced
// on every call. reg is the caller's OWN commands.SkillRegistry; this
// function ADDS to it exactly the skills PolicyGateExternalTier admits and
// adds NOTHING when the whole tier is refused (ceiling/sandbox) or the
// directory is absent/empty.
//
// HXC-159 F-17 (2026-09-23): PolicyGateExternalTier now performs PER-SKILL
// (partial) admission on a similarity conflict — active may be a STRICT
// SUBSET of everything dir contains. commands.SkillLoader has no
// directory-scoped filtered-load primitive of its own (Reload() walks and
// registers the WHOLE directory unconditionally, T1.6), so after the
// unconditional load this function PRUNES every policy-excluded skill back
// out of reg via SkillRegistry.Remove — a confusable, policy-refused skill
// is never left silently registered just because it shares a directory
// with an admitted one.
//
// The prune is keyed on the skill's DIRECTORY basename (the identity
// commands.SkillRegistry uses), NOT on pkg/skills' Qualified() front-matter
// name — see policyGateExternalTierDetailed / round-4 finding F-21 for why
// the two differ on the real corpus.
func LoadExternalTierIntoRegistry(reg *commands.SkillRegistry, dir string) error {
	if dir == "" {
		return nil
	}
	active, exclusions, excludedDirs, refusal, err := policyGateExternalTierDetailed(dir, externalTierTrust)
	if err != nil {
		return fmt.Errorf("skill activation: external tier policy gate %q: %w", dir, err)
	}
	if refusal != nil {
		log.Printf("⚠️  skill activation: external tier %q refused by policy: %v", dir, refusal)
		return nil
	}
	if len(active) == 0 {
		return nil
	}
	loader := commands.NewSkillLoader(reg, dir, "")
	if loadErr := loader.Load(); loadErr != nil {
		return fmt.Errorf("skill activation: load %q: %w", dir, loadErr)
	}
	for i, ex := range exclusions {
		log.Printf("⚠️  skill activation: external tier %q: %s", dir, ex.String())
		reg.Remove(excludedDirs[i])
	}
	return nil
}
