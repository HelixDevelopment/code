// Package skillconv converts between the mutually-unreadable skill dialects
// in this checkout via a canonical Agent-Skills-open-standard-shaped model.
//
// Dialects (see T-P1.08, spec 001-helixskills-incorporation):
//
//   - HelixCode (helix_code/internal/commands/markdown_skills.go):
//     frontmatter {description, triggers, variables, requires_isolation},
//     skill name from the FILENAME / containing directory.
//   - HelixAgent (submodules/helix_agent/internal/skills/types.go):
//     frontmatter {name, description, allowed-tools, version, license,
//     author, category, tags}, name carried IN the frontmatter.
//   - HelixSkills TOML: upstream corpus not in this checkout — OUT OF SCOPE,
//     blocked on P3 (submodules/skills). No TOML shape is invented here.
//
// Canonical model: Agent Skills open standard core (name, description, version,
// license, body) plus profiled `x-` extension namespaces. The HelixCode
// regex-with-named-captures trigger model has NO standard equivalent and MUST
// land in the `x-helixcode` namespace — never dropped, never merged into the
// Agent trigger-phrase model (see doc comment on HelixCodeProfile).
package skillconv

// CanonicalSchema identifies the canonical model version in JSON output.
const CanonicalSchema = "agent-skills-canonical/v1"

// CanonicalSkill is the dialect-neutral skill. Name, Description and Body are
// the standard core; everything dialect-specific lives under exactly one of
// the profiled namespaces.
type CanonicalSkill struct {
	Schema      string `json:"schema" yaml:"schema"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Version     string `json:"version,omitempty" yaml:"version,omitempty"`
	License     string `json:"license,omitempty" yaml:"license,omitempty"`
	Body        string `json:"body" yaml:"body"`

	XHelixCode  *HelixCodeProfile  `json:"x-helixcode,omitempty" yaml:"x-helixcode,omitempty"`
	XHelixAgent *HelixAgentProfile `json:"x-helixagent,omitempty" yaml:"x-helixagent,omitempty"`

	// seenHelixCode / seenHelixAgent record dialect lineage (in-memory only,
	// never serialised) so cross-dialect carriage is written exactly once:
	// a canonical that has ever passed through HelixCode form never gains an
	// x-helixagent block, and one that has ever passed through HelixAgent
	// form never gains x-helixcode carriage. Without lineage, a
	// HC->Agent->HC trip would accumulate the intermediate's native fields
	// and break convert(convert(x))==x.
	seenHelixCode  bool
	seenHelixAgent bool
}

// HelixCodeProfile carries the HelixCode trigger/variable model.
//
// The regex-with-named-captures trigger model (FindMatching + SubexpNames,
// markdown_skills.go) has no Agent Skills standard equivalent: Agent
// trigger_phrases are plain phrases derived from prose. The two models are
// therefore NEVER merged — triggers ride here as opaque strings, preserved
// byte-for-byte, including patterns that would not compile (the production
// parser skips bad regexes at match time but preserves the raw strings).
type HelixCodeProfile struct {
	Triggers          []string          `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	Variables         map[string]string `json:"variables,omitempty" yaml:"variables,omitempty"`
	RequiresIsolation bool              `json:"requires_isolation,omitempty" yaml:"requires_isolation,omitempty"`
	SourcePath        string            `json:"source_path,omitempty" yaml:"source_path,omitempty"`
	// Extra preserves unknown HelixCode frontmatter keys verbatim
	// (e.g. an Agent-style `+"`name:`"+` key in a trigger-less file).
	Extra map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// Meaningful reports whether the profile carries anything worth writing as
// cross-dialect carriage. An empty profile (description-only skill) writes no
// carriage, so description-only skills convert cleanly with no residue.
func (p *HelixCodeProfile) Meaningful() bool {
	if p == nil {
		return false
	}
	return len(p.Triggers) > 0 || len(p.Variables) > 0 || p.RequiresIsolation
}

// HelixAgentProfile carries the HelixAgent allowance/metadata model plus the
// body-section derivation (Overview, WhenToUse, ...), which is deterministic
// from Body and therefore round-trips by re-derivation.
type HelixAgentProfile struct {
	AllowedTools string   `json:"allowed_tools,omitempty" yaml:"allowed_tools,omitempty"`
	Version      string   `json:"version,omitempty" yaml:"version,omitempty"`
	License      string   `json:"license,omitempty" yaml:"license,omitempty"`
	Author       string   `json:"author,omitempty" yaml:"author,omitempty"`
	Category     string   `json:"category,omitempty" yaml:"category,omitempty"`
	Tags         []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	// TriggerPhrases is DERIVED from Description by the production parser
	// (extractTriggers: `triggers on:` + quoted strings). Stored, never
	// emitted to frontmatter — re-derived on parse.
	TriggerPhrases []string `json:"trigger_phrases,omitempty" yaml:"trigger_phrases,omitempty"`
	// Metadata holds the file's native `metadata:` map MINUS carriage keys.
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Overview      string         `json:"overview,omitempty" yaml:"overview,omitempty"`
	WhenToUse     string         `json:"when_to_use,omitempty" yaml:"when_to_use,omitempty"`
	Instructions  string         `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	Examples      []SkillExample `json:"examples,omitempty" yaml:"examples,omitempty"`
	Prerequisites []string       `json:"prerequisites,omitempty" yaml:"prerequisites,omitempty"`
	Outputs       []string       `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	ErrorHandling []SkillError   `json:"error_handling,omitempty" yaml:"error_handling,omitempty"`
	Resources     []string       `json:"resources,omitempty" yaml:"resources,omitempty"`
	RelatedSkills []string       `json:"related_skills,omitempty" yaml:"related_skills,omitempty"`
	Extra         map[string]any `json:"extra,omitempty" yaml:"extra,omitempty"`
}

// SkillExample mirrors HelixAgent's SkillExample (types.go).
type SkillExample struct {
	Title   string `json:"title" yaml:"title"`
	Request string `json:"request" yaml:"request"`
	Result  string `json:"result" yaml:"result"`
}

// SkillError mirrors HelixAgent's SkillError (types.go).
type SkillError struct {
	Error    string `json:"error" yaml:"error"`
	Cause    string `json:"cause" yaml:"cause"`
	Solution string `json:"solution" yaml:"solution"`
}
