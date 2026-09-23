package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"dev.helix.code/internal/commands"
	skillslib "github.com/HelixDevelopment/skills/pkg/skills"
)

// externalTierSkillManifestName mirrors commands.skillManifestName (unexported
// in that package): the exact-case packaged-manifest filename, "SKILL.md".
// Duplicated here as a literal (with this cross-reference comment) rather
// than exporting the other package's internal constant just for this one
// diagnostic check.
const externalTierSkillManifestName = "SKILL.md"

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
// HXC-159 T-P9.01 round-4 findings N1/N2 remediation (2026-09-23): this
// function used to hand dir's ENTIRE contents to a fresh
// commands.SkillLoader (commands.NewSkillLoader(reg, dir, "").Load()) after
// computing the policy verdict, then prune the policy-excluded names back
// out of reg. That second, independent scan does NOT evaluate the same
// scope pkg/skills does:
//   - commands.SkillLoader ALSO registers legacy flat "<name>.md" files
//     (markdown_skills.go's skillManifestName doc comment) — a form
//     pkg/skills' Loader (loader.go's walkForManifests) never discovers at
//     all, at any directory depth, because it only ever looks for an
//     exact-case SKILL.md. Once ANY packaged skill was admitted, every flat
//     file in the directory registered with ZERO ceiling/similarity check
//     (N1 — a policy-gate bypass).
//   - commands.SkillLoader ONLY reads ONE level deep
//     ("<dir>/<name>/SKILL.md"); pkg/skills' Loader recurses arbitrarily
//     deep. A nested skill pkg/skills genuinely admitted (and that could WIN
//     a similarity conflict against a real top-level skill, pruning it) was
//     therefore never actually registered by the shallow rescan — stranding
//     BOTH the nested winner and the top-level loser (N2).
//
// The general fix: this function no longer re-scans dir at all. It converts
// the ALREADY-LOADED, ALREADY-POLICY-EVALUATED `active` set pkg/skills
// itself returns directly into *commands.Skill values (via
// commands.NewSkill, the construction seam built for exactly this) and adds
// ONLY those to reg. This makes pkg/skills' evaluated scope and this
// consumer's loaded scope agree BY CONSTRUCTION — there is no second scanner
// whose scope can drift out of sync with the first, and no post-hoc prune
// step is needed (a policy-excluded skill was never Added in the first
// place, so it can never be pruned from — or accidentally left in, or
// accidentally delete something else out of — the registry). This also
// answers the F-21 reviewer's related, unverified
// builtin-shadow-prune-ordering concern for THIS call site: since no prune
// (reg.Remove) ever runs here any more, a policy-excluded skill sharing a
// name with a builtin can no longer cause this function to delete the
// builtin's registry entry — that removal mechanism no longer exists.
//
// Flat "<name>.md" files anywhere under the external tier are therefore
// NEVER registered from this tier — not merely "evaluated and possibly
// refused": pkg/skills has no concept of that HelixCode-specific legacy
// format at all (it is the Agent Skills open standard, deliberately
// project-not-aware per CONST-051(B); baking a helix_code-only legacy
// convention into the shared library would violate that decoupling
// mandate), and the real external-tier provisioner
// (scripts/register_skills.sh, T-P1.04) only ever produces packaged
// "<name>/SKILL.md" directories (including as symlinks) — flat-file support
// was never a deliberate external-tier capability, it was an incidental
// side effect of reusing the general-purpose commands.SkillLoader
// unconditionally. This function logs (at WARN) any flat "*.md" file it
// finds directly under dir so the drop is loud and diagnosable, never
// silent (§11.4.6) — never a silently-dropped capability.
//
// Registry identity: every admitted skill is keyed by its skillslib.Skill.Dir
// (the skill's own directory basename) — the SAME identity
// commands.SkillRegistry has always used for packaged skills — never by
// pkg/skills' Qualified() front-matter name, which can legitimately differ
// from the directory (round-4 finding F-21). Two admitted skills that would
// collide on that Dir identity (possible in principle: pkg/skills' Dir is a
// bare basename, not a full relative path, so two skills at different
// nesting depths COULD share a leaf directory name) is treated as a fail-
// closed load error rather than letting one silently clobber the other in
// reg (§11.4.6 / §11.4.201) — see dedupeByDirOrError below.
func LoadExternalTierIntoRegistry(reg *commands.SkillRegistry, dir string) error {
	if dir == "" {
		return nil
	}
	skills, err := externalTierSkills(dir)
	if err != nil {
		return err
	}
	for _, s := range skills {
		reg.Add(s)
	}
	return nil
}

