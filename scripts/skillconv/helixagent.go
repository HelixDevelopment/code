package skillconv

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Section/trigger extraction below is transcribed (read-only) from
// submodules/helix_agent/internal/skills/parser.go so skillconv derives
// exactly what the production parser derives. skillconv never modifies that
// file; any divergence found by the corpus round-trip is reported as a named
// gap, never silently absorbed.

var (
	haQuotedRegex   = regexp.MustCompile(`"([^"]+)"`)
	haExampleRegex  = regexp.MustCompile(`\*\*Example:?\s*([^*]*)\*\*\s*\n([^*]+)`)
	haCategoryRegex = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	haTagRegex      = regexp.MustCompile(`Tags?:\s*([^\n]+)`)
	haTriggerRegex  = regexp.MustCompile(`(?i)triggers?\s+on:?\s*([^.]+)`)
)

// Known HelixAgent frontmatter keys. Anything else is foreign (e.g. a
// HelixCode `triggers:` list in devops/* files) and preserved verbatim in
// Extra. NOTE: `tags` is known-but-shadowed: the production parser overwrites
// frontmatter tags with body-derived tags (GAP-02).
var haKnownKeys = map[string]bool{
	"name": true, "description": true, "allowed-tools": true,
	"version": true, "license": true, "author": true,
	"category": true, "tags": true, "metadata": true,
}

// HelixAgentFile is the parsed HelixAgent dialect shape.
type HelixAgentFile struct {
	Name         string
	Description  string
	AllowedTools string
	Version      string
	License      string
	Author       string
	Category     string
	Tags         []string
	Metadata     map[string]string
	// CodeCarriage holds the HelixCode profile decoded from metadata
	// x-helixcode/* keys (nil when the file never passed through HC form).
	CodeCarriage *HelixCodeProfile
	Extra        map[string]any
	Body         string

	// catNative records whether Category came from frontmatter (true) or was
	// derived from the path (false). Derived categories are never emitted:
	// they re-derive on parse, so emitting them would accumulate residue on
	// cross-dialect trips (HC->Agent materialises "uncategorized", which must
	// not flow back into the HC file's x-helixagent block).
	catNative bool

	TriggerPhrases []string
	Overview       string
	WhenToUse      string
	Instructions   string
	Examples       []SkillExample
	Prerequisites  []string
	Outputs        []string
	ErrorHandling  []SkillError
	Resources      []string
	RelatedSkills  []string
	SourcePath     string
}

// Normalize returns a copy with nil collections replaced by empty ones (see
// HelixCodeFile.Normalize). CodeCarriage stays nil-able.
func (f *HelixAgentFile) Normalize() *HelixAgentFile {
	out := *f
	out.Tags = append([]string{}, f.Tags...)
	out.Metadata = map[string]string{}
	for k, v := range f.Metadata {
		out.Metadata[k] = v
	}
	out.Extra = map[string]any{}
	for k, v := range f.Extra {
		out.Extra[k] = v
	}
	out.TriggerPhrases = append([]string{}, f.TriggerPhrases...)
	out.Examples = append([]SkillExample{}, f.Examples...)
	out.Prerequisites = append([]string{}, f.Prerequisites...)
	out.Outputs = append([]string{}, f.Outputs...)
	out.ErrorHandling = append([]SkillError{}, f.ErrorHandling...)
	out.Resources = append([]string{}, f.Resources...)
	out.RelatedSkills = append([]string{}, f.RelatedSkills...)
	if out.CodeCarriage != nil {
		cc := *out.CodeCarriage
		cc.Triggers = append([]string{}, cc.Triggers...)
		cc.Variables = map[string]string{}
		for k, v := range out.CodeCarriage.Variables {
			cc.Variables[k] = v
		}
		cc.Extra = map[string]any{}
		for k, v := range out.CodeCarriage.Extra {
			cc.Extra[k] = v
		}
		out.CodeCarriage = &cc
	}
	return &out
}

