// Command skillconv exercises the skillconv converters over real corpora and
// reports machine-readable round-trip counts. It is the runnable half of the
// T-P1.08 tool module (scripts/skillconv): the library does the conversion,
// this command proves it over whatever checkout it is pointed at.
//
// Usage:
//
//	skillconv roundtrip --root /path/to/repo [--hc dir ...] [--ha dir ...]
//
// With no --hc/--ha flags the in-tree default corpus roots are used
// (helix_code/internal/commands/builtin_skills, constitution/skills,
// submodules/helix_agent/skills). Exit 0 only when every file round-trips in
// both same-dialect and cross-dialect directions; the JSON report on stdout
// is the acceptance evidence (per-corpus pass counts).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	skillconv "dev.helix.code/scripts/skillconv"
)

type corpusReport struct {
	Corpus    string   `json:"corpus"`
	Dialect   string   `json:"dialect"`
	Files     int      `json:"files"`
	SamePass  int      `json:"same_pass"`
	CrossPass int      `json:"cross_pass"`
	JSONPass  int      `json:"json_pass"`
	Failures  []string `json:"failures,omitempty"`
}

type fullReport struct {
	Schema  string         `json:"schema"`
	Root    string         `json:"root"`
	Corpora []corpusReport `json:"corpora"`
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "roundtrip" {
		fmt.Fprintln(os.Stderr, "usage: skillconv roundtrip --root <repo> [--hc dir ...] [--ha dir ...]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("roundtrip", flag.ExitOnError)
	root := fs.String("root", "", "repository root containing the corpora")
	var hcDirs, haDirs multiFlag
	fs.Var(&hcDirs, "hc", "extra HelixCode corpus dir (repeatable)")
	fs.Var(&haDirs, "ha", "extra HelixAgent corpus dir (repeatable)")
	_ = fs.Parse(os.Args[2:])
	if *root == "" {
		fmt.Fprintln(os.Stderr, "skillconv: --root is required")
		os.Exit(2)
	}
	roots := append([]string{
		filepath.Join(*root, "helix_code", "internal", "commands", "builtin_skills"),
		filepath.Join(*root, "constitution", "skills"),
		filepath.Join(*root, "submodules", "helix_agent", "skills"),
	}, append([]string(hcDirs), []string(haDirs)...)...)

	rep := fullReport{Schema: "skillconv-roundtrip/v1", Root: *root}
	ok := true
	for _, r := range roots {
		cr := runCorpus(r)
		rep.Corpora = append(rep.Corpora, cr)
		if len(cr.Failures) > 0 {
			ok = false
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rep)
	if !ok {
		os.Exit(1)
	}
}

func runCorpus(root string) corpusReport {
	cr := corpusReport{Corpus: root}
	var files []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.ToUpper(info.Name()) == "SKILL.MD" {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	cr.Files = len(files)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			cr.Failures = append(cr.Failures, f+": read: "+err.Error())
			continue
		}
		if cr.Dialect == "" {
			cr.Dialect = "mixed"
		}
		if skillconv.DetectDialect(string(raw)) == skillconv.DialectHelixCode {
			name := filepath.Base(filepath.Dir(f))
			orig, err := skillconv.ParseHelixCode(string(raw), name, f)
			if err != nil {
				cr.Failures = append(cr.Failures, f+": parse: "+err.Error())
				continue
			}
			canon := skillconv.HelixCodeToCanonical(orig)
			back, _ := skillconv.CanonicalToHelixCode(canon)
			re, err := skillconv.ParseHelixCode(skillconv.EmitHelixCode(back), name, f)
			if err != nil {
				cr.Failures = append(cr.Failures, f+": re-parse: "+err.Error())
				continue
			}
			if !reflect.DeepEqual(orig.Normalize(), re.Normalize()) {
				cr.Failures = append(cr.Failures, f+": same-dialect mismatch")
				continue
			}
			cr.SamePass++
			if crossHC(orig, name, f) {
				cr.CrossPass++
			} else {
				cr.Failures = append(cr.Failures, f+": cross-dialect mismatch")
			}
			if jsonStable(canon) {
				cr.JSONPass++
			} else {
				cr.Failures = append(cr.Failures, f+": json unstable")
			}
		} else {
			orig, err := skillconv.ParseHelixAgent(string(raw), f)
			if err != nil {
				cr.Failures = append(cr.Failures, f+": parse: "+err.Error())
				continue
			}
			canon := skillconv.HelixAgentToCanonical(orig)
			back, _ := skillconv.CanonicalToHelixAgent(canon)
			re, err := skillconv.ParseHelixAgent(skillconv.EmitHelixAgent(back), f)
			if err != nil {
				cr.Failures = append(cr.Failures, f+": re-parse: "+err.Error())
				continue
			}
			if !reflect.DeepEqual(orig.Normalize(), re.Normalize()) {
				cr.Failures = append(cr.Failures, f+": same-dialect mismatch")
				continue
			}
			cr.SamePass++
			if crossHA(orig, f) {
				cr.CrossPass++
			} else {
				cr.Failures = append(cr.Failures, f+": cross-dialect mismatch")
			}
			if jsonStable(canon) {
				cr.JSONPass++
			} else {
				cr.Failures = append(cr.Failures, f+": json unstable")
			}
		}
	}
	return cr
}

func crossHC(orig *skillconv.HelixCodeFile, name, path string) bool {
	agent, err := skillconv.CanonicalToHelixAgent(skillconv.HelixCodeToCanonical(orig))
	if err != nil {
		return false
	}
	agentBack, err := skillconv.ParseHelixAgent(skillconv.EmitHelixAgent(agent), path)
	if err != nil {
		return false
	}
	home, err := skillconv.CanonicalToHelixCode(skillconv.HelixAgentToCanonical(agentBack))
	if err != nil {
		return false
	}
	re, err := skillconv.ParseHelixCode(skillconv.EmitHelixCode(home), name, path)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(orig.Normalize(), re.Normalize())
}

func crossHA(orig *skillconv.HelixAgentFile, path string) bool {
	code, err := skillconv.CanonicalToHelixCode(skillconv.HelixAgentToCanonical(orig))
	if err != nil {
		return false
	}
	codeBack, err := skillconv.ParseHelixCode(skillconv.EmitHelixCode(code), orig.Name, path)
	if err != nil {
		return false
	}
	home, err := skillconv.CanonicalToHelixAgent(skillconv.HelixCodeToCanonical(codeBack))
	if err != nil {
		return false
	}
	re, err := skillconv.ParseHelixAgent(skillconv.EmitHelixAgent(home), path)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(orig.Normalize(), re.Normalize())
}

func jsonStable(c *skillconv.CanonicalSkill) bool {
	js, err := json.Marshal(c)
	if err != nil {
		return false
	}
	var back skillconv.CanonicalSkill
	if err := json.Unmarshal(js, &back); err != nil {
		return false
	}
	js2, err := json.Marshal(&back)
	if err != nil {
		return false
	}
	return string(js) == string(js2)
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
