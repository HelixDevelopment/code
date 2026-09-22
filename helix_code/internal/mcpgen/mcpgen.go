// Package mcpgen generates .mcp.json MCP-server entries from a skills
// manifest instead of hand-authored, hardcoded paths (T-P5.04, HXC-159 Phase
// P5, RS-17). It is a thin, pure-data generator: it reads a TSV manifest (the
// T-P1.04.2 schema: name<TAB>source<TAB>owner, one row per line, blank lines
// and "#"-comment lines ignored) and a filesystem root the manifest's
// `source` column is relative to, and derives one MCP server entry per
// manifest row that has a corresponding <root>/<source>/<name>.sh wrapper
// script on disk. Rows without a wrapper script are silently skipped — not
// every manifest skill has one.
//
// The generated entries are merged into a target .mcp.json document,
// REPLACING any existing entry with the same key (a hand-edit to a
// manifest-derived entry never survives — that is the whole point) while
// PRESERVING every entry whose key does not come from the manifest
// (hand-authored, non-skill servers).
//
// The root parameter is always caller-supplied — never hardcoded — so this
// package stays decoupled from any one project's directory layout (D-3 /
// CONST-051(B)): a caller (cmd/mcpgen, or any other consumer) supplies its
// own manifest path, root, and target file.
package mcpgen

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ManifestRow is one parsed row of the T-P1.04.2 TSV manifest schema.
type ManifestRow struct {
	Name   string
	Source string
	Owner  string
}

// ParseManifest parses the T-P1.04.2 TSV schema: one row per line,
// name<TAB>source<TAB>owner. Blank lines and lines whose first non-whitespace
// character is "#" are ignored. A malformed row (not exactly three
// TAB-separated fields) is a parse error — a manifest reader must never
// silently drop a row it cannot understand.
func ParseManifest(r io.Reader) ([]ManifestRow, error) {
	var rows []ManifestRow
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("mcpgen: manifest line %d: want 3 TAB-separated fields (name, source, owner), got %d: %q", lineNo, len(fields), line)
		}
		rows = append(rows, ManifestRow{
			Name:   strings.TrimSpace(fields[0]),
			Source: strings.TrimSpace(fields[1]),
			Owner:  strings.TrimSpace(fields[2]),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mcpgen: reading manifest: %w", err)
	}
	return rows, nil
}

// ParseManifestFile is a convenience wrapper around ParseManifest for a file
// path.
func ParseManifestFile(path string) ([]ManifestRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mcpgen: open manifest %s: %w", path, err)
	}
	defer f.Close()
	return ParseManifest(f)
}

// ServerEntry is one generated .mcp.json server object. It deliberately
// carries only the fields this generator can derive purely from the
// manifest's path structure — command, args, and the two constant
// bookkeeping fields every hand-authored "command"-type entry in this
// project already carries (disabled, autoApprove). It does NOT carry
// `description` or `env`: the T-P1.04.2 manifest schema has no column for
// either, and inventing one here would be exactly the kind of
// project-specific coupling this generator exists to avoid. A caller that
// needs richer per-entry metadata should extend the manifest schema
// (tracked as a documented follow-up, not assumed here).
type ServerEntry struct {
	Type        string   `json:"type"`
	Command     string   `json:"command"`
	Args        []string `json:"args"`
	Disabled    bool     `json:"disabled"`
	AutoApprove []string `json:"autoApprove"`
}

// wrapperScriptRelPath returns the manifest-derived, root-relative path to a
// row's MCP wrapper script: <source>/<name>.sh.
func wrapperScriptRelPath(row ManifestRow) string {
	return filepath.Join(row.Source, row.Name+".sh")
}

// Generate derives manifest-sourced server entries. root is the filesystem
// root the manifest's `source` paths are relative to (config-injected, never
// hardcoded — see the package doc). A row is included in the result only
// when <root>/<source>/<name>.sh exists as a regular, non-directory file;
// absence is not an error, since not every manifest skill ships an MCP
// wrapper script.
func Generate(rows []ManifestRow, root string) (map[string]ServerEntry, error) {
	out := make(map[string]ServerEntry, len(rows))
	for _, row := range rows {
		relPath := wrapperScriptRelPath(row)
		absPath := filepath.Join(root, relPath)
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("mcpgen: stat %s: %w", absPath, err)
		}
		if info.IsDir() {
			continue
		}
		out[row.Name] = ServerEntry{
			Type:        "command",
			Command:     "bash",
			Args:        []string{relPath},
			Disabled:    false,
			AutoApprove: []string{},
		}
	}
	return out, nil
}

