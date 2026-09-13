package skillconv

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Carriage keys for the foreign profile inside each dialect's native file.
//   - A HelixCode file carries the Agent profile in an `x-helixagent:` mapping
//     (unknown keys are ignored by the production HC parser, so this is safe).
//   - A HelixAgent file carries the Code profile inside its native `metadata:`
//     map as JSON-encoded strings under the reserved `x-helixcode/` prefix.
const (
	hcAgentExtKey   = "x-helixagent"
	haMetaPrefix    = "x-helixcode/"
	haMetaTriggers  = "x-helixcode/triggers"
	haMetaVariables = "x-helixcode/variables"
	haMetaIsolation = "x-helixcode/requires_isolation"
	haMetaExtra     = "x-helixcode/extra"
)

// HelixCodeFile is the parsed HelixCode dialect shape: skillFrontmatter plus
// the externally-supplied name (from filename / containing directory) plus
// verbatim-preserved unknown keys and optional foreign carriage.
type HelixCodeFile struct {
	Name              string
	Description       string
	Triggers          []string
	Variables         map[string]string
	RequiresIsolation bool
	SourcePath        string
	AgentExt          map[string]any
	Extra             map[string]any
	Body              string
}

// Normalize returns a copy with nil collections replaced by empty ones so
// reflect.DeepEqual is meaningful across parse/emit/parse cycles (YAML
// unmarshal yields nil for absent keys; emit normalises the key set).
func (f *HelixCodeFile) Normalize() *HelixCodeFile {
	out := *f
	out.Triggers = append([]string{}, f.Triggers...)
	out.Variables = map[string]string{}
	for k, v := range f.Variables {
		out.Variables[k] = v
	}
	out.Extra = map[string]any{}
	for k, v := range f.Extra {
		out.Extra[k] = v
	}
	// AgentExt stays nil-able: nil means "no carriage block in source".
	return &out
}

// ParseHelixCode parses raw SKILL.md content in the HelixCode dialect. It
// mirrors parseSkillFile (helix_code/internal/commands/markdown_skills.go):
// frontmatter is required to open with "---\n" and close at "\n---"; without
// the prefix the whole content is the body. Unknown frontmatter keys are
// preserved verbatim in Extra (the production parser ignores them); the
// x-helixagent carriage block is split out into AgentExt.
func ParseHelixCode(raw, name, sourcePath string) (*HelixCodeFile, error) {
	f := &HelixCodeFile{
		Name:       name,
		SourcePath: sourcePath,
		Variables:  map[string]string{},
		Extra:      map[string]any{},
	}
	body := raw
	if strings.HasPrefix(raw, "---\n") {
		rest := raw[4:]
		end := strings.Index(rest, "\n---")
		if end == -1 {
			return nil, fmt.Errorf("skill %s: unterminated frontmatter", name)
		}
		fmBlock := rest[:end]
		body = strings.TrimPrefix(rest[end+4:], "\n")
		var fm map[string]any
		if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
			return nil, fmt.Errorf("skill %s: parse frontmatter: %w", name, err)
		}
		if fm == nil {
			fm = map[string]any{}
		}
		f.Description = scalarToString(fm["description"])
		f.Triggers = coerceStringList(fm["triggers"])
		f.Variables = coerceStringMap(fm["variables"])
		f.RequiresIsolation, _ = fm["requires_isolation"].(bool)
		if ax, ok := fm[hcAgentExtKey].(map[string]any); ok {
			f.AgentExt = ax
		}
		for k, v := range fm {
			switch k {
			case "description", "triggers", "variables", "requires_isolation", hcAgentExtKey:
			default:
				f.Extra[k] = v
			}
		}
	}
	f.Body = strings.TrimSpace(body)
	return f, nil
}

// EmitHelixCode renders the file back to SKILL.md bytes. The frontmatter key
// set is normalised (all four known keys always present); Extra keys and the
// AgentExt carriage block are written back verbatim. Byte-identity with the
// source is explicitly NOT claimed — struct-level round-trip is.
func EmitHelixCode(f *HelixCodeFile) string {
	fm := map[string]any{
		"description":        f.Description,
		"triggers":           append([]string{}, f.Triggers...),
		"variables":          copyStringMap(f.Variables),
		"requires_isolation": f.RequiresIsolation,
	}
	keys := make([]string, 0, len(f.Extra))
	for k := range f.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fm[k] = f.Extra[k]
	}
	if f.AgentExt != nil {
		fm[hcAgentExtKey] = f.AgentExt
	}
	yb, err := yaml.Marshal(fm)
	if err != nil {
		panic(fmt.Sprintf("skillconv: marshal helixcode frontmatter: %v", err))
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.Write(yb)
	sb.WriteString("---\n\n")
	sb.WriteString(f.Body)
	sb.WriteString("\n")
	return sb.String()
}

