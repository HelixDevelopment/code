package commands

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// builtinSkillsFS holds the skill manifests bundled into the binary. Each
// immediate subdirectory of builtin_skills/ that contains a SKILL.md is loaded
// as a built-in skill named after that subdirectory. These form the
// lowest-precedence tier: a user or project skill of the same name overrides
// the built-in (see SkillLoader.Reload). The embedded tree always ships with
// the binary, so built-in skills work out of the box with no on-disk files.
//
// The SKILL.md content under builtin_skills/ is a COPY, not a symlink
// (//go:embed cannot follow a link outside the module tree). 7 of these 8
// entries share a directory name with a constitution/skills/<name>/ entry
// and ARE GENERATED FROM IT (HXC-159 T-P9.01 finding F-13, resolved
// 2026-09-23 via Option C — a superset frontmatter schema): the
// constitution side is the canonical Claude-Code-native Agent Skill doc
// (`name`/`description`/`version`), extended with two OPTIONAL fields
// (`triggers`/`requires_isolation`) this loader itself parses (see
// skillFrontmatter below) but that Claude Code's own native skill
// discovery, and every other Agent-Skills-format consumer, simply ignores
// (unknown YAML keys are dropped, not rejected). constitution/skills/ is
// therefore the single source of truth for BOTH facets: Claude Code's own
// semantic-description-driven discovery AND this loader's REPL
// regex-trigger auto-completion. Running `scripts/generate_builtin_skills.sh`
// (no flags) copies each constitution-sourced skill's SKILL.md verbatim
// into builtin_skills/ and is now SAFE — TestGovernanceSkills_RegisteredAndInvocable
// stays green because the source frontmatter carries the same `triggers:`
// this loader's auto-trigger matcher depends on. `--check` (also wired via
// `go:generate` below, and via TestBuiltinSkills_NoDrift as an always-on
// regression guard) reports drift without mutating anything.
// `conventional-commit` has no constitution/skills/ counterpart at all and
// is HelixCode's own built-in skill unconditionally.
//
//go:generate bash ../../scripts/generate_builtin_skills.sh --check
//go:embed builtin_skills
var builtinSkillsFS embed.FS

// builtinSkillsRoot is the directory prefix inside builtinSkillsFS.
const builtinSkillsRoot = "builtin_skills"

// Skill is an agent-invoked Markdown skill loaded from .helix/skills/*.md.
type Skill struct {
	name              string
	description       string
	body              string
	variables         map[string]string
	triggerPatterns   []string         // raw strings (preserved for /skills show)
	triggers          []*regexp.Regexp // compiled (bad regex skipped at parse time)
	requiresIsolation bool
	sourcePath        string
}

// skillFrontmatter is the YAML shape of a skill's frontmatter block.
type skillFrontmatter struct {
	Description       string            `yaml:"description"`
	Triggers          []string          `yaml:"triggers"`
	Variables         map[string]string `yaml:"variables"`
	RequiresIsolation bool              `yaml:"requires_isolation"`
}

// Name returns the skill name (derived from filename, without extension).
func (s *Skill) Name() string { return s.name }

// Description returns the description parsed from frontmatter.
func (s *Skill) Description() string { return s.description }

// SourcePath returns the .md file path this skill was loaded from.
func (s *Skill) SourcePath() string { return s.sourcePath }

// RequiresIsolation reports whether the skill requests sandbox isolation.
func (s *Skill) RequiresIsolation() bool { return s.requiresIsolation }

// Body returns the raw (unrendered) body string of the skill.
func (s *Skill) Body() string { return s.body }

// TriggerPatterns returns a copy of the raw trigger regex strings from frontmatter.
func (s *Skill) TriggerPatterns() []string {
	out := make([]string, len(s.triggerPatterns))
	copy(out, s.triggerPatterns)
	return out
}