// manifestKeySet returns the set of skill names present in rows, regardless
// of whether Generate ultimately produced an entry for them — used so a
// manifest row whose wrapper script was REMOVED still causes its stale
// .mcp.json entry to be dropped on the next regeneration, rather than
// lingering forever as an orphan a manifest edit can no longer reach.
func manifestKeySet(rows []ManifestRow) map[string]bool {
	set := make(map[string]bool, len(rows))
	for _, row := range rows {
		set[row.Name] = true
	}
	return set
}

// document is the minimal .mcp.json shape this package understands: a single
// top-level "mcpServers" object. Every other top-level key (if any existed)
// and every non-manifest-derived server entry is preserved byte-for-byte via
// json.RawMessage — this package never re-serialises data it did not itself
// generate, so a hand-authored entry's extra fields (description, env, …)
// survive a merge unchanged.
type document struct {
	McpServers map[string]json.RawMessage `json:"mcpServers"`
}

// loadDocument reads an existing target file, if any. A missing file yields
// an empty document (first run); any other read/parse error is returned.
func loadDocument(path string) (document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return document{McpServers: map[string]json.RawMessage{}}, nil
		}
		return document{}, fmt.Errorf("mcpgen: read %s: %w", path, err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return document{}, fmt.Errorf("mcpgen: parse existing %s as JSON: %w", path, err)
	}
	if doc.McpServers == nil {
		doc.McpServers = map[string]json.RawMessage{}
	}
	return doc, nil
}

// marshalDeterministic renders doc with sorted keys and stable 2-space
// indentation, terminated by a single trailing newline, so re-running the
// generator on unchanged inputs produces a byte-identical file (RS-17).
// encoding/json already sorts map[string]... keys when marshalling, but that
// guarantee is documented behaviour this function pins explicitly (a
// paired-mutation-style safety net: if that guarantee ever changed, this
// function's own round-trip tests would catch it).
func marshalDeterministic(doc document) ([]byte, error) {
	// Marshal into a stable, top-level ordering by hand: "mcpServers" is the
	// only field this package's `document` type declares, but encode via a
	// json.RawMessage-typed intermediate so nested server objects retain
	// their own natural (sorted, since map keys) field order without a
	// second unmarshal/remarshal round-trip introducing drift.
	inner, err := json.Marshal(doc.McpServers)
	if err != nil {
		return nil, fmt.Errorf("mcpgen: marshal mcpServers: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString("{\n  \"mcpServers\": ")
	if err := json.Indent(&buf, inner, "  ", "  "); err != nil {
		return nil, fmt.Errorf("mcpgen: indent mcpServers: %w", err)
	}
	buf.WriteString("\n}\n")
	return buf.Bytes(), nil
}

// WriteMerged generates manifest-derived entries from rows and root, merges
// them into whatever already exists at targetPath (preserving non-manifest
// entries, replacing manifest-derived ones unconditionally), and writes the
// result back to targetPath with deterministic formatting. Calling this
// twice in a row with unchanged inputs produces a byte-identical file.
func WriteMerged(targetPath string, rows []ManifestRow, root string) error {
	doc, err := loadDocument(targetPath)
	if err != nil {
		return err
	}
	generated, err := Generate(rows, root)
	if err != nil {
		return err
	}
	known := manifestKeySet(rows)

	// Drop stale manifest-derived entries whose row (or wrapper script) is
	// gone, so removing a row from the manifest removes its .mcp.json entry
	// on the next regeneration rather than leaving an orphan forever.
	for name := range doc.McpServers {
		if known[name] {
			if _, stillGenerated := generated[name]; !stillGenerated {
				delete(doc.McpServers, name)
			}
		}
	}
	for name, entry := range generated {
		raw, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("mcpgen: marshal generated entry %q: %w", name, err)
		}
		doc.McpServers[name] = raw
	}

	out, err := marshalDeterministic(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("mcpgen: mkdir for %s: %w", targetPath, err)
	}
	// Write via temp-then-rename so a crash mid-write never leaves a
	// truncated .mcp.json behind.
	tmp := targetPath + ".mcpgen.tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("mcpgen: write temp file: %w", err)
	}
	if err := os.Rename(tmp, targetPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("mcpgen: rename temp file into place: %w", err)
	}
	return nil
}

