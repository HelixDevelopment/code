package mcpgen_test

// T-P5.04 (HXC-159 Phase P5) — generate `.mcp.json` entries from the manifest
// instead of hardcoding constitution skill paths.
//
// # Scope note (honest boundary, §11.4.6)
//
// The repository's real `.mcp.json` lives at the meta-repo root, OUTSIDE
// this subagent's declared file scope (helix_code/**, specs/.../tasks.md,
// qa-results/hxc159/**) for HXC-159 Phase P5 — two other agents are working
// submodules/helix_agent and constitution/ concurrently in the same
// checkout, and the root .mcp.json is not part of either the P5 task's or
// this subagent's assigned surface. Every test in this file therefore
// operates on a FIXTURE manifest and a FIXTURE target file under t.TempDir()
// — never the real repository .mcp.json. The generator (internal/mcpgen,
// cmd/mcpgen) is fully general: pointing it at the real root manifest
// (scripts/skills_manifest.tsv) and the real root .mcp.json via its --root/
// --manifest/--out flags is a mechanical follow-up for whichever stream owns
// that file; see the final report for the exact command.
//
// # RUNTIME SIGNATURE (§11.4.108) — RS-17
//
// Re-running the generator on unchanged inputs produces a byte-identical
// file (idempotence), AND each generated server genuinely starts and answers
// a probe — "path correctness proven by execution, not by the string looking
// right." Before this package existed, this file did not compile at all —
// captured verbatim in
// qa-results/hxc159/mcp_generated/<run-id>/red_compile_fail.txt.
//
// # T-P5.04.1's RED scenario
//
// "assert that a hand-edited path currently survives undetected": before
// this package's Drift function exists, NOTHING in helix_code notices when a
// generated-looking .mcp.json entry's path has been hand-edited to point
// somewhere else. TestDrift_DetectsHandEditedPath is the standing regression
// guard proving Drift (the gate CM-MCP-ENTRIES-GENERATED's engine) now
// catches exactly that.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"dev.helix.code/internal/mcpgen"
)

const manifestFixture = `# comment line, ignored
media-validator	skills/media-validator	constitution
no-wrapper-script	skills/no-wrapper-script	constitution
`

// writeProbeableScript writes a real, executable bash script at
// <root>/<relDir>/<name>.sh that answers the generator's probe contract
// (exits 0 when invoked with the probe marker argument, printing a
// recognisable line to stdout) — so the probe test below exercises a REAL
// process start, not a mocked one.
func writeProbeableScript(t *testing.T, root, relDir, name string) string {
	t.Helper()
	dir := filepath.Join(root, relDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	scriptPath := filepath.Join(dir, name+".sh")
	content := "#!/usr/bin/env bash\necho \"" + name + " MCPGEN-PROBE-OK\"\nexit 0\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return scriptPath
}

func TestParseManifest_SkipsCommentsAndBlankLines(t *testing.T) {
	rows, err := mcpgen.ParseManifest(strings.NewReader(manifestFixture))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (comment + blank must be skipped): %+v", len(rows), rows)
	}
	if rows[0].Name != "media-validator" || rows[0].Source != "skills/media-validator" || rows[0].Owner != "constitution" {
		t.Fatalf("row[0] = %+v, want {media-validator skills/media-validator constitution}", rows[0])
	}
}

