// Command mcpgen generates .mcp.json MCP-server entries from a skills
// manifest (T-P5.04, HXC-159 Phase P5). See internal/mcpgen for the
// generation logic; this file is a thin CLI wrapper.
//
// Every input is a flag — manifest path, source root, and target file — so
// this tool stays decoupled from any one project's directory layout (D-3 /
// CONST-051(B)): there is no hardcoded path to the real repository's
// scripts/skills_manifest.tsv or .mcp.json anywhere in this package.
//
// Usage:
//
//	mcpgen generate --manifest FILE --root DIR --out FILE
//	mcpgen check    --manifest FILE --root DIR --out FILE   # exit 0 iff no drift
//	mcpgen probe    --manifest FILE --root DIR              # start+wait every generated entry
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"dev.helix.code/internal/mcpgen"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	manifestPath := fs.String("manifest", "", "path to the TSV skills manifest (name<TAB>source<TAB>owner)")
	root := fs.String("root", ".", "filesystem root the manifest's source column is relative to")
	out := fs.String("out", ".mcp.json", "target .mcp.json path to write/check")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	switch sub {
	case "generate", "check", "probe":
		// fall through
	default:
		usage()
		os.Exit(2)
	}

	if *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "mcpgen: --manifest is required")
		os.Exit(2)
	}

	rows, err := mcpgen.ParseManifestFile(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcpgen: %v\n", err)
		os.Exit(1)
	}

	switch sub {
	case "generate":
		if err := mcpgen.WriteMerged(*out, rows, *root); err != nil {
			fmt.Fprintf(os.Stderr, "mcpgen: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("mcpgen: wrote %s (%d manifest row(s) considered)\n", *out, len(rows))

	case "check":
		drifted, err := mcpgen.Drift(*out, rows, *root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcpgen: %v\n", err)
			os.Exit(1)
		}
		if len(drifted) == 0 {
			fmt.Printf("mcpgen: CM-MCP-ENTRIES-GENERATED: WIRED — %s matches the manifest for all %d generated entr(y/ies)\n", *out, len(rows))
			return
		}
		fmt.Printf("mcpgen: CM-MCP-ENTRIES-GENERATED: NOT-WIRED — drifted entries: %v\n", drifted)
		os.Exit(1)

	case "probe":
		entries, genErr := mcpgen.Generate(rows, *root)
		if genErr != nil {
			fmt.Fprintf(os.Stderr, "mcpgen: %v\n", genErr)
			os.Exit(1)
		}
		ctx := context.Background()
		failed := 0
		for name, entry := range entries {
			result, probeErr := mcpgen.Probe(ctx, entry, *root)
			status := "OK"
			if probeErr != nil {
				status = "FAIL: " + probeErr.Error()
				failed++
			}
			fmt.Printf("mcpgen probe %-20s started=%-5v %s\n", name, result.Started, status)
		}
		if failed > 0 {
			os.Exit(1)
		}
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mcpgen <generate|check|probe> --manifest FILE --root DIR [--out FILE]")
}