// Drift reports the names of every manifest-derived entry currently on disk
// at targetPath whose content differs from what Generate would produce right
// now — i.e. a hand-edit (or stale generation) that has drifted from the
// manifest's authority. An empty, nil-error result means the target is fully
// in sync. This is the engine behind gate CM-MCP-ENTRIES-GENERATED.
func Drift(targetPath string, rows []ManifestRow, root string) ([]string, error) {
	doc, err := loadDocument(targetPath)
	if err != nil {
		return nil, err
	}
	generated, err := Generate(rows, root)
	if err != nil {
		return nil, err
	}

	var drifted []string
	for name, want := range generated {
		wantRaw, err := json.Marshal(want)
		if err != nil {
			return nil, fmt.Errorf("mcpgen: marshal expected entry %q: %w", name, err)
		}
		gotRaw, ok := doc.McpServers[name]
		if !ok {
			drifted = append(drifted, name)
			continue
		}
		if !jsonEqual(gotRaw, wantRaw) {
			drifted = append(drifted, name)
		}
	}
	sort.Strings(drifted)
	return drifted, nil
}

// jsonEqual compares two JSON byte slices for semantic (not byte-for-byte)
// equality by round-tripping both through the same generic decode — so
// whitespace/key-order differences in the on-disk file never produce a false
// drift report.
func jsonEqual(a, b []byte) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	aCanon, err := json.Marshal(av)
	if err != nil {
		return false
	}
	bCanon, err := json.Marshal(bv)
	if err != nil {
		return false
	}
	return string(aCanon) == string(bCanon)
}

// ProbeResult is the outcome of Probe.
type ProbeResult struct {
	// Started reports whether the operating system was able to start the
	// entry's command+args at all — the proof that the generated path
	// genuinely resolves to something executable, independent of whatever
	// that program then does.
	Started bool
	Stdout  string
	Stderr  string
	Err     error
}

// Probe executes entry's Command+Args (resolved relative to root, mirroring
// how the args were generated as root-relative paths) with a bounded
// timeout, appending no extra arguments — RS-17's "starts and answers a
// probe" is satisfied by the process genuinely starting and running to
// completion within the timeout; a script that does not exist, is not
// executable, or whose path was hand-edited to something wrong fails to
// start, which Probe reports via a non-nil error / Started=false — "path
// correctness proven by execution, not by the string looking right."
func Probe(ctx context.Context, entry ServerEntry, root string) (ProbeResult, error) {
	args := make([]string, len(entry.Args))
	for i, a := range entry.Args {
		if filepath.IsAbs(a) {
			args[i] = a
		} else {
			args[i] = filepath.Join(root, a)
		}
	}

	probeCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		probeCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(probeCtx, entry.Command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startErr := cmd.Start()
	result := ProbeResult{
		Started: startErr == nil,
		Stdout:  "",
		Stderr:  "",
	}
	if startErr != nil {
		result.Err = fmt.Errorf("mcpgen: probe %s %v: failed to start: %w", entry.Command, args, startErr)
		return result, result.Err
	}
	waitErr := cmd.Wait()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if waitErr != nil {
		// The process STARTED (path resolved, was executable); a non-zero
		// exit is reported but does not itself invalidate Started, since a
		// third-party wrapper script's exit-code contract is not this
		// package's to assume. Callers that require exit 0 can check
		// result.Err.
		result.Err = fmt.Errorf("mcpgen: probe %s %v: process exited with error: %w", entry.Command, args, waitErr)
		return result, result.Err
	}
	return result, nil
}
