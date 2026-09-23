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
// description-similarity + C5-sandbox-precondition checks — over dir and
// reports the outcome as one of three DISTINCT cases, never collapsed:
//
//  1. dir does not exist (or is empty/""): (nil, nil, nil) — matches the
//     pre-existing "AllSkillsDirs entries are silently skipped when
//     absent" contract; this is not a policy question.
//  2. the policy GENUINELY REFUSES the tier (ceiling exceeded, a
//     similarity conflict, or the C5 sandbox precondition failing for a
//     third-party-tier skill): (nil, <refusal>, nil). Refusal is
//     fail-closed over the WHOLE tier — Registry.Activate's allowlist here
//     is every skill the directory contains, and Activate is all-or-
//     nothing over that list, so a refusal means NONE of the tier's
//     skills may be exposed this load, not a silent partial admission.
//  3. a genuine load/registration error (malformed front-matter, an
//     unreadable file, a duplicate qualified identity): (nil, nil, err).
//
// On success (case where policy admits the tier), active holds the full,
// policy-approved skill set discovered — callers that want the RENDERED
// bodies (not just the policy verdict) still load the directory through
// their own existing skill loader afterward; this function's job is
// solely the ADMIT/REFUSE decision.
func PolicyGateExternalTier(dir string) (active []skillslib.Skill, refusal error, err error) {
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
func policyGateExternalTierWithTrust(dir string, trust skillslib.TrustTier) (active []skillslib.Skill, refusal error, err error) {
	if dir == "" {
		return nil, nil, nil
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		return nil, nil, nil
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
		return nil, nil, fmt.Errorf("external skill tier %q: register source: %w", dir, regErr)
	}
	loaded, loadErr := reg.Load()
	if loadErr != nil {
		return nil, nil, fmt.Errorf("external skill tier %q: load: %w", dir, loadErr)
	}
	if len(loaded) == 0 {
		return nil, nil, nil
	}

	reg.SetSandboxReady(os.Getenv(externalTierSandboxReadyEnvVar) == "true")

	allow := make([]string, 0, len(loaded))
	for _, s := range loaded {
		allow = append(allow, s.Qualified())
	}
	activated, actErr := reg.Activate(allow, 0)
	if actErr != nil {
		// A policy refusal (CeilingExceededError, SimilarityConflictError,
		// ThirdPartyActivationBlockedError, UnknownAllowEntryError) is NOT
		// a load error: the external tier is genuinely, correctly refused —
		// report it as the refusal return, not the err return, so callers
		// can log/skip rather than treating it as an infrastructure fault.
		return nil, actErr, nil
	}
	return activated, nil, nil
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
// adds NOTHING when the tier is refused or the directory is absent/empty —
// it never partially loads a refused tier.
func LoadExternalTierIntoRegistry(reg *commands.SkillRegistry, dir string) error {
	if dir == "" {
		return nil
	}
	active, refusal, err := PolicyGateExternalTier(dir)
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
	return nil
}
