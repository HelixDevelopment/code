package skillconformance

import (
	"strings"
	"time"

	"dev.helix.code/internal/commands"
)

// DirTierMap names the on-disk directories a real commands.SkillLoader
// was constructed with, so GenerateReport can classify each loaded
// skill's tier from its SourcePath prefix. A tier left blank ("") is
// simply not matched — GenerateReport falls back to "builtin" for any
// skill whose source path matches neither, which is correct for the
// embedded builtin_skills tier (it has no on-disk directory to match).
type DirTierMap struct {
	Project  string
	User     string
	External string
}

// tierFor classifies a loaded skill's source path against t, longest
// (most specific) match first so a nested External dir under Project
// (unlikely, but not forbidden) resolves correctly.
func (t DirTierMap) tierFor(sourcePath string) string {
	type candidate struct {
		dir  string
		tier string
	}
	candidates := []candidate{
		{t.External, "external"},
		{t.Project, "project"},
		{t.User, "user"},
	}
	best := ""
	bestLen := -1
	for _, c := range candidates {
		if c.dir == "" {
			continue
		}
		if strings.HasPrefix(sourcePath, c.dir) && len(c.dir) > bestLen {
			best = c.tier
			bestLen = len(c.dir)
		}
	}
	if best == "" {
		return "builtin"
	}
	return best
}

// trustForTier is this package's OWN, EXPLICITLY-DOCUMENTED policy
// assumption (§11.4.6: commands.Skill carries no trust-tier field at all
// — there is nothing to measure here, so this is a declared inference,
// not a measured fact). builtin/project skills ship with or are
// authored inside this checkout (first-party); user/external skills are
// installed from outside the repo the running binary was built from
// (vendored — the closest existing TrustTier to "installed, not
// authored here").
func trustForTier(tier string) string {
	switch tier {
	case "builtin", "project":
		return "first-party"
	default: // "user", "external"
		return "vendored"
	}
}

// GenerateReport builds a CapabilityManifest from an ALREADY-LOADED
// commands.SkillRegistry (call SkillLoader.Load()/Reload() first). Every
// loader_api_surface flag below is set from a fact measured directly in
// commands/markdown_skills.go and internal/agent/skill_activation.go —
// see the citing comment on each — never assumed.
func GenerateReport(reg *commands.SkillRegistry, tiers DirTierMap) CapabilityManifest {
	list := reg.List()
	reports := make([]SkillReport, 0, len(list))
	for _, s := range list {
		tier := tiers.tierFor(s.SourcePath())
		reports = append(reports, SkillReport{
			Qualified: tier + "." + s.Name(),
			Source:    tier,
			Trust:     trustForTier(tier),
		})
	}
	return CapabilityManifest{
		SchemaVersion: SchemaVersion,
		Consumer:      "helix_code",
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		LoaderAPISurface: LoaderAPISurface{
			// true: 3 directory tiers (built-in/user/project) plus the
			// T-P5.05 external tier are all independently registerable
			// sources (markdown_skills.go's SkillLoader + T-P5.05's
			// agent.AllSkillsDirs 4-tier precedence chain).
			SourceRegistration: true,
			// false: SkillRegistry.Add/Get/Remove/List key SOLELY by
			// bare skill.Name() (markdown_skills.go:250-289) — a
			// same-named skill from two tiers is a silent OVERWRITE
			// (Reload's "later writes to want[name] win" precedence),
			// never a coexisting <source>.<name> pair.
			NamespacedIdentity: false,
			// false: skillFrontmatter (markdown_skills.go) parses
			// description/triggers/variables/requires_isolation only —
			// there is no `capabilities:` field anywhere in this
			// loader's frontmatter schema.
			CapabilityDeclaration: false,
			// false: with no declared-capability field, there is
			// nothing for a call boundary to check against; grep of
			// this module finds no such call boundary either.
			CapabilityEnforcement: false,
			// false: T-P5.05.4 (tasks.md) recorded zero Go
			// implementation of a per-skill activation
			// allowlist/ceiling anywhere in this module as of that
			// finding; unchanged as of this report.
			AllowlistActivation: false,
			// false: no capability audit trail exists (there is
			// nothing to audit — see CapabilityEnforcement above).
			AuditLog: false,
			// false: Skill carries no content-hash field and Reload
			// performs no hash comparison; T-P7.03 pinning has not
			// been wired into this consumer's loader as of this run.
			ContentHashProvenance: false,
			// true: Reload's per-file loop logs a WARN and `continue`s
			// past a failed loadSkillFile — proven live by
			// TestGenerateReport_MalformedSkillIsVisibleNotFatal
			// against a real malformed fixture, not merely inferred
			// from the doc comment.
			MalformedSkillVisibility: true,
		},
		Skills: reports,
	}
}
