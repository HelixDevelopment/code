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
// but are NOT copies of it — investigated under HXC-159 T-P9.01 finding
// F-13 (2026-09-23): the constitution side is a Claude-Code-native Agent
// Skill doc; the builtin side is a HelixCode-internal regex-triggered
// REPL completion-prompt template (`description`/`triggers`/
// `requires_isolation` frontmatter this loader itself parses — see
// skillFrontmatter below). Running scripts/generate_builtin_skills.sh in
// its regenerate mode overwrites the builtin format with the constitution
// one and PROVABLY breaks TestGovernanceSkills_RegisteredAndInvocable (the
// regex triggers this loader's auto-trigger matcher depends on are lost) —
// do NOT run it in regenerate mode without first reconciling the two
// formats. `scripts/generate_builtin_skills.sh --check` is safe and
// reports drift for visibility only (§11.4.77 regeneration-mechanism
// declaration, detection-only pending that reconciliation).
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

// SkillLoader registers skills from three precedence tiers, lowest to highest:
//
//	built-in   skills embedded in the binary (builtin_skills/<name>/SKILL.md)
//	user       skills under the user skills directory
//	project    skills under the project skills directory
//
// On-disk tiers (user, project) recognise two layouts:
//
//	<dir>/<name>.md                   (legacy flat form; name = filename sans .md)
//	<dir>/<name>/SKILL.md             (packaged manifest form; name = subdir name)
//
// Resolution is project > user > built-in: a project (or user) skill of a given
// name overrides a user (or built-in) skill of the same name. Within a single
// on-disk directory, the packaged SKILL.md manifest takes precedence over a
// legacy flat <name>.md of the same skill name. The built-in tier carries only
// the packaged SKILL.md layout. Non-existent directories are silently skipped;
// per-file parse errors are logged at WARN and do not cause Load/Reload to
// fail.
type SkillLoader struct {
	registry   *SkillRegistry
	projectDir string
	userDir    string
	mu         sync.Mutex
	loaded     map[string]string // skill name → source path
	log        *zap.Logger
}

// NewSkillLoader constructs a loader. Either dir may be empty or
// nonexistent; the loader gracefully skips missing dirs.
func NewSkillLoader(reg *SkillRegistry, projectDir, userDir string) *SkillLoader {
	return &SkillLoader{
		registry:   reg,
		projectDir: projectDir,
		userDir:    userDir,
		loaded:     map[string]string{},
		log:        zap.NewNop(),
	}
}

// SetLogger installs a non-noop logger.
func (l *SkillLoader) SetLogger(log *zap.Logger) { l.log = log }

// Load is a synonym for Reload. Provided for clarity at startup.
func (l *SkillLoader) Load() error { return l.Reload() }

// Reload re-scans both directories and reconciles the registry. Added files
// are registered, removed files are unregistered, changed files are replaced.
// Per-file errors (parse, read) are logged at WARN and the file is skipped;
// they do NOT cause Reload to fail.
func (l *SkillLoader) Reload() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	want := map[string]*Skill{}
	// Lowest-precedence tier first: built-in skills embedded in the binary.
	// They are written into `want` before the on-disk passes, so a user or
	// project skill of the same name overrides the built-in (later writes to
	// `want[name]` win). Precedence: project > user > built-in.
	l.loadBuiltinSkills(want)
	// Then user, then project (project overrides user, both override built-in).
	for _, dir := range []string{l.userDir, l.projectDir} {
		if dir == "" {
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

// ParseSkillForTest exposes parseSkillFile to other packages' tests.
// Production code uses SkillLoader; this helper is for unit tests in
// packages such as internal/agent that need to construct Skills without
// a real filesystem or a running SkillLoader.
func ParseSkillForTest(name, raw, sourcePath string) (*Skill, error) {
	return parseSkillFile(name, raw, sourcePath)
}
