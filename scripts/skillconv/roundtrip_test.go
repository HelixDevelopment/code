// T-P1.08.3/4/6: round-trip tests over the REAL in-tree corpora.
//
// Invariant under test: convert(convert(x)) == x, where one "convert" is a
// single dialect<->canonical leg.
//
// Dialect is detected PER FILE (DetectDialect), never per directory: the
// T-P1.08 survey premise "constitution/skills/* is HelixCode-shaped" is
// refuted by the files (5 of 8 carry name:/version: with no triggers:, i.e.
// Agent shape; 3 carry no frontmatter). Measured 2026-09-13: helixcode=11
// files (8 builtin + 3 frontmatter-less), helixagent=1179 (1174 helix_agent
// + 5 constitution).
//
// Per-file assertions:
//  1. same-dialect:  parse -> canonical -> emit(home) -> re-parse == original.
//  2. cross-dialect: parse -> canonical -> OTHER -> parse -> canonical ->
//     home -> re-parse == original. The foreign trigger/allowance model rides
//     as inert carriage (never merged, never dropped).
//  3. canonical JSON stability: marshal -> unmarshal -> marshal is byte-stable.
//
// Timestamps and file paths are runtime data, not file content: skillconv
// keeps neither LoadedAt-style stamp, and SourcePath is parse-supplied.
// Equality is reflect.DeepEqual on Normalize()d structs.
//
// OUT OF SCOPE (blocked on P3, recorded in evidence, never invented here):
//   - T-P1.08.5 HelixSkills TOML leg — upstream corpus not in this checkout.
//   - T-P1.08.6's third-corpus third is the TOML leg above; the two in-tree
//     dialects round-trip here in full (1190 files).
package skillconv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// corpusRoots covers every in-tree SKILL.md: both HelixCode trees and the
// full 1174-file HelixAgent tree.
func corpusRoots(root string) []string {
	return []string{
		filepath.Join(root, "helix_code", "internal", "commands", "builtin_skills"),
		filepath.Join(root, "constitution", "skills"),
		filepath.Join(root, "submodules", "helix_agent", "skills"),
	}
}