// ParseHelixAgent parses raw SKILL.md content in the HelixAgent dialect,
// mirroring Parser.Parse: YAML frontmatter (may be absent), raw body kept,
// TriggerPhrases derived from Description, sections derived from Body,
// Category from frontmatter-or-path, Tags from Body (shadowing frontmatter).
// Body margins are trimmed (GAP-N1: interior bytes verbatim).
func ParseHelixAgent(raw, sourcePath string) (*HelixAgentFile, error) {
	f := &HelixAgentFile{
		SourcePath: sourcePath,
		Metadata:   map[string]string{},
		Extra:      map[string]any{},
	}
	fmText, body := splitHAFrontmatter(raw)
	if fmText != "" {
		var fm map[string]any
		if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
			return nil, fmt.Errorf("parse frontmatter: %w", err)
		}
		f.Name = scalarToString(fm["name"])
		f.Description = scalarToString(fm["description"])
		f.AllowedTools = coerceAllowedTools(fm["allowed-tools"])
		f.Version = scalarToString(fm["version"])
		f.License = scalarToString(fm["license"])
		f.Author = scalarToString(fm["author"])
		if c, ok := fm["category"]; ok && scalarToString(c) != "" {
			f.Category = scalarToString(c)
			f.catNative = true
		} else {
			f.Category = extractHACategory(sourcePath)
		}
		if m, ok := fm["metadata"].(map[string]any); ok {
			for k, v := range m {
				f.Metadata[k] = scalarToString(v)
			}
		}
		for k, v := range fm {
			if !haKnownKeys[k] {
				f.Extra[k] = v
			}
		}
	} else {
		f.Category = extractHACategory(sourcePath)
	}
	f.Body = strings.TrimSpace(body)
	f.TriggerPhrases = extractHATriggers(f.Description)
	parseHASections(f, f.Body)
	f.Tags = extractHATags(f.Body)
	f.CodeCarriage = decodeHACarriage(f.Metadata)
	return f, nil
}

// splitHAFrontmatter mirrors Parser.splitFrontmatter: first line must be
// `---` (trimmed), frontmatter ends at the next trimmed `---` line.
// Absent frontmatter yields ("", whole content, nil-equivalent).
func splitHAFrontmatter(content string) (fm, body string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", content
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return "", content // unterminated: production errors; skillconv keeps content (GAP-07)
	}
	return strings.Join(lines[1:end], "\n"), strings.Join(lines[end+1:], "\n")
}

// EmitHelixAgent renders the file back to SKILL.md bytes. Derived fields
// (TriggerPhrases, sections, Tags) are never written — they re-derive on
// parse. Category is materialised (derived-or-native converges after one
// pass). The HC carriage is re-injected into metadata as JSON strings.
func EmitHelixAgent(f *HelixAgentFile) string {
	fm := map[string]any{
		"name":        f.Name,
		"description": f.Description,
	}
	if f.AllowedTools != "" {
		fm["allowed-tools"] = f.AllowedTools
	}
	if f.Version != "" {
		fm["version"] = f.Version
	}
	if f.License != "" {
		fm["license"] = f.License
	}
	if f.Author != "" {
		fm["author"] = f.Author
	}
	if f.Category != "" && f.catNative {
		fm["category"] = f.Category
	}
	meta := map[string]any{}
	keys := make([]string, 0, len(f.Metadata))
	for k := range f.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		meta[k] = f.Metadata[k]
	}
	encodeHACarriage(meta, f.CodeCarriage)
	if len(meta) > 0 {
		fm["metadata"] = meta
	}
	xkeys := make([]string, 0, len(f.Extra))
	for k := range f.Extra {
		xkeys = append(xkeys, k)
	}
	sort.Strings(xkeys)
	for _, k := range xkeys {
		fm[k] = f.Extra[k]
	}
	yb, err := yaml.Marshal(fm)
	if err != nil {
		panic(fmt.Sprintf("skillconv: marshal helixagent frontmatter: %v", err))
	}
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.Write(yb)
	sb.WriteString("---\n\n")
	sb.WriteString(f.Body)
	sb.WriteString("\n")
	return sb.String()
}