// externalTierExtraTier adapts the external skills tier to
// commands.ExtraTier so it can be folded into a commands.SkillLoader's
// Reload sequence at a caller-declared position (HXC-159 T-P9.01 round-5
// NEW-2/NEW-4 remediation, 2026-09-23) — see
// skill_activation.go's loadSkillLoader for where it is wired in. It shares
// externalTierSkills with LoadExternalTierIntoRegistry's own direct,
// one-shot call form so there is exactly ONE implementation of "what does
// the external tier currently admit," never two that can drift apart the
// way N1/N2 originally did.
type externalTierExtraTier struct {
	dir string
}

// Load implements commands.ExtraTier.
func (e externalTierExtraTier) Load() (map[string]*commands.Skill, error) {
	return externalTierSkills(e.dir)
}

// externalTierSkills computes the SAME policy-admitted-and-converted skill
// set LoadExternalTierIntoRegistry adds to a registry directly, keyed by
// registry identity (Skill.Name()), but RETURNS it instead of mutating a
// caller's registry — the shared implementation both
// LoadExternalTierIntoRegistry (the pre-existing direct one-shot
// registration; unchanged observable contract since the N1/N2 fix) and
// externalTierExtraTier (the commands.ExtraTier Reload-integration hook)
// build on.
//
// A whole-tier policy refusal (ceiling exceeded, C5 sandbox precondition)
// is logged and reported as an EMPTY map, never an error — matching
// LoadExternalTierIntoRegistry's pre-existing "refused tier admits
// nothing, does not fail the caller" contract. A genuine infrastructure
// error (malformed front-matter, an unreadable file, a duplicate qualified
// identity, a Dir collision within the admitted set, a manifest hash
// mismatch — see skillsFromPolicySet) is returned as err.
func externalTierSkills(dir string) (map[string]*commands.Skill, error) {
	active, exclusions, _, refusal, err := policyGateExternalTierDetailed(dir, externalTierTrust)
	if err != nil {
		return nil, fmt.Errorf("skill activation: external tier policy gate %q: %w", dir, err)
	}
	if refusal != nil {
		log.Printf("⚠️  skill activation: external tier %q refused by policy: %v", dir, refusal)
		return map[string]*commands.Skill{}, nil
	}
	warnAboutFlatSkillFiles(dir)
	for _, ex := range exclusions {
		log.Printf("⚠️  skill activation: external tier %q: %s", dir, ex.String())
	}
	if len(active) == 0 {
		return map[string]*commands.Skill{}, nil
	}
	built, dupErr := skillsFromPolicySet(dir, active)
	if dupErr != nil {
		return nil, dupErr
	}
	out := make(map[string]*commands.Skill, len(built))
	for _, s := range built {
		out[s.Name()] = s
	}
	return out, nil
}

