package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dead_providers_gate_test.go — guards for HXC-002-F3-02 (gap ledger
// docs/qa/2026-09-05-gap-ledger.md): the dead `helix-llm` / `helix-debate`
// provider block was removed from config/config.yaml and must never return.
//
// Two layers:
//  1. TestConfigYAML_NoDeadProviderBlock — the shipped config parses AND its
//     bytes contain no helix-llm/helix-debate provider entries.
//  2. TestDeadProviderStrings_ZeroNonCommentGoReferences — the review-note
//     grep gate: zero NON-COMMENT Go references to either string anywhere in
//     the module (a Go test file legitimately cites the endpoint string inside
//     a historical comment — shell_expansion_test.go — so a naive grep is not
//     sufficient; comments are stripped before matching).

// repoRoot resolves the module root from this test's package directory
// (internal/config).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func TestConfigYAML_NoDeadProviderBlock(t *testing.T) {
	root := repoRoot(t)
	cfgPath := filepath.Join(root, "config", "config.yaml")

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read shipped config: %v", err)
	}
	for _, dead := range []string{"helix-llm", "helix-debate"} {
		if strings.Contains(string(raw), dead) {
			t.Fatalf("config/config.yaml still contains dead provider %q — "+
				"HXC-002-F3-02 removal is not in effect", dead)
		}
	}

	// The shipped config must still LOAD cleanly with the block removed
	// (no parse error, LLM defaults intact). Its credentials are
	// ${...}-placeholders by design — supply the env values the secret
	// check requires (CONST-042-safe: throwaway test values, never real).
	t.Setenv("HELIX_CONFIG", cfgPath)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HELIX_AUTH_JWT_SECRET", "test-secret-not-the-default")
	t.Setenv("HELIX_DATABASE_PASSWORD", "test-db-password")
	t.Setenv("HELIX_REDIS_PASSWORD", "test-redis-password")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() of shipped config after dead-block removal: %v", err)
	}
	if cfg.LLM.DefaultProvider != "local" {
		t.Fatalf("llm.default_provider = %q, want \"local\"", cfg.LLM.DefaultProvider)
	}
}

// stripGoComments removes // line comments and /* */ block comments so the
// gate matches only live code. Not a full Go lexer — sufficient for a string
// needle that never appears inside string literals in URLs (the only // -
// containing tokens in this codebase are http:// URLs, and neither needle is
// part of a URL).
func stripGoComments(src string) string {
	var out strings.Builder
	var i int
	for i < len(src) {
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		out.WriteByte(src[i])
		i++
	}
	return out.String()
}

func TestDeadProviderStrings_ZeroNonCommentGoReferences(t *testing.T) {
	root := repoRoot(t)
	needles := []string{"helix-llm", "helix-debate"}

	var hits []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// The gate file itself names the needles as string literals — exempt
		// it (a gate that flags itself is noise, not evidence).
		if strings.HasSuffix(path, "dead_providers_gate_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		code := stripGoComments(string(raw))
		for _, n := range needles {
			if strings.Contains(code, n) {
				hits = append(hits, path+": "+n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}

	// FAIL-OPEN FLOOR (§11.4.201). Everything above this line only ever
	// ACCUMULATES failures, so an empty `hits` is ambiguous: it means either
	// "the module is clean" (the verdict this gate intends to report) or "the
	// walk examined nothing" — a wrong root, an over-broad SkipDir, a suffix
	// filter that stopped matching. The second case reports PASS while
	// checking nothing at all, which is the exact fail-open shape this gate
	// exists to prevent elsewhere. Asserting a floor makes a broken walk fail
	// LOUDLY instead of vacuously, the same way governedSourceFiles in
	// internal/llm/cloud_gate_redaction_scan_test.go refuses an empty corpus.
	//
	// The floor is calibrated on the real module and set far below it, so it
	// discriminates "walk is broken" from "codebase legitimately shrank":
	// measured 2214 non-excluded .go files at the time of writing, so 500
	// leaves room for a large deletion round while still catching any walk
	// that collapses to a handful of files or to none.
	const minScannedGoFiles = 500
	if scanned < minScannedGoFiles {
		t.Fatalf("dead-provider walk examined only %d .go file(s) under %s, "+
			"want at least %d — the walk is broken (wrong root, over-broad "+
			"SkipDir, or a filter that stopped matching), so the clean verdict "+
			"below would be vacuous rather than earned",
			scanned, root, minScannedGoFiles)
	}

	if len(hits) > 0 {
		t.Fatalf("non-comment Go references to dead providers found "+
			"(HXC-002-F3-02 grep gate): %v", hits)
	}
	t.Logf("scanned %d .go file(s) under %s for dead-provider references", scanned, root)
}
