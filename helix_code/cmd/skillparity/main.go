// Command skillparity implements gate CM-CROSS-CONSUMER-PARITY (HXC-159
// T-P7.02): it reads two capability-manifest JSON files (one per
// consumer, T-P7.01) and diffs them via internal/skillparity.Diff,
// printing the result and exiting non-zero on any unexplained asymmetry.
//
// Usage:
//
//	skillparity -a helix_code_report.json -b helix_agent_report.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"dev.helix.code/internal/skillconformance"
	"dev.helix.code/internal/skillparity"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("skillparity", flag.ContinueOnError)
	aPath := fs.String("a", "", "path to the first consumer's capability manifest JSON (required)")
	bPath := fs.String("b", "", "path to the second consumer's capability manifest JSON (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *aPath == "" || *bPath == "" {
		fmt.Fprintln(os.Stderr, "skillparity: -a and -b are both required")
		return 2
	}

	a, err := loadManifest(*aPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skillparity: %v\n", err)
		return 1
	}
	b, err := loadManifest(*bPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skillparity: %v\n", err)
		return 1
	}
	if errs := a.Validate(); len(errs) != 0 {
		fmt.Fprintf(os.Stderr, "skillparity: %s failed schema validation: %v\n", *aPath, errs)
		return 1
	}
	if errs := b.Validate(); len(errs) != 0 {
		fmt.Fprintf(os.Stderr, "skillparity: %s failed schema validation: %v\n", *bPath, errs)
		return 1
	}

	d := skillparity.Diff(a, b)
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "skillparity: encode diff: %v\n", err)
		return 1
	}
	fmt.Println(string(raw))

	if !d.OK() {
		fmt.Fprintf(os.Stderr, "skillparity: FAIL — %d surface asymmetries, %d skill asymmetries, none explicitly marked optional on both sides\n",
			len(d.SurfaceAsymmetries), len(d.SkillAsymmetries))
		return 1
	}
	fmt.Fprintln(os.Stderr, "skillparity: PASS — no unexplained asymmetry")
	return 0
}

func loadManifest(path string) (skillconformance.CapabilityManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return skillconformance.CapabilityManifest{}, fmt.Errorf("read %q: %w", path, err)
	}
	var m skillconformance.CapabilityManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return skillconformance.CapabilityManifest{}, fmt.Errorf("parse %q: %w", path, err)
	}
	return m, nil
}