// SkillOptions holds the fields needed to construct a *Skill without a
// filesystem round-trip. It is the input to NewSkill (T-P5.01, HXC-159 Phase
// P5) — the exported construction seam for callers outside this package (an
// external skill source such as the T-P5.05 external tier, a converter, an
// in-memory fixture) that need to build a *Skill and register it via
// SkillRegistry.Add without ever writing a SKILL.md to disk first.
//
// Field semantics deliberately mirror skillFrontmatter / parseSkillFile so a
// Skill built via NewSkill is indistinguishable, at every exported method,
// from one loaded from a file:
//   - Variables defaults to a non-nil empty map when nil (parseSkillFile's
//     zero-value initialisation).
//   - TriggerPatterns are compiled the same way parseSkillFile compiles
//     meta.Triggers: an invalid regex is silently skipped from the compiled
//     set (never a construction error) while the raw pattern string is
//     preserved for introspection via Skill.TriggerPatterns.
//   - Body is trimmed of leading/trailing whitespace exactly as
//     parseSkillFile trims the post-frontmatter body.
type SkillOptions struct {
	// Name is the skill's registry key. Required — see NewSkill's validation.
	Name string
	// Description is the human-readable summary parsed from frontmatter on
	// disk-loaded skills; here it is supplied directly.
	Description string
	// Body is the raw (unrendered) skill body, the frontmatter-delimiter-free
	// text a disk-loaded skill carries after its closing "---".
	Body string
	// Variables are the skill's default substitution variables. Nil is
	// treated as empty (never a construction error).
	Variables map[string]string
	// TriggerPatterns are raw regex strings compiled the same way disk-loaded
	// trigger patterns are; an entry that fails to compile is silently
	// dropped from matching but preserved for introspection.
	TriggerPatterns []string
	// RequiresIsolation mirrors skillFrontmatter's requires_isolation field.
	RequiresIsolation bool
	// SourcePath is an opaque provenance string (SourcePath()). Disk-loaded
	// skills carry a real filesystem path or a "builtin:" prefix; an
	// in-memory skill should carry whatever identifies its true origin (e.g.
	// an external-tier directory entry) so diagnostics remain honest about
	// where the skill actually came from — never a filesystem path that does
	// not exist.
	SourcePath string
}

// NewSkill constructs a *Skill entirely in memory from opts — no filesystem
// round-trip. It is the exported counterpart to the unexported
// parseSkillFile: parseSkillFile builds a Skill FROM raw on-disk frontmatter
// text; NewSkill builds one from already-structured fields, for callers that
// have (or produce) that structure without ever having written a SKILL.md.
//
// NewSkill constructs THROUGH validation rather than widening the type's
// invariants: it enforces the one invariant every disk-loaded skill already
// satisfies unconditionally (a non-empty Name — parseSkillFile has no code
// path that produces an empty one, since the name always comes from a
// filename or directory basename), and it otherwise reproduces
// parseSkillFile's exact field semantics (see SkillOptions) so a
// NewSkill-built Skill cannot be distinguished, by any exported method or by
// SkillRegistry/SkillDispatcher behaviour, from one loaded from disk.
// Skill's fields themselves remain unexported; this function is the only
// production entry point (besides parseSkillFile) that can populate them.
func NewSkill(opts SkillOptions) (*Skill, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("commands: NewSkill: name must not be empty")
	}

	vars := opts.Variables
	if vars == nil {
		vars = map[string]string{}
	}

	s := &Skill{
		name:              opts.Name,
		description:       opts.Description,
		body:              strings.TrimSpace(opts.Body),
		variables:         vars,
		triggerPatterns:   append([]string(nil), opts.TriggerPatterns...),
		requiresIsolation: opts.RequiresIsolation,
		sourcePath:        opts.SourcePath,
	}
	for _, p := range opts.TriggerPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			// Matches parseSkillFile: an invalid pattern is silently skipped
			// from the compiled set. The raw string survives in
			// s.triggerPatterns for introspection via TriggerPatterns().
			continue
		}
		s.triggers = append(s.triggers, re)
	}
	return s, nil
}