// HelixAgentToCanonical converts an Agent file to the canonical model. HC
// carriage decoded from metadata restores x-helixcode.
func HelixAgentToCanonical(f *HelixAgentFile) *CanonicalSkill {
	c := &CanonicalSkill{
		Schema:      CanonicalSchema,
		Name:        f.Name,
		Description: f.Description,
		Version:     f.Version,
		License:     f.License,
		Body:        f.Body,
		XHelixAgent: &HelixAgentProfile{
			AllowedTools:   f.AllowedTools,
			Version:        f.Version,
			License:        f.License,
			Author:         f.Author,
			Category:       nativeOrEmpty(f.Category, f.catNative),
			Tags:           append([]string{}, f.Tags...),
			TriggerPhrases: append([]string{}, f.TriggerPhrases...),
			Metadata:       map[string]string{},
			Overview:       f.Overview,
			WhenToUse:      f.WhenToUse,
			Instructions:   f.Instructions,
			Examples:       append([]SkillExample{}, f.Examples...),
			Prerequisites:  append([]string{}, f.Prerequisites...),
			Outputs:        append([]string{}, f.Outputs...),
			ErrorHandling:  append([]SkillError{}, f.ErrorHandling...),
			Resources:      append([]string{}, f.Resources...),
			RelatedSkills:  append([]string{}, f.RelatedSkills...),
			Extra:          map[string]any{},
		},
		seenHelixAgent: true,
	}
	for k, v := range f.Metadata {
		if !strings.HasPrefix(k, haMetaPrefix) {
			c.XHelixAgent.Metadata[k] = v
		}
	}
	for k, v := range f.Extra {
		c.XHelixAgent.Extra[k] = v
	}
	if f.CodeCarriage != nil {
		c.XHelixCode = &HelixCodeProfile{
			Triggers:          append([]string{}, f.CodeCarriage.Triggers...),
			Variables:         map[string]string{},
			RequiresIsolation: f.CodeCarriage.RequiresIsolation,
			Extra:             cloneAnyMap(f.CodeCarriage.Extra),
		}
		for k, v := range f.CodeCarriage.Variables {
			c.XHelixCode.Variables[k] = v
		}
	}
	return c
}

// CanonicalToHelixAgent converts back. x-helixcode carriage is injected into
// metadata ONLY when the canonical carries a meaningful HC profile AND has
// never passed through Agent form (lineage rule, mirror of the HC side).
func CanonicalToHelixAgent(c *CanonicalSkill) (*HelixAgentFile, error) {
	if c == nil {
		return nil, fmt.Errorf("skillconv: nil canonical")
	}
	f := &HelixAgentFile{
		Name:        c.Name,
		Description: c.Description,
		Metadata:    map[string]string{},
		Extra:       map[string]any{},
		Body:        c.Body,
	}
	if c.XHelixAgent != nil {
		f.AllowedTools = c.XHelixAgent.AllowedTools
		f.Author = c.XHelixAgent.Author
		f.Category = c.XHelixAgent.Category
		f.Version = c.XHelixAgent.Version
		if f.Version == "" {
			f.Version = c.Version
		}
		f.License = c.XHelixAgent.License
		if f.License == "" {
			f.License = c.License
		}
		for k, v := range c.XHelixAgent.Metadata {
			f.Metadata[k] = v
		}
		for k, v := range c.XHelixAgent.Extra {
			f.Extra[k] = v
		}
	}
	if c.XHelixCode != nil && (c.XHelixCode.Meaningful() || len(c.XHelixCode.Extra) > 0) && !c.seenHelixAgent {
		f.CodeCarriage = &HelixCodeProfile{
			Triggers:          append([]string{}, c.XHelixCode.Triggers...),
			Variables:         map[string]string{},
			RequiresIsolation: c.XHelixCode.RequiresIsolation,
			Extra:             cloneAnyMap(c.XHelixCode.Extra),
		}
		for k, v := range c.XHelixCode.Variables {
			f.CodeCarriage.Variables[k] = v
		}
	}
	f.catNative = f.Category != ""
	// Re-derive exactly as the parser would, so the struct is parse-identical.
	if f.Category == "" {
		f.Category = "uncategorized"
	}
	f.TriggerPhrases = extractHATriggers(f.Description)
	parseHASections(f, f.Body)
	f.Tags = extractHATags(f.Body)
	return f, nil
}

