package clientcore

// agentic.go — the SHARED agentic-capability wiring every HelixCode client
// reuses: the read-only tool registry (git_status + fs_read/glob/grep) with LSP
// diagnostics + MCP tools merged in, skills + plugins loading, and the
// tool-loop system prompt + tool-trace adaptation.
//
// This is the exact wiring the terminal UI performed inline in its Initialize
// (applications/terminal_ui/main.go). Promoting it here lets the desktop GUI
// call the SAME code instead of reimplementing it (§11.4.74). It contains NO
// UI-toolkit types, so it is reusable by every client (CONST-051(B)).
//
// SAFETY (§11.4.133): the registry is built with tools.NewToolRegistry(nil) — no
// approval manager — and the agentic tool loop MUST run with ReadOnlyOnly=true
// so nothing destructive (fs_write/fs_edit/shell) is ever reachable. Every
// wiring step degrades gracefully: a missing config / a Start failure / zero
// tools all log + continue — the client still runs without that capability.

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dev.helix.code/internal/agent"
	"dev.helix.code/internal/commands"
	"dev.helix.code/internal/ensembleui"
	"dev.helix.code/internal/mcp"
	"dev.helix.code/internal/plugins"
	"dev.helix.code/internal/tools"
	"dev.helix.code/internal/tools/git"

	"go.uber.org/zap"
)

// AgenticTools bundles the read-only tool registry and the capability
// sub-managers wired onto it. The registry drives the agentic tool loop; the
// sub-managers are retained so the client's Close can release their resources
// (MCP child processes, LSP server processes). Any field may be nil when its
// wiring failed or nothing is configured/installed — the caller degrades
// gracefully.
type AgenticTools struct {
	Registry   *tools.ToolRegistry
	MCPManager *mcp.Manager
	LSPManager *tools.LSPManager
}

// Close releases the agentic sub-managers' resources (MCP child processes). It
// is nil-safe at every level so a partially-wired AgenticTools (or a nil
// receiver) closes cleanly.
func (a *AgenticTools) Close() {
	if a == nil {
		return
	}
	if a.MCPManager != nil {
		_ = a.MCPManager.Close()
	}
}

// WireAgenticTools builds the read-only agentic tool registry and wires LSP +
// MCP onto it, exactly as the TUI does. It returns a fully-wired *AgenticTools
// on success. On registry-construction failure it returns (nil, err) so the
// caller can fall back to the plain streaming chat path; LSP/MCP sub-failures
// are non-fatal (logged + skipped) and never error the call.
//
// repoDir is resolved to the enclosing git repository root (walking up for
// .git) so git_status inspects the real repo even when the client is launched
// from a subdirectory. mcpProjectPath is the project-local MCP config path
// (e.g. ".helixcode/mcp.yml"); the user path (~/.helixcode/mcp.yml) is merged
// over it.
func WireAgenticTools(mcpProjectPath string) (*AgenticTools, error) {
	reg, regErr := tools.NewToolRegistry(nil)
	if regErr != nil {
		return nil, regErr
	}

	repoDir, wdErr := os.Getwd()
	if wdErr != nil {
		repoDir = "."
	}
	repoDir = resolveRepoRoot(repoDir)

	// fs_read / glob / grep are auto-registered by NewToolRegistry (all three
	// LevelReadOnly). Add the read-only git_status inspection capability pinned
	// to the repo root. No write/exec tool is added.
	reg.Register(git.NewGitStatusTool(repoDir))

	at := &AgenticTools{Registry: reg}

	// Wire LSP — registers the two read-only diagnostics tools. WireLSP always
	// returns a non-nil manager even when no LSP server is on PATH (its spec set
	// is then empty and every call is a no-op), so the wiring is branch-free and
	// degrades gracefully.
	at.LSPManager = tools.WireLSP(reg, repoDir, zap.NewNop())
	log.Printf("✅ clientcore: LSP diagnostics tools wired (lsp_get_diagnostics, lsp_analyze_diagnostic)")

	// Wire MCP — merge the user + project MCP server configs, start the
	// alwaysLoad servers, and register their tools onto the registry. Servers
	// marked readOnly:true in config register their tools at LevelReadOnly so
	// they pass the ReadOnlyOnly agent loop. Graceful at every failure point.
	mcpUserPath := filepath.Join(os.Getenv("HOME"), ".helixcode", "mcp.yml")
	mcpCfg, mcpErr := mcp.LoadMerged(mcpUserPath, mcpProjectPath)
	switch {
	case mcpErr != nil:
		log.Printf("⚠️  clientcore: MCP config load failed (%v); continuing without MCP tools", mcpErr)
	case mcpCfg == nil || len(mcpCfg.Servers) == 0:
		log.Printf("ℹ️  clientcore: no MCP servers configured; continuing without MCP tools")
	default:
		mgr := mcp.NewManager()
		mgr.SetConfig(mcpCfg)
		if startErr := mgr.Start(context.Background()); startErr != nil {
			log.Printf("⚠️  clientcore: MCP start failed (%v); continuing without MCP tools", startErr)
		} else {
			reg.RegisterMCPManager(mgr)
			at.MCPManager = mgr
			log.Printf("✅ clientcore: MCP wired — %d tool(s) registered from %d server(s)", len(mgr.Tools()), len(mcpCfg.Servers))
		}
	}

	log.Printf("✅ clientcore: agentic tool registry ready (read-only: git_status, fs_read, glob, grep + LSP/MCP tools)")
	return at, nil
}