// parseSkillFile parses raw .md content into a Skill. Frontmatter is required
// for skills (it carries the trigger patterns). Bad regex patterns are
// silently dropped from the compiled triggers list; the raw patterns are
// preserved in triggerPatterns for diagnostics.
func parseSkillFile(name, raw, sourcePath string) (*Skill, error) {
	s := &Skill{
		name:       name,
		sourcePath: sourcePath,
		variables:  map[string]string{},
	}

	body := raw
	if strings.HasPrefix(raw, "---\n") {
		// Locate closing ---
		rest := raw[4:]
		end := strings.Index(rest, "\n---")
		if end == -1 {
			return nil, fmt.Errorf("skill %s: unterminated frontmatter", name)
		}
		fmBlock := rest[:end]
		body = strings.TrimPrefix(rest[end+4:], "\n")

		var meta skillFrontmatter
		if err := yaml.Unmarshal([]byte(fmBlock), &meta); err != nil {
			return nil, fmt.Errorf("skill %s: parse frontmatter: %w", name, err)
		}
		s.description = meta.Description
		s.triggerPatterns = append([]string(nil), meta.Triggers...)
		s.requiresIsolation = meta.RequiresIsolation
		if meta.Variables != nil {
			s.variables = meta.Variables
		}
		// Compile trigger patterns; silently skip any that are invalid regex.
		for _, p := range meta.Triggers {
			re, err := regexp.Compile(p)
			if err != nil {
				continue
			}
			s.triggers = append(s.triggers, re)
		}
	}

	s.body = strings.TrimSpace(body)
	return s, nil
}

// Render fills the body with positional args and the skill's default variables.
// args maps to {{ARG1}}, {{ARG2}}, … (1-based). selection and currentFile map
// to {{SELECTION}} and {{CURRENT_FILE}}.
func (s *Skill) Render(args []string, selection, currentFile string) (string, error) {
	cc := &CommandContext{Args: args, Selection: selection, CurrentFile: currentFile}
	return (&MarkdownCommand{name: s.name, body: s.body, variables: s.variables}).render(cc)
}

// RenderWithCaptures merges the captures map into the skill's default variables
// and renders the body. Captures take precedence over skill defaults on
// key collision. args, selection, and currentFile are forwarded to the
// substitution engine as usual.
func (s *Skill) RenderWithCaptures(args []string, captures map[string]string, selection, currentFile string) (string, error) {
	merged := make(map[string]string, len(s.variables)+len(captures))
	for k, v := range s.variables {
		merged[k] = v
	}
	for k, v := range captures {
		merged[k] = v
	}
	cc := &CommandContext{Args: args, Selection: selection, CurrentFile: currentFile}
	return (&MarkdownCommand{name: s.name, body: s.body, variables: merged}).render(cc)
}

// SkillRegistry stores skills by name and matches user input against trigger
// patterns. All operations are safe for concurrent use.
type SkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

// NewSkillRegistry constructs an empty SkillRegistry.
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{skills: map[string]*Skill{}}
}

// Add registers a skill, replacing any existing skill with the same name.
func (r *SkillRegistry) Add(s *Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[s.Name()] = s
}

// Remove unregisters the skill with the given name (no-op if absent).
func (r *SkillRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.skills, name)
}

// Get returns the skill registered under name, or (nil, false) if absent.
func (r *SkillRegistry) Get(name string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// List returns all registered skills in lexicographic name order.
func (r *SkillRegistry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for n := range r.skills {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*Skill, 0, len(names))
	for _, n := range names {
		out = append(out, r.skills[n])
	}
	return out
}

// FindMatching iterates skills in lexicographic name order and returns the
// first skill whose compiled trigger patterns match input. The second return
// value is a map of named capture groups from the matching regex. Returns
// (nil, nil, false) when no skill matches.
func (r *SkillRegistry) FindMatching(input string) (*Skill, map[string]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.skills))
	for n := range r.skills {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		s := r.skills[n]
		for _, re := range s.triggers {
			m := re.FindStringSubmatch(input)
			if m == nil {
				continue
			}
			caps := map[string]string{}
			for i, capName := range re.SubexpNames() {
				if capName == "" || i >= len(m) {
					continue
				}
				caps[capName] = m[i]
			}
			return s, caps, true
		}
	}
	return nil, nil, false
}