func collectSkillFiles(t *testing.T, roots []string) []string {
	t.Helper()
	var out []string
	for _, r := range roots {
		err := filepath.Walk(r, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.ToUpper(info.Name()) == "SKILL.MD" {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", r, err)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("no SKILL.md files under %v — corpus missing", roots)
	}
	return out
}

// TestDialectDetection pins the per-file classification (and thereby the
// survey correction): 11 helixcode + 1179 helixagent == 1190 total.
func TestDialectDetection(t *testing.T) {
	files := collectSkillFiles(t, corpusRoots(repoRoot(t)))
	counts := map[Dialect]int{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		counts[DetectDialect(string(raw))]++
	}
	t.Logf("DIALECTS total=%d helixcode=%d helixagent=%d",
		len(files), counts[DialectHelixCode], counts[DialectHelixAgent])
	if len(files) != 1190 {
		t.Errorf("corpus size changed: got %d, want 1190 (investigate, don't just update)", len(files))
	}
	if counts[DialectHelixCode] != 11 || counts[DialectHelixAgent] != 1179 {
		t.Errorf("dialect split changed: got hc=%d ha=%d, want hc=11 ha=1179",
			counts[DialectHelixCode], counts[DialectHelixAgent])
	}
}

// TestRoundTrip_SameDialect: parse -> canonical -> emit(home) -> re-parse == original.
func TestRoundTrip_SameDialect(t *testing.T) {
	files := collectSkillFiles(t, corpusRoots(repoRoot(t)))
	passHC, passHA, nHC, nHA := 0, 0, 0, 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if DetectDialect(string(raw)) == DialectHelixCode {
			nHC++
			name := filepath.Base(filepath.Dir(f))
			orig, err := ParseHelixCode(string(raw), name, f)
			if err != nil {
				t.Errorf("%s: parse: %v", f, err)
				continue
			}
			back, err := CanonicalToHelixCode(HelixCodeToCanonical(orig))
			if err != nil {
				t.Errorf("%s: back-convert: %v", f, err)
				continue
			}
			re, err := ParseHelixCode(EmitHelixCode(back), name, f)
			if err != nil {
				t.Errorf("%s: re-parse: %v", f, err)
				continue
			}
			if !reflect.DeepEqual(orig.Normalize(), re.Normalize()) {
				t.Errorf("%s: round-trip mismatch:\norig=%#v\nre  =%#v", f, orig.Normalize(), re.Normalize())
				continue
			}
			passHC++
		} else {
			nHA++
			orig, err := ParseHelixAgent(string(raw), f)
			if err != nil {
				t.Errorf("%s: parse: %v", f, err)
				continue
			}
			back, err := CanonicalToHelixAgent(HelixAgentToCanonical(orig))
			if err != nil {
				t.Errorf("%s: back-convert: %v", f, err)
				continue
			}
			re, err := ParseHelixAgent(EmitHelixAgent(back), f)
			if err != nil {
				t.Errorf("%s: re-parse: %v", f, err)
				continue
			}
			if !reflect.DeepEqual(orig.Normalize(), re.Normalize()) {
				t.Errorf("%s: round-trip mismatch:\norig=%#v\nre  =%#v", f, orig.Normalize(), re.Normalize())
				continue
			}
			passHA++
		}
	}
	t.Logf("SAME-DIALECT total=%d hc=%d/%d ha=%d/%d", len(files), passHC, nHC, passHA, nHA)
	if passHC != nHC || passHA != nHA {
		t.Fatalf("same-dialect: hc %d/%d ha %d/%d", passHC, nHC, passHA, nHA)
	}
}

// TestRoundTrip_CrossDialect: parse -> canonical -> OTHER -> parse ->
// canonical -> home -> re-parse == original. Foreign models ride as carriage.
func TestRoundTrip_CrossDialect(t *testing.T) {
	files := collectSkillFiles(t, corpusRoots(repoRoot(t)))
	passHC, passHA, nHC, nHA := 0, 0, 0, 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if DetectDialect(string(raw)) == DialectHelixCode {
			nHC++
			name := filepath.Base(filepath.Dir(f))
			orig, err := ParseHelixCode(string(raw), name, f)
			if err != nil {
				t.Errorf("%s: parse: %v", f, err)
				continue
			}
			agent, err := CanonicalToHelixAgent(HelixCodeToCanonical(orig))
			if err != nil {
				t.Errorf("%s: to-agent: %v", f, err)
				continue
			}
			agentBack, err := ParseHelixAgent(EmitHelixAgent(agent), f)
			if err != nil {
				t.Errorf("%s: re-parse agent: %v", f, err)
				continue
			}
			home, err := CanonicalToHelixCode(HelixAgentToCanonical(agentBack))
			if err != nil {
				t.Errorf("%s: to-code: %v", f, err)
				continue
			}
			re, err := ParseHelixCode(EmitHelixCode(home), name, f)
			if err != nil {
				t.Errorf("%s: re-parse home: %v", f, err)
				continue
			}
			if !reflect.DeepEqual(orig.Normalize(), re.Normalize()) {
				t.Errorf("%s: cross round-trip mismatch:\norig=%#v\nre  =%#v", f, orig.Normalize(), re.Normalize())
				continue
			}
			passHC++
		} else {
			nHA++
			orig, err := ParseHelixAgent(string(raw), f)
			if err != nil {
				t.Errorf("%s: parse: %v", f, err)
				continue
			}
			code, err := CanonicalToHelixCode(HelixAgentToCanonical(orig))
			if err != nil {
				t.Errorf("%s: to-code: %v", f, err)
				continue
			}
			codeBack, err := ParseHelixCode(EmitHelixCode(code), orig.Name, f)
			if err != nil {
				t.Errorf("%s: re-parse code: %v", f, err)
				continue
			}
			home, err := CanonicalToHelixAgent(HelixCodeToCanonical(codeBack))
			if err != nil {
				t.Errorf("%s: to-agent: %v", f, err)
				continue
			}
			re, err := ParseHelixAgent(EmitHelixAgent(home), f)
			if err != nil {
				t.Errorf("%s: re-parse home: %v", f, err)
				continue
			}
			if !reflect.DeepEqual(orig.Normalize(), re.Normalize()) {
				t.Errorf("%s: cross round-trip mismatch:\norig=%#v\nre  =%#v", f, orig.Normalize(), re.Normalize())
				continue
			}
			passHA++
		}
	}
	t.Logf("CROSS-DIALECT total=%d hc=%d/%d ha=%d/%d", len(files), passHC, nHC, passHA, nHA)
	if passHC != nHC || passHA != nHA {
		t.Fatalf("cross-dialect: hc %d/%d ha %d/%d", passHC, nHC, passHA, nHA)
	}
}