// HelixCodeToCanonical converts a HelixCode file to the canonical model. The
// trigger/variable/isolation model lands in x-helixcode; the Agent carriage
// (if the file previously passed through Agent form) restores x-helixagent.
func HelixCodeToCanonical(f *HelixCodeFile) *CanonicalSkill {
	c := &CanonicalSkill{
		Schema:      CanonicalSchema,
		Name:        f.Name,
		Description: f.Description,
		Body:        f.Body,
		XHelixCode: &HelixCodeProfile{
			Triggers:          append([]string{}, f.Triggers...),
			Variables:         copyStringMap(f.Variables),
			RequiresIsolation: f.RequiresIsolation,
			SourcePath:        f.SourcePath,
			Extra:             cloneAnyMap(f.Extra),
		},
		seenHelixCode: true,
	}
	if f.AgentExt != nil {
		c.XHelixAgent = agentProfileFromExt(f.AgentExt)
		if c.XHelixAgent.Version != "" {
			c.Version = c.XHelixAgent.Version
		}
		if c.XHelixAgent.License != "" {
			c.License = c.XHelixAgent.License
		}
		c.seenHelixAgent = true
	}
	return c
}

// CanonicalToHelixCode converts back. The x-helixagent block is written ONLY
// when the canonical has never passed through HelixCode form (lineage rule):
// otherwise an HC->Agent->HC trip would accumulate the intermediate's native
// fields and break convert(convert(x))==x.
func CanonicalToHelixCode(c *CanonicalSkill) (*HelixCodeFile, error) {
	if c == nil {
		return nil, fmt.Errorf("skillconv: nil canonical")
	}
	f := &HelixCodeFile{
		Name:        c.Name,
		Description: c.Description,
		Variables:   map[string]string{},
		Extra:       map[string]any{},
		Body:        c.Body,
	}
	if c.XHelixCode != nil {
		f.Triggers = append([]string{}, c.XHelixCode.Triggers...)
		f.Variables = copyStringMap(c.XHelixCode.Variables)
		f.RequiresIsolation = c.XHelixCode.RequiresIsolation
		f.SourcePath = c.XHelixCode.SourcePath
		f.Extra = cloneAnyMap(c.XHelixCode.Extra)
	}
	if c.XHelixAgent != nil && !c.seenHelixCode {
		f.AgentExt = agentProfileToExt(c.XHelixAgent)
	}
	return f, nil
}

// agentProfileFromExt restores an Agent profile from an x-helixagent block.
func agentProfileFromExt(ext map[string]any) *HelixAgentProfile {
	p := &HelixAgentProfile{
		Metadata: map[string]string{},
		Tags:     []string{},
		Extra:    map[string]any{},
	}
	p.AllowedTools = scalarToString(ext["allowed-tools"])
	p.Author = scalarToString(ext["author"])
	p.Category = scalarToString(ext["category"])
	p.Version = scalarToString(ext["version"])
	p.License = scalarToString(ext["license"])
	p.Tags = coerceStringList(ext["tags"])
	if m, ok := ext["metadata"].(map[string]any); ok {
		for k, v := range m {
			p.Metadata[k] = scalarToString(v)
		}
	}
	if x, ok := ext["extra"].(map[string]any); ok {
		p.Extra = cloneAnyMap(x)
	}
	return p
}

// agentProfileToExt serialises an Agent profile to an x-helixagent block,
// omitting zero values so description-only skills leave no residue.
func agentProfileToExt(p *HelixAgentProfile) map[string]any {
	ext := map[string]any{}
	if p.AllowedTools != "" {
		ext["allowed-tools"] = p.AllowedTools
	}
	if p.Author != "" {
		ext["author"] = p.Author
	}
	if p.Category != "" {
		ext["category"] = p.Category
	}
	if p.Version != "" {
		ext["version"] = p.Version
	}
	if p.License != "" {
		ext["license"] = p.License
	}
	if len(p.Tags) > 0 {
		ext["tags"] = append([]string{}, p.Tags...)
	}
	if len(p.Metadata) > 0 {
		m := map[string]any{}
		for k, v := range p.Metadata {
			m[k] = v
		}
		ext["metadata"] = m
	}
	// Agent-native unknown keys ride inside the carriage block so an
	// Agent->HC->Agent trip restores them (they have no HC frontmatter slot).
	if len(p.Extra) > 0 {
		ext["extra"] = cloneAnyMap(p.Extra)
	}
	if len(ext) == 0 {
		return nil
	}
	return ext
}

// cloneAnyMap deep-copies a string-keyed any-map one level (values are
// scalars, lists or nested maps in this corpus; nested maps are shared by
// reference read-only — emit never mutates them).
func cloneAnyMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyStringMap(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