// skillManifestName is the canonical filename for a packaged skill manifest.
// A directory <skills-dir>/<name>/SKILL.md is loaded as the skill named after
// its containing directory. This is the first-class skill-manifest convention
// (T1.6); it coexists with the legacy flat "<name>.md" form.
const skillManifestName = "SKILL.md"

// ExtraTier is implemented by a caller-owned skill source that must be
// integrated into a SkillLoader's Reload sequence at a specific,
// caller-declared directory position, but cannot live in THIS package —
// e.g. HXC-159's policy-gated external tier (internal/agent's
// LoadExternalTierIntoRegistry / externalTierExtraTier), which depends on
// the shared pkg/skills policy engine one level up. Importing that engine
// here would create an internal/commands -> internal/agent ->
// internal/commands import cycle (internal/agent already imports
// internal/commands), so the integration instead runs through this small
// interface, registered via SetExtraTier.
type ExtraTier interface {
	// Load returns the skills this tier CURRENTLY admits, keyed by the
	// same identity SkillRegistry.Add keys on (Skill.Name()). A read or
	// policy-gate error is reported via err; Reload logs it at WARN and
	// treats the tier as admitting nothing for THIS pass — matching the
	// existing per-directory WARN-and-skip philosophy every other tier
	// already uses (it never aborts Reload).
	Load() (map[string]*Skill, error)
}

// SkillLoader registers skills from the embedded built-in tier plus an
// ORDERED list of on-disk directory tiers, lowest precedence first:
//
//	built-in   skills embedded in the binary (builtin_skills/<name>/SKILL.md)
//	dirs[0]    the first caller-supplied directory
//	dirs[1]    the second caller-supplied directory
//	...        (each later directory overrides an earlier one, and the
//	           built-in tier, on a same-name collision)
//
// On-disk directories recognise two layouts:
//
//	<dir>/<name>.md                   (legacy flat form; name = filename sans .md)
//	<dir>/<name>/SKILL.md             (packaged manifest form; name = subdir name)
//
// Within a single on-disk directory, the packaged SKILL.md manifest takes
// precedence over a legacy flat <name>.md of the same skill name. The
// built-in tier carries only the packaged SKILL.md layout. Non-existent
// directories are silently skipped; per-file parse errors are logged at
// WARN and do not cause Load/Reload to fail.
//
// The built-in tier is loaded UNCONDITIONALLY, as the lowest-precedence
// base layer, on EVERY Reload call — not merely the first — so a caller
// that deletes an on-disk override and reloads always correctly falls back
// to the built-in (HXC-159 T-P9.01 round-5 NEW-4 remediation, 2026-09-23:
// see NewMultiDirSkillLoader's doc comment for the per-instance
// SkipBuiltins(bool) design this replaces and why it broke exactly that
// reload path).
type SkillLoader struct {
	registry   *SkillRegistry
	dirs       []string             // ordered on-disk tiers, lowest precedence first
	extraTiers map[string]ExtraTier // dirs[] entry -> tier hook, see SetExtraTier
	mu         sync.Mutex
	loaded     map[string]string // skill name → source path
	log        *zap.Logger
}

// NewSkillLoader constructs a loader over the classic two-slot
// (projectDir, userDir) shape — project overrides user overrides built-in.
// Either dir may be empty or nonexistent; the loader gracefully skips
// missing dirs. Equivalent to
// NewMultiDirSkillLoader(reg, []string{userDir, projectDir}) — see that
// constructor for the general, arbitrary-ordered-list form.
func NewSkillLoader(reg *SkillRegistry, projectDir, userDir string) *SkillLoader {
	return NewMultiDirSkillLoader(reg, []string{userDir, projectDir})
}