// TestCanonical_JSONStability: every corpus file -> canonical -> JSON ->
// canonical -> JSON is byte-stable.
func TestCanonical_JSONStability(t *testing.T) {
	files := collectSkillFiles(t, corpusRoots(repoRoot(t)))
	total, pass := 0, 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var canon *CanonicalSkill
		if DetectDialect(string(raw)) == DialectHelixCode {
			hc, err := ParseHelixCode(string(raw), filepath.Base(filepath.Dir(f)), f)
			if err != nil {
				t.Errorf("%s: parse: %v", f, err)
				continue
			}
			canon = HelixCodeToCanonical(hc)
		} else {
			ha, err := ParseHelixAgent(string(raw), f)
			if err != nil {
				t.Errorf("%s: parse: %v", f, err)
				continue
			}
			canon = HelixAgentToCanonical(ha)
		}
		total++
		js, err := json.Marshal(canon)
		if err != nil {
			t.Errorf("%s: marshal: %v", f, err)
			continue
		}
		var back CanonicalSkill
		if err := json.Unmarshal(js, &back); err != nil {
			t.Errorf("%s: unmarshal: %v", f, err)
			continue
		}
		js2, err := json.Marshal(&back)
		if err != nil {
			t.Errorf("%s: re-marshal: %v", f, err)
			continue
		}
		if string(js) != string(js2) {
			t.Errorf("%s: JSON unstable", f)
			continue
		}
		pass++
	}
	t.Logf("CANONICAL-JSON files=%d pass=%d", total, pass)
	if pass != total {
		t.Fatalf("JSON stability: %d/%d", pass, total)
	}
}

// BenchmarkCorpusConvert measures conversion cost over the full in-tree corpus
// (T-P1.08 test type: benchmarking). No assertion — the number is evidence.
func BenchmarkCorpusConvert(b *testing.B) {
	root := filepath.Join("..", "..")
	var files []string
	for _, r := range corpusRoots(root) {
		_ = filepath.Walk(r, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.ToUpper(info.Name()) == "SKILL.MD" {
				files = append(files, p)
			}
			return nil
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, f := range files {
			raw, _ := os.ReadFile(f)
			if DetectDialect(string(raw)) == DialectHelixCode {
				if hc, err := ParseHelixCode(string(raw), filepath.Base(filepath.Dir(f)), f); err == nil {
					_ = EmitHelixCode(mustCode(HelixCodeToCanonical(hc)))
				}
			} else if ha, err := ParseHelixAgent(string(raw), f); err == nil {
				_ = EmitHelixAgent(mustAgent(HelixAgentToCanonical(ha)))
			}
		}
	}
}

func mustAgent(c *CanonicalSkill) *HelixAgentFile {
	ha, err := CanonicalToHelixAgent(c)
	if err != nil {
		panic(err)
	}
	return ha
}

func mustCode(c *CanonicalSkill) *HelixCodeFile {
	hc, err := CanonicalToHelixCode(c)
	if err != nil {
		panic(err)
	}
	return hc
}