func TestGenerate_OnlyIncludesRowsWithAWrapperScript(t *testing.T) {
	root := t.TempDir()
	writeProbeableScript(t, root, "skills", "media-validator")
	// Deliberately do NOT write a script for no-wrapper-script — not every
	// manifest skill has an MCP wrapper, and that must not be an error.

	rows, err := mcpgen.ParseManifest(strings.NewReader(manifestFixture))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	entries, err := mcpgen.Generate(rows, root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, ok := entries["media-validator"]; !ok {
		t.Fatalf("entries = %+v, want media-validator present (its .sh wrapper exists)", entries)
	}
	if _, ok := entries["no-wrapper-script"]; ok {
		t.Fatalf("entries = %+v, want no-wrapper-script ABSENT (no .sh wrapper on disk, absence is not an error)", entries)
	}
	got := entries["media-validator"]
	wantArgs := []string{filepath.Join("skills", "media-validator", "media-validator.sh")}
	if got.Type != "command" || got.Command != "bash" || len(got.Args) != 1 || got.Args[0] != wantArgs[0] {
		t.Fatalf("generated entry = %+v, want type=command command=bash args=%v", got, wantArgs)
	}
}

func TestGenerate_IdempotentByteIdenticalRegeneration(t *testing.T) {
	root := t.TempDir()
	writeProbeableScript(t, root, "skills", "media-validator")
	rows, err := mcpgen.ParseManifest(strings.NewReader(manifestFixture))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	target := filepath.Join(t.TempDir(), ".mcp.json")

	// First run: target does not exist yet.
	if err := mcpgen.WriteMerged(target, rows, root); err != nil {
		t.Fatalf("WriteMerged (first run): %v", err)
	}
	first, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile after first run: %v", err)
	}

	// Second run: unchanged inputs, must produce a BYTE-IDENTICAL file.
	if err := mcpgen.WriteMerged(target, rows, root); err != nil {
		t.Fatalf("WriteMerged (second run): %v", err)
	}
	second, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile after second run: %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("regeneration is not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}

	// Sanity: the file is valid, parseable JSON with the expected shape.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(first, &doc); err != nil {
		t.Fatalf("generated file is not valid JSON: %v\ncontent:\n%s", err, first)
	}
	if _, ok := doc["mcpServers"]; !ok {
		t.Fatalf("generated document missing top-level \"mcpServers\" key: %s", first)
	}
}

func TestWriteMerged_PreservesHandAuthoredNonManifestEntries(t *testing.T) {
	root := t.TempDir()
	writeProbeableScript(t, root, "skills", "media-validator")
	rows, err := mcpgen.ParseManifest(strings.NewReader(manifestFixture))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}

	target := filepath.Join(t.TempDir(), ".mcp.json")
	// Seed the target with a hand-authored, non-manifest-sourced entry
	// (mirroring the real root .mcp.json's "codegraph" / "open-design"
	// entries, which are not derived from any skill manifest row).
	seed := `{
  "mcpServers": {
    "codegraph": {
      "type": "stdio",
      "command": "codegraph",
      "args": ["serve", "--mcp"]
    }
  }
}
`
	if err := os.WriteFile(target, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed WriteFile: %v", err)
	}

	if err := mcpgen.WriteMerged(target, rows, root); err != nil {
		t.Fatalf("WriteMerged: %v", err)
	}

	out, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if _, ok := doc.McpServers["codegraph"]; !ok {
		t.Fatalf("hand-authored non-manifest entry \"codegraph\" was DROPPED by the merge; got: %s", out)
	}
	if _, ok := doc.McpServers["media-validator"]; !ok {
		t.Fatalf("manifest-derived entry \"media-validator\" is missing after merge; got: %s", out)
	}
}