// NewMultiDirSkillLoader constructs a loader over an ARBITRARY ORDERED list
// of on-disk directory tiers, lowest precedence first — the general form of
// NewSkillLoader's fixed two-slot signature, and the SINGLE canonical
// sequencing primitive every front-end (interactive REPL bootstrap, the
// `skills` CLI subcommand, internal/agent's LoadSkillsAndDispatcher /
// LoadSkillsAndDispatcherWithLoader) now shares (HXC-159 T-P9.01 round-5
// NEW-2/NEW-4 remediation, 2026-09-23).
//
// Before this fix, a caller driving more than two ordered directories
// (plus the built-in tier, plus a policy-gated external tier that cannot
// re-scan through this package) had to hand-assemble a SEQUENCE of
// independently constructed SkillLoader instances sharing one
// SkillRegistry, using a per-instance SkipBuiltins(bool) flag to keep only
// the FIRST instance re-adding the embedded built-in tier. That design had
// two defects, both found by the independent review of the fix that
// introduced it (commit 97e8c852): (1) each front-end hand-rolled its OWN
// version of the sequence, and the ORDER in which builtins/legacy/
// canonical/external/project were assembled could — and did — silently
// diverge between front-ends (NEW-2: the same skill name resolved to a
// DIFFERENT source depending on which front-end loaded it); (2) the
// SkipBuiltins flag, once set true on a loader instance, ALSO silently
// disabled built-in re-registration on every SUBSEQUENT Reload of THAT
// SAME instance — including a filesystem-watcher-triggered or `/skills
// reload`-triggered one — so deleting an on-disk override of a built-in
// and reloading PERMANENTLY lost the built-in with nothing left to
// restore it (NEW-4).
//
// This constructor eliminates both defects by construction: ONE loader
// instance owns the FULL tier stack (built-in + every on-disk directory,
// in order, plus any registered ExtraTier — see SetExtraTier), so there is
// only ONE place the sequence is defined (no possibility of two front-ends
// independently drifting), and Reload always re-establishes the SAME
// built-in base layer before replaying every tier, so the reload path can
// no longer lose it.
func NewMultiDirSkillLoader(reg *SkillRegistry, dirs []string) *SkillLoader {
	return &SkillLoader{
		registry: reg,
		dirs:     append([]string(nil), dirs...),
		loaded:   map[string]string{},
		log:      zap.NewNop(),
	}
}

// SetLogger installs a non-noop logger.
func (l *SkillLoader) SetLogger(log *zap.Logger) { l.log = log }

// SetExtraTier registers tier to run at dir's position in the ordered dirs
// list this loader was constructed with (see NewMultiDirSkillLoader):
// whenever Reload reaches an entry equal to dir, it calls tier.Load()
// INSTEAD of scanning dir as an on-disk directory, and folds the result
// into the SAME want map — by the SAME override-by-precedence-order rule
// every other tier uses. Reload's removal tracking (l.loaded) covers this
// tier exactly like an on-disk one: a skill the tier stops admitting is
// removed on the very next Reload, exactly like a deleted file.
//
// A dir value that never appears in this loader's own dirs list is
// harmless to register — the hook simply never fires.
func (l *SkillLoader) SetExtraTier(dir string, tier ExtraTier) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.extraTiers == nil {
		l.extraTiers = map[string]ExtraTier{}
	}
	l.extraTiers[dir] = tier
}

// Load is a synonym for Reload. Provided for clarity at startup.
func (l *SkillLoader) Load() error { return l.Reload() }