// skillsFromPolicySet converts pkg/skills' policy-admitted set into
// *commands.Skill values, keyed by directory identity, failing closed on a
// Dir collision within the admitted set rather than silently letting one
// clobber another in the caller's registry.
//
// HXC-159 T-P9.01 round-5 findings NEW-1/NEW-5 remediation (2026-09-23):
// this used to convert pkg/skills' ALREADY-PARSED XHelixCode struct fields
// directly into a *commands.Skill via commands.NewSkill — a construction
// shortcut that bypassed the production YAML parser every OTHER tier
// (built-in, legacy, canonical, project) already uses. pkg/skills' own
// front-matter reader (submodules/skills/pkg/skills/frontmatter.go) is
// DELIBERATELY a minimal, stdlib-only scanner — R-25 forbids a YAML
// dependency in that project-not-aware, dialect-agnostic library — and it
// has two divergences from a real YAML parser that this consumer's
// dialect actively exercises:
//   - list items (frontmatter.go's `triggers:` parsing) are never passed
//     through unquote(): a double-quoted trigger pattern such as
//     `"(?i)^zap (?P<what>\w+) now$"` is stored VERBATIM, quote characters
//     and all, with its `\\w+` escape NEVER unescaped to `\w+` — so the
//     compiled regex requires literal `"` characters around the match and
//     a literal backslash before `w`, and never matches real input
//     (NEW-1; measured on the real corpus: 5 of 7 admitted external-tier
//     skills' triggers broken this way).
//   - `requires_isolation` is read via an exact, case-sensitive string
//     comparison against the literal `"true"`
//     (`scalars["requires_isolation"] == "true"`), so `True`, `TRUE`, or
//     `true # comment` all silently parse to false where a real YAML
//     1.1 boolean reader recognises every one of them (NEW-5).
//
// The fix (the independent reviewer's own recommended direction): the
// policy-admitted set (`active`, unchanged — pkg/skills remains the SOLE
// determinant of WHICH skills get registered) still drives WHAT gets
// registered, but each admitted skill is now built by (1) re-reading its
// OWN manifest file directly from disk, (2) verifying its SHA-256 against
// the Hash pkg/skills computed when it evaluated/admitted the skill
// (refusing on mismatch — a free provenance/tamper-detection check as a
// side benefit: if the file changed on disk between evaluation and this
// conversion, admitting it here would be registering a manifest the
// policy engine never actually evaluated), and (3) parsing it with
// commands.ParseSkillFile — the SAME production yaml.v3-based parser every
// other tier already goes through.
func skillsFromPolicySet(dir string, active []skillslib.Skill) ([]*commands.Skill, error) {
	seen := make(map[string]skillslib.Skill, len(active))
	out := make([]*commands.Skill, 0, len(active))
	for _, s := range active {
		if s.Dir == "" {
			// pkg/skills always sets Dir from filepath.Base of a real,
			// walked directory (loader.go's LoadOne); an empty value here
			// would mean the shared library's identity contract changed
			// beneath this consumer — fail closed rather than register
			// under an empty registry key (§11.4.6, §11.4.201).
			return nil, fmt.Errorf("skill activation: external tier %q: admitted skill %q carries an empty directory identity", dir, s.Qualified())
		}
		if prior, ok := seen[s.Dir]; ok {
			return nil, fmt.Errorf(
				"skill activation: external tier %q: two admitted skills share directory identity %q (%q and %q) — "+
					"commands.SkillRegistry keys by directory basename, so this consumer cannot represent both; refusing rather than silently dropping one",
				dir, s.Dir, prior.Qualified(), s.Qualified())
		}
		seen[s.Dir] = s

		// s.RelPath is the source-root-relative directory of the manifest
		// (pkg/skills' Skill.RelPath doc comment) — e.g. "skills/codex/
		// skill-creator", or "." when the manifest sits directly at dir's
		// own root (filepath.Join collapses "." harmlessly). This is the
		// SAME path pkg/skills itself read SKILL.md from (loader.go's
		// loadDir), so re-reading it here re-reads the IDENTICAL file the
		// policy engine evaluated.
		manifestPath := filepath.Join(dir, s.RelPath, externalTierSkillManifestName)
		raw, readErr := os.ReadFile(manifestPath)
		if readErr != nil {
			return nil, fmt.Errorf("skill activation: external tier %q: re-read admitted skill %q manifest %q: %w",
				dir, s.Qualified(), manifestPath, readErr)
		}
		sum := sha256.Sum256(raw)
		gotHash := hex.EncodeToString(sum[:])
		if gotHash != s.Hash {
			// The manifest changed on disk between pkg/skills' evaluation
			// and this conversion (or the two hash computations otherwise
			// disagree) — admitting it here would register a manifest the
			// policy engine never actually evaluated, a policy-gate bypass
			// in its own right. Fail closed (§11.4.6, §11.4.201, §11.4.252).
			return nil, fmt.Errorf(
				"skill activation: external tier %q: admitted skill %q manifest %q changed since policy evaluation "+
					"(hash %s at evaluation, %s now) — refusing to register a manifest the policy engine never actually evaluated",
				dir, s.Qualified(), manifestPath, s.Hash, gotHash)
		}

		skill, parseErr := commands.ParseSkillFile(s.Dir, string(raw), fmt.Sprintf("external:%s", manifestPath))
		if parseErr != nil {
			return nil, fmt.Errorf("skill activation: external tier %q: parse admitted skill %q manifest %q: %w",
				dir, s.Qualified(), manifestPath, parseErr)
		}
		out = append(out, skill)
	}
	return out, nil
}

// warnAboutFlatSkillFiles logs (at WARN) every legacy flat "<name>.md" file
// found directly under dir — the on-disk form the external tier no longer
// registers at all (see LoadExternalTierIntoRegistry's doc comment). A
// read error here is not itself fatal to loading the tier's genuinely
// evaluated skills; it is logged and the function returns, matching the
// existing per-directory WARN-and-continue philosophy used throughout this
// package.
func warnAboutFlatSkillFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == externalTierSkillManifestName {
			continue
		}
		log.Printf("⚠️  skill activation: external tier %q: legacy flat skill file %q is NOT supported in the policy-gated external tier "+
			"(pkg/skills only evaluates packaged \"<name>/SKILL.md\" directories) — this file will NOT be loaded; "+
			"convert it to the packaged form if it needs to be active", dir, entry.Name())
	}
}