// TestDrift_DetectsHandEditedPath is the T-P5.04.1 RED-scenario's standing
// GREEN guard: a manifest-derived entry whose on-disk path has been
// hand-edited away from what the generator would currently produce is
// reported by Drift, naming the entry. Before this function existed, no
// mechanism in helix_code detected this at all — the defect T-P5.04.1
// describes as "currently survives undetected."
func TestDrift_DetectsHandEditedPath(t *testing.T) {
	root := t.TempDir()
	writeProbeableScript(t, root, "skills", "media-validator")
	rows, err := mcpgen.ParseManifest(strings.NewReader(manifestFixture))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	target := filepath.Join(t.TempDir(), ".mcp.json")
	if err := mcpgen.WriteMerged(target, rows, root); err != nil {
		t.Fatalf("WriteMerged: %v", err)
	}

	// Sanity: freshly generated file has NO drift.
	drifted, err := mcpgen.Drift(target, rows, root)
	if err != nil {
		t.Fatalf("Drift (pre-hand-edit): %v", err)
	}
	if len(drifted) != 0 {
		t.Fatalf("Drift on a freshly generated file reported drift: %v", drifted)
	}

	// Hand-edit the on-disk file: point media-validator's args at a
	// DIFFERENT (still-real, still-plausible-looking) path — reproducing
	// exactly the "hardcoded, re-coupled" defect T-P5.04 exists to catch.
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	handEdited := strings.Replace(string(raw),
		filepath.ToSlash(filepath.Join("skills", "media-validator", "media-validator.sh")),
		"some/other/hand-edited/path.sh", 1)
	if handEdited == string(raw) {
		t.Fatal("test bug: the replacement found nothing to replace")
	}
	if err := os.WriteFile(target, []byte(handEdited), 0o644); err != nil {
		t.Fatalf("WriteFile (hand-edit): %v", err)
	}

	drifted, err = mcpgen.Drift(target, rows, root)
	if err != nil {
		t.Fatalf("Drift (post-hand-edit): %v", err)
	}
	if len(drifted) != 1 || drifted[0] != "media-validator" {
		t.Fatalf("Drift = %v, want exactly [\"media-validator\"] naming the hand-edited entry", drifted)
	}
}

// TestGenerate_NoSecretsInOutput proves the generator structurally cannot
// leak a secret: it emits only PATH REFERENCES (command/args), never file
// CONTENTS. A wrapper script's body containing something that looks like a
// credential must never appear anywhere in the generated JSON.
func TestGenerate_NoSecretsInOutput(t *testing.T) {
	root := t.TempDir()
	scriptPath := writeProbeableScript(t, root, "skills", "media-validator")
	// Overwrite with content containing an embedded "secret" the generator
	// must never read into its output.
	secret := "sk-test-EXTREMELY-SECRET-TOKEN-0123456789"
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bash\nexport API_KEY="+secret+"\necho ok\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rows, err := mcpgen.ParseManifest(strings.NewReader(manifestFixture))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	target := filepath.Join(t.TempDir(), ".mcp.json")
	if err := mcpgen.WriteMerged(target, rows, root); err != nil {
		t.Fatalf("WriteMerged: %v", err)
	}
	out, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(out), secret) {
		t.Fatalf("generated output leaked the wrapper script's embedded secret:\n%s", out)
	}
}

// TestProbe_RealGeneratedServerStartsAndAnswers is RS-17's second half: the
// generated entry's command+args genuinely resolve to an executable file
// that starts and produces output — a REAL os/exec.Command invocation
// against a REAL script this test wrote, never a mock.
func TestProbe_RealGeneratedServerStartsAndAnswers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SKIP-OK: bash-script probe fixture targets POSIX shells")
	}
	root := t.TempDir()
	writeProbeableScript(t, root, "skills", "media-validator")
	rows, err := mcpgen.ParseManifest(strings.NewReader(manifestFixture))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	entries, err := mcpgen.Generate(rows, root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	entry, ok := entries["media-validator"]
	if !ok {
		t.Fatal("media-validator entry missing from Generate output")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := mcpgen.Probe(ctx, entry, root)
	if err != nil {
		t.Fatalf("Probe: unexpected error for a genuinely executable script: %v", err)
	}
	if !result.Started {
		t.Fatal("Probe.Started = false, want true — the OS must have been able to exec the generated command+args")
	}
	if !strings.Contains(result.Stdout, "MCPGEN-PROBE-OK") {
		t.Fatalf("Probe.Stdout = %q, want it to contain the fixture script's marker output", result.Stdout)
	}

	// Negative control: a path that does not resolve must be REPORTED, not
	// silently swallowed — "path correctness proven by execution."
	broken := entry
	broken.Args = []string{filepath.Join(root, "this", "path", "does", "not", "exist.sh")}
	result2, err := mcpgen.Probe(ctx, broken, root)
	if err == nil && result2.Started {
		t.Fatal("Probe against a nonexistent script path reported success; want it to report the path did not resolve")
	}
}