// Reload re-scans every configured tier — the embedded built-in tier
// (always first, always unconditional), each ordered on-disk directory in
// turn, and any registered ExtraTier at its declared position — and
// reconciles the registry. Added skills are registered, removed skills are
// unregistered, changed skills are replaced. Per-tier errors (a directory
// read failure, a per-file parse failure, an ExtraTier's own Load error)
// are logged at WARN and the tier is skipped for this pass; they do NOT
// cause Reload to fail — matching the pre-existing "a single broken skill
// file never blocks the front-end" contract, now applied uniformly across
// every tier rather than a hard-fail-for-on-disk / soft-fail-for-external
// split (HXC-159 T-P9.01 round-5 NEW-2/NEW-4 remediation, 2026-09-23).
func (l *SkillLoader) Reload() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	want := map[string]*Skill{}
	// Lowest-precedence tier first: built-in skills embedded in the binary.
	// Loaded UNCONDITIONALLY on every call — see this type's doc comment
	// for why (NEW-4 remediation) — so a later directory's or ExtraTier's
	// override always has a real built-in base layer to override, and a
	// removed override always has a real built-in base layer to fall back
	// to.
	l.loadBuiltinSkills(want)
	for _, dir := range l.dirs {
		if dir == "" {
			continue
		}
		if tier, ok := l.extraTiers[dir]; ok {
			skills, err := tier.Load()
			if err != nil {
				l.log.Warn("skill loader: extra tier load failed", zap.String("dir", dir), zap.Error(err))
				continue
			}
			for name, s := range skills {
				want[name] = s
			}
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			l.log.Warn("skill loader: read dir", zap.String("dir", dir), zap.Error(err))
			continue
		}
		// First pass: legacy flat "<name>.md" files in this directory.
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			// A flat file literally named SKILL.md has no skill name of its own
			// (the name comes from a containing directory in the packaged form),
			// so skip it here; the packaged-manifest pass handles subdirectories.
			if entry.Name() == skillManifestName {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".md")
			path := filepath.Join(dir, entry.Name())
			if s := l.loadSkillFile(name, path); s != nil {
				want[name] = s
			}
		}
		// Second pass: packaged "<name>/SKILL.md" manifests. These take
		// precedence over a legacy flat file of the same skill name within
		// this same directory (manifest form is the first-class T1.6 layout).
		for _, entry := range entries {
			if !entry.IsDir() {
				// A SYMLINKED skill directory — the exact shape
				// scripts/register_skills.sh provisions into .claude/skills/
				// (HXC-159 T-P1.04) — reports IsDir()==false from os.ReadDir.
				// Follow it via os.Stat; a dangling link or a link to a plain
				// file stat-fails / is not a dir and is skipped. Before this
				// (HXC-159 T-P9.01 review finding) every registrar-produced
				// entry was silently invisible to the external tier.
				if entry.Type()&os.ModeSymlink == 0 {
					continue
				}
				if info, statErr := os.Stat(filepath.Join(dir, entry.Name())); statErr != nil || !info.IsDir() {
					continue
				}
			}
			manifest := filepath.Join(dir, entry.Name(), skillManifestName)
			if info, statErr := os.Stat(manifest); statErr != nil || info.IsDir() {
				continue
			}
			name := entry.Name()
			if s := l.loadSkillFile(name, manifest); s != nil {
				want[name] = s
			}
		}
	}

	// Remove names that disappeared (only those previously loaded by us).
	for name := range l.loaded {
		if _, ok := want[name]; !ok {
			l.registry.Remove(name)
			delete(l.loaded, name)
		}
	}
	// Add or replace.
	for name, s := range want {
		l.registry.Add(s)
		l.loaded[name] = s.SourcePath()
	}
	return nil
}

// loadSkillFile reads and parses a single skill file at path under the given
// skill name. Read or parse errors are logged at WARN and reported as a nil
// return (the caller skips the file); they never abort Reload.
func (l *SkillLoader) loadSkillFile(name, path string) *Skill {
	data, err := os.ReadFile(path)
	if err != nil {
		l.log.Warn("skill loader: read file", zap.String("path", path), zap.Error(err))
		return nil
	}
	s, err := parseSkillFile(name, string(data), path)
	if err != nil {
		l.log.Warn("skill loader: parse error", zap.String("path", path), zap.Error(err))
		return nil
	}
	return s
}