// Skills bundles the loaded skill registry + dispatcher and the plugin loader.
// Any field may be nil when its load failed or nothing is installed — the
// caller skips the corresponding step.
type Skills struct {
	Registry     *commands.SkillRegistry
	Dispatcher   *agent.SkillDispatcher
	PluginLoader *plugins.Loader
}

// LoadSkillsAndPlugins loads `.md` skills from the user dir then the project dir
// (project last so it overrides on collision), and the plugins from pluginsDir.
// Both degrade gracefully: a load error or missing directory yields a nil
// dispatcher / loader. Mirrors the TUI's startup wiring.
func LoadSkillsAndPlugins(pluginsDir string) *Skills {
	s := &Skills{}

	// T-P5.03 + T-P5.05 (HXC-159 Phase P5): scan the full ordered skills-dir
	// tier — agent.AllSkillsDirs returns [legacy ~/.helix/skills, canonical
	// os.UserConfigDir()/helixcode/skills, external .claude/skills,
	// projectDir], project last so it overrides on collision. Before T-P5.03
	// clientcore scanned ONLY ~/.helix/skills (a skill installed via the
	// CLI's convention was invisible here); before T-P5.05 an
	// externally-provisioned skill (e.g. via scripts/register_skills.sh) was
	// invisible everywhere. See docs/guides/skills_directory_migration.md.
	skillDirs := agent.AllSkillsDirs(".helix/skills")
	if skReg, skDisp, skErr := agent.LoadSkillsAndDispatcher(skillDirs); skErr != nil {
		log.Printf("⚠️  clientcore: skills unavailable (%v); chat continues without skill triggers", skErr)
	} else {
		s.Registry = skReg
		s.Dispatcher = skDisp
		log.Printf("✅ clientcore: skills loaded (%d available)", len(skReg.List()))
	}

	if loader, plErr := plugins.LoadPlugins(context.Background(), pluginsDir); plErr != nil {
		log.Printf("⚠️  clientcore: plugins unavailable (%v); chat continues without plugin triggers", plErr)
	} else {
		s.PluginLoader = loader
		log.Printf("✅ clientcore: plugins loaded from %s", pluginsDir)
	}

	return s
}

// BuildToolLoopSystemPrompt composes the agent-steering system prompt from the
// registry's live tool names (CONST-046 — structural, not hardcoded user-facing
// prose). It asserts the agent is operating INSIDE the user's real codebase and
// REQUIRES a tool call before any claim about the codebase, so a model can never
// answer "I cannot see your files" from memory.
func BuildToolLoopSystemPrompt(registry *tools.ToolRegistry) string {
	names := make([]string, 0)
	for _, t := range registry.List() {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	return "You are the Helix coding agent, operating INSIDE the user's real codebase at the current working directory. " +
		"You have these tools available: " + strings.Join(names, ", ") + ". " +
		"These tools give you genuine read access to the user's files and git state — you CAN see the codebase. " +
		"When the user asks whether you can see or access their codebase (or anything about its files, structure, or git state), " +
		"you MUST call a tool FIRST (e.g. glob to list files, git_status to inspect the repo, fs_read to read a file) and then " +
		"answer from what the tool actually returned — concretely (how many files, which languages, the repository's state). " +
		"NEVER claim you cannot see or access the codebase without first calling a tool: you have both the tools and the access. " +
		"Prefer calling a tool over guessing."
}

// AdaptToolTrace converts the agent loop's []agent.ToolTraceEntry into the
// shared []ensembleui.ToolTraceLine that ensembleui.FormatToolTrace consumes, so
// clients never expose internal/agent's type to the render helper.
func AdaptToolTrace(entries []agent.ToolTraceEntry) []ensembleui.ToolTraceLine {
	out := make([]ensembleui.ToolTraceLine, len(entries))
	for i, e := range entries {
		out[i] = ensembleui.ToolTraceLine{
			ToolName:  e.ToolName,
			Output:    e.Output,
			Err:       e.Err,
			Arguments: e.Arguments,
		}
	}
	return out
}

// resolveRepoRoot walks up from start looking for a directory containing a .git
// entry and returns it (the enclosing git repository root). When no .git is
// found on the way to the filesystem root, it returns start unchanged — so
// git_status still operates on the working directory as a sensible fallback.
func resolveRepoRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}