// decodeHACarriage extracts x-helixcode/* JSON values from metadata. Native
// metadata keys pass through untouched.
func decodeHACarriage(meta map[string]string) *HelixCodeProfile {
	has := false
	for k := range meta {
		if strings.HasPrefix(k, haMetaPrefix) {
			has = true
			break
		}
	}
	if !has {
		return nil
	}
	cc := &HelixCodeProfile{Variables: map[string]string{}}
	if s, ok := meta[haMetaTriggers]; ok && s != "" {
		var t []string
		if err := json.Unmarshal([]byte(s), &t); err == nil {
			cc.Triggers = t
		}
	}
	if s, ok := meta[haMetaVariables]; ok && s != "" {
		var m map[string]string
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			cc.Variables = m
		}
	}
	if s, ok := meta[haMetaIsolation]; ok && s != "" {
		cc.RequiresIsolation = s == "true"
	}
	// HC-native unknown keys ride as a JSON object (GAP-04: JSON-typed;
	// string values — the only in-tree instances — are stable).
	if s, ok := meta[haMetaExtra]; ok && s != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			cc.Extra = m
		}
	}
	if cc.Extra == nil {
		cc.Extra = map[string]any{}
	}
	return cc
}

// encodeHACarriage injects the HC profile into the metadata map as JSON
// strings. A nil/empty profile writes nothing (no residue).
func encodeHACarriage(meta map[string]any, cc *HelixCodeProfile) {
	if cc == nil || (!cc.Meaningful() && len(cc.Extra) == 0) {
		return
	}
	tb, _ := json.Marshal(append([]string{}, cc.Triggers...))
	vb, _ := json.Marshal(cc.Variables)
	meta[haMetaTriggers] = string(tb)
	meta[haMetaVariables] = string(vb)
	meta[haMetaIsolation] = strconv.FormatBool(cc.RequiresIsolation)
	if len(cc.Extra) > 0 {
		if eb, err := json.Marshal(cc.Extra); err == nil {
			meta[haMetaExtra] = string(eb)
		}
	}
}

// coerceAllowedTools: the production type is a string, but the wild corpus
// contains flow sequences (`allowed-tools: ['a', 'b']`, GAP-01). Join those
// with ", " — content preserved, YAML shape normalised (named, never silent).
func coerceAllowedTools(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s := scalarToString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return scalarToString(v)
	}
}

// --- transcribed derivation helpers (parser.go) ---

func extractHATriggers(description string) []string {
	triggers := []string{}
	if m := haTriggerRegex.FindStringSubmatch(description); len(m) > 1 {
		for _, part := range strings.Split(m[1], ",") {
			tr := strings.Trim(strings.TrimSpace(part), `"'`)
			if tr != "" {
				triggers = append(triggers, strings.ToLower(tr))
			}
		}
	}
	for _, m := range haQuotedRegex.FindAllStringSubmatch(description, -1) {
		if len(m) > 1 {
			tr := strings.ToLower(strings.TrimSpace(m[1]))
			if tr != "" && !containsString(triggers, tr) {
				triggers = append(triggers, tr)
			}
		}
	}
	return triggers
}

