// Command skillconformance runs HelixCode's OWN skillconformance report
// generator (HXC-159 T-P7.01/T-P7.02, RS-12) against real on-disk skill
// directories using the production commands.SkillLoader, and writes the
// resulting capability manifest as JSON.
//
// Usage:
//
//	skillconformance -project <dir> -user <dir> [-external <dir>] -out report.json
//
// A blank -user/-external falls back to the production defaults
// (agent.UserSkillsDirs()[1], agent.ExternalSkillsDir()).
package main

import (
	"flag"
	"fmt"
	"os"

	"dev.helix.code/internal/agent"
	"dev.helix.code/internal/commands"
	"dev.helix.code/internal/skillconformance"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("skillconformance", flag.ContinueOnError)
	projectDir := fs.String("project", "", "project skills directory (conventionally .helix/skills)")
	userDir := fs.String("user", "", "user skills directory (default: the canonical user skills dir)")
	externalDir := fs.String("external", "", "external skills directory (default: HELIXCODE_EXTERNAL_SKILLS_DIR / .claude/skills)")
	outPath := fs.String("out", "", "output path for the JSON report (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	resolvedUser := *userDir
	if resolvedUser == "" {
		dirs := agent.UserSkillsDirs()
		if len(dirs) > 0 {
			resolvedUser = dirs[len(dirs)-1] // canonical is last (highest precedence)
		}
	}
	resolvedExternal := *externalDir
	if resolvedExternal == "" {
		resolvedExternal = agent.ExternalSkillsDir()
	}

	reg := commands.NewSkillRegistry()
	loader := commands.NewSkillLoader(reg, *projectDir, resolvedUser)
	if err := loader.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "skillconformance: load project/user tiers: %v\n", err)
		return 1
	}
	// The external tier is a THIRD directory this package's SkillLoader
	// does not natively accept (it takes exactly project+user); load it
	// with a second registry pass over the same shared registry so its
	// skills are reported too, tagged distinctly by tier via DirTierMap.
	// HONEST LIMITATION: this two-pass demo composes tiers in call order
	// (project/user, then external), so a same-named external skill
	// would overwrite a same-named project skill here — the OPPOSITE of
	// AllSkillsDirs' documented [legacy, canonical, external, project]
	// precedence. No single production call site actually feeds all
	// four AllSkillsDirs() tiers through one commands.SkillLoader today
	// (T-P5.05.4's own honest-boundary note); this CLI is a
	// report-generation demo over real fixtures/directories, not a
	// re-implementation of production precedence ordering.
	if resolvedExternal != "" {
		externalLoader := commands.NewSkillLoader(reg, resolvedExternal, "")
		if err := externalLoader.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "skillconformance: load external tier: %v\n", err)
			return 1
		}
	}

	report := skillconformance.GenerateReport(reg, skillconformance.DirTierMap{
		Project:  *projectDir,
		User:     resolvedUser,
		External: resolvedExternal,
	})
	if errs := report.Validate(); len(errs) != 0 {
		fmt.Fprintln(os.Stderr, "skillconformance: generated report failed its own schema validation:")
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %v\n", e)
		}
		return 1
	}

	raw, err := report.MarshalIndentJSON()
	if err != nil {
		fmt.Fprintf(os.Stderr, "skillconformance: encode: %v\n", err)
		return 1
	}
	if *outPath == "" {
		os.Stdout.Write(raw)
		return 0
	}
	if err := os.WriteFile(*outPath, raw, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "skillconformance: write %q: %v\n", *outPath, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "skillconformance: wrote %s (%d skills)\n", *outPath, len(report.Skills))
	return 0
}