// loadBuiltinSkills walks the embedded builtin_skills tree and registers each
// builtin_skills/<name>/SKILL.md manifest into want under the directory name,
// reusing the same parse path as on-disk skills. Built-in skills are the
// lowest-precedence tier; the caller invokes this first so on-disk skills can
// override by name. Per-manifest read/parse errors are logged at WARN and the
// manifest is skipped — they never abort Reload. The source path is recorded as
// "builtin:<name>/SKILL.md" so callers can tell a built-in skill apart from an
// on-disk one.
func (l *SkillLoader) loadBuiltinSkills(want map[string]*Skill) {
	entries, err := fs.ReadDir(builtinSkillsFS, builtinSkillsRoot)
	if err != nil {
		// The embedded tree is compiled in; a read error here is unexpected.
		l.log.Warn("skill loader: read builtin skills", zap.Error(err))
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// embed.FS always uses forward slashes regardless of host OS.
		manifestPath := path.Join(builtinSkillsRoot, name, skillManifestName)
		data, readErr := builtinSkillsFS.ReadFile(manifestPath)
		if readErr != nil {
			// Subdirectory without a SKILL.md (or unreadable): not a skill.
			continue
		}
		sourcePath := "builtin:" + path.Join(name, skillManifestName)
		s, parseErr := parseSkillFile(name, string(data), sourcePath)
		if parseErr != nil {
			l.log.Warn("skill loader: parse builtin skill",
				zap.String("name", name), zap.Error(parseErr))
			continue
		}
		want[name] = s
	}
}

// Loaded returns a snapshot of skill name → source path for all skills
// currently managed by this loader.
func (l *SkillLoader) Loaded() map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]string, len(l.loaded))
	for k, v := range l.loaded {
		out[k] = v
	}
	return out
}

// ParseSkillFile parses raw SKILL.md content (frontmatter + body) into a
// *Skill using the SAME production yaml.v3-based parser every on-disk tier
// (built-in, legacy, canonical, project) already goes through via
// SkillLoader. name is the skill's registry identity (the directory
// basename for a packaged manifest, or the flat-file stem); sourcePath is
// an opaque provenance string recorded on the returned Skill.
//
// This is the exported production entry point HXC-159's external tier uses
// (internal/agent's skillsFromPolicySet) to build a *Skill from a manifest
// it re-reads directly from disk itself, rather than converting an
// already-parsed struct from a DIFFERENT parser (HXC-159 T-P9.01 round-5
// NEW-1/NEW-5 remediation, 2026-09-23): pkg/skills' own front-matter reader
// (a deliberately minimal, dependency-free scanner — R-25 forbids a YAML
// dependency in that project-not-aware library) does not process YAML
// string-escaping or YAML-1.1 boolean forms the way this parser does,
// which broke trigger-regex matching (NEW-1) and requires_isolation
// parsing (NEW-5) when the external tier's skills were built directly from
// pkg/skills' own parsed fields instead of being re-parsed here.
func ParseSkillFile(name, raw, sourcePath string) (*Skill, error) {
	return parseSkillFile(name, raw, sourcePath)
}

// ParseSkillForTest exposes parseSkillFile to other packages' tests.
// Production code uses SkillLoader (or, for a manifest it re-reads from
// disk itself outside SkillLoader's own directory scan, ParseSkillFile
// directly — see its doc comment); this helper is for unit tests in
// packages such as internal/agent that need to construct Skills without a
// real filesystem or a running SkillLoader. It is now a thin alias for
// ParseSkillFile, kept under its original test-only name for the many
// existing call sites.
func ParseSkillForTest(name, raw, sourcePath string) (*Skill, error) {
	return ParseSkillFile(name, raw, sourcePath)
}
