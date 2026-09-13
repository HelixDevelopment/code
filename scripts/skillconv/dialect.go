package skillconv

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Dialect identifies which frontmatter shape a SKILL.md file uses.
type Dialect string

const (
	// DialectHelixCode: frontmatter {description, triggers, variables,
	// requires_isolation}; name from filename / containing directory.
	DialectHelixCode Dialect = "helixcode"
	// DialectHelixAgent: frontmatter {name, description, allowed-tools,
	// version, license, author, category, tags, metadata}; name in-file.
	DialectHelixAgent Dialect = "helixagent"
)

// DetectDialect inspects a SKILL.md document's frontmatter keys and returns
// the dialect whose parser can read it without loss. Deterministic,
// closed-set rule:
//
//  1. frontmatter carries name:/allowed-tools: → HelixAgent. The `name:` key
//     is the Agent identity slot; 40 in-tree Agent files additionally carry
//     a `triggers:` key holding a SLASH-COMMAND list (e.g. `/monitoring`),
//     not HC regexes — it rides verbatim in Extra and is never interpreted
//     as a regex model (GAP-06).
//  2. else frontmatter carries triggers:/variables:/requires_isolation:
//     → HelixCode (the regex-trigger model exists only there).
//  3. else (description-only or no frontmatter at all) → HelixCode by
//     default: the name-from-directory rule still yields an addressable
//     skill, while the Agent shape would leave Name empty.
//
// CORRECTION TO THE T-P1.08 SURVEY (§11.4.6: files beat premises): the survey
// asserted constitution/skills/*/SKILL.md is uniformly HelixCode-shaped. The
// files refute it — 5 of 8 carry name:/version: with no triggers: (Agent
// shape) and 3 carry no frontmatter at all. Classification is therefore
// per-FILE by this rule, never per-directory. Measured 2026-09-13:
// helixcode=11 (8 builtin + 3 frontmatter-less), helixagent=1179
// (1174 helix_agent + 5 constitution).
func DetectDialect(raw string) Dialect {
	fm := frontmatterKeys(raw)
	if fm["name"] || fm["allowed-tools"] {
		return DialectHelixAgent
	}
	if fm["triggers"] || fm["variables"] || fm["requires_isolation"] {
		return DialectHelixCode
	}
	return DialectHelixCode
}

// frontmatterKeys returns the top-level frontmatter key set. Files without
// frontmatter, or with unparseable frontmatter, yield an empty set (which
// DetectDialect maps to the HelixCode default).
func frontmatterKeys(raw string) map[string]bool {
	out := map[string]bool{}
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return out
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &fm); err != nil {
		return out
	}
	for k := range fm {
		out[k] = true
	}
	return out
}