func parseHASections(f *HelixAgentFile, body string) {
	lines := strings.Split(body, "\n")
	current := ""
	var sb strings.Builder
	flush := func() {
		saveHASection(f, current, sb.String())
		sb.Reset()
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = strings.TrimPrefix(line, "## ")
		} else {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	flush()
}

func saveHASection(f *HelixAgentFile, section, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	switch s := strings.ToLower(section); {
	case strings.Contains(s, "overview"):
		f.Overview = content
	case strings.Contains(s, "when to use"):
		f.WhenToUse = content
	case strings.Contains(s, "instruction"):
		f.Instructions = content
	case strings.Contains(s, "example"):
		f.Examples = parseHAExamples(content)
	case strings.Contains(s, "prerequisite"):
		f.Prerequisites = parseHAList(content)
	case strings.Contains(s, "output"):
		f.Outputs = parseHAList(content)
	case strings.Contains(s, "error"):
		f.ErrorHandling = parseHAErrorTable(content)
	case strings.Contains(s, "resource"):
		f.Resources = parseHAList(content)
	case strings.Contains(s, "related"):
		f.RelatedSkills = parseHARelated(content)
	}
}

func parseHAExamples(content string) []SkillExample {
	out := []SkillExample{}
	for _, m := range haExampleRegex.FindAllStringSubmatch(content, -1) {
		if len(m) <= 2 {
			continue
		}
		ex := SkillExample{Title: strings.TrimSpace(m[1])}
		for i, line := range strings.Split(m[2], "\n") {
			if strings.HasPrefix(line, "Request:") {
				ex.Request = strings.TrimSpace(strings.TrimPrefix(line, "Request:"))
			} else if strings.HasPrefix(line, "Result:") {
				result := strings.TrimPrefix(line, "Result:")
				rest := strings.Split(m[2], "\n")
				for j := i + 1; j < len(rest); j++ {
					if strings.TrimSpace(rest[j]) == "" {
						break
					}
					result += " " + strings.TrimSpace(rest[j])
				}
				ex.Result = strings.TrimSpace(result)
			}
		}
		out = append(out, ex)
	}
	return out
}

func parseHAList(content string) []string {
	out := []string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		} else if strings.HasPrefix(line, "* ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "* ")))
		}
	}
	return out
}

func parseHAErrorTable(content string) []SkillError {
	out := []SkillError{}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "|") && !strings.Contains(line, "---") &&
			!strings.Contains(strings.ToLower(line), "error") {
			parts := strings.Split(line, "|")
			if len(parts) >= 4 {
				se := SkillError{
					Error:    strings.TrimSpace(parts[1]),
					Cause:    strings.TrimSpace(parts[2]),
					Solution: strings.TrimSpace(parts[3]),
				}
				if se.Error != "" {
					out = append(out, se)
				}
			}
		}
	}
	return out
}

func parseHARelated(content string) []string {
	out := []string{}
	for _, m := range haCategoryRegex.FindAllStringSubmatch(content, -1) {
		if len(m) > 1 {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

func extractHACategory(filePath string) string {
	dir := filepath.Dir(filePath)
	parts := strings.Split(dir, string(filepath.Separator))
	for i, part := range parts {
		if part == "skills" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "uncategorized"
}

func extractHATags(content string) []string {
	out := []string{}
	if m := haTagRegex.FindStringSubmatch(content); len(m) > 1 {
		for _, part := range strings.Split(m[1], ",") {
			if tag := strings.TrimSpace(part); tag != "" {
				out = append(out, tag)
			}
		}
	}
	return out
}

// nativeOrEmpty keeps effective values out of the canonical profile when they
// were derived rather than authored (see catNative).
func nativeOrEmpty(v string, native bool) string {
	if native {
		return v
	}
	return ""
}

func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
