# Requirements Checklist: HelixLLM Provider Exposure, Claude Toolkit Integration, and Heroku MCP Removal

**Purpose**: Track every requirement, verification step, and deliverable for the HelixLLM/Claude Toolkit integration and Heroku MCP removal.
**Created**: 2026-09-10
**Feature**: [specs/004-helixllm-claude-toolkit-integration/spec.md](spec.md)

## 1. Discovery & Scoping

- [ ] CHK001 Locate the Heroku MCP submodule path in `.gitmodules` and the working tree.
- [ ] CHK002 Identify every source file, import, build script, config file, and doc reference that mentions/wires Heroku MCP.
- [ ] CHK003 Locate the existing HelixAgent provider wiring in HelixCode (registry, factory, config, model list source).
- [ ] CHK004 Locate the HelixLLM submodule path and confirm how it exposes its model list.
- [ ] CHK005 Identify Claude Toolkit installation scripts, provider config files, and alias definitions for `kimi1`/`kimi2`.
- [ ] CHK006 Map Claude Toolkit's current provider/alias architecture (local vs remote endpoint handling).

## 2. Heroku MCP Removal (HelixCode)

- [ ] CHK007 Remove the Heroku MCP submodule directory and its `.gitmodules` entry.
- [ ] CHK008 Remove all Heroku MCP source files, tests, and fixture data.
- [ ] CHK009 Remove all Go imports, package references, and provider registrations for Heroku MCP.
- [ ] CHK010 Remove Heroku MCP build wiring from Makefiles, Docker files, and CI scripts.
- [ ] CHK011 Remove Heroku MCP configuration keys, env vars, and documentation references.
- [ ] CHK012 Verify `git status`/`git submodule status` show no Heroku MCP remnants.
- [ ] CHK013 Run `make build` in `helix_code/` after removal and confirm success.

## 3. HelixLLM Provider Exposure (HelixCode)

- [ ] CHK014 Register HelixLLM in the HelixCode provider registry using the same abstraction as HelixAgent.
- [ ] CHK015 Source HelixLLM's model list dynamically from the HelixLLM submodule (no hardcoded lists beyond constitutional fallback).
- [ ] CHK016 Implement/request HelixLLM generation dispatch (HTTP/gRPC/whatever the submodule exposes).
- [ ] CHK017 Add HelixLLM configuration schema (host, port, auth, enabled flag) to HelixCode config.
- [ ] CHK018 Add HelixLLM env-var binding and example config entries.
- [ ] CHK019 Add unit/integration tests proving HelixLLM provider registration and request formatting.
- [ ] CHK020 Run the relevant HelixCode tests and confirm green.

## 4. Claude Toolkit Provider Binding

- [ ] CHK021 Add HelixAgent, HelixLLM, and HelixLLM provider endpoint configuration to Claude Toolkit.
- [ ] CHK022 Support local mode (localhost / default ports) for all three providers.
- [ ] CHK023 Support LAN mode (configurable host + port) for all three providers.
- [ ] CHK024 Support remote/cloud mode (configurable host + port + TLS/auth) for all three providers.
- [ ] CHK025 Ensure endpoint changes take effect on reload/restart without source edits.
- [ ] CHK026 Add installation/setup script updates to prompt for/select provider topology.
- [ ] CHK027 Add example configuration files for local, LAN, and remote scenarios.
- [ ] CHK028 Wire the provider endpoints into Claude Toolkit's request dispatch path.

## 5. kimi1 / kimi2 Alias Fix (Claude Toolkit)

- [ ] CHK029 Reproduce the endless context-compaction loop with `kimi1` and `kimi2` aliases.
- [ ] CHK030 Root-cause the loop (alias resolution, model mapping, context budget, retry logic, etc.).
- [ ] CHK031 Fix alias resolution so `kimi1` and `kimi2` map to a valid model/provider configuration.
- [ ] CHK032 Add guardrails to prevent unbounded context compaction for any alias.
- [ ] CHK033 Verify repeated requests via `kimi1` and `kimi2` complete and context size stays bounded.
- [ ] CHK034 Add deterministic regression tests for alias resolution and context bounds.

## 6. Live Installation & Verification

- [ ] CHK035 Run the HelixCode bash installation/setup script on a clean state.
- [ ] CHK036 Run the Claude Toolkit bash installation/setup script on a clean state.
- [ ] CHK037 Start HelixAgent, HelixLLM, and HelixLLM providers locally and confirm health.
- [ ] CHK038 Send a live request from Claude Toolkit through local HelixAgent and capture evidence.
- [ ] CHK039 Send a live request from Claude Toolkit through local HelixLLM and capture evidence.
- [ ] CHK040 Send a live request from Claude Toolkit through local HelixLLM provider and capture evidence.
- [ ] CHK041 Repeat live requests against a LAN-hosted endpoint and capture evidence.
- [ ] CHK042 Repeat live requests against a remote/cloud endpoint and capture evidence (or document honest SKIP).
- [ ] CHK043 Produce deterministic evidence artifacts (logs, JSON verdicts, screenshots) for each topology.

## 7. Documentation

- [ ] CHK044 Update HelixCode README with HelixLLM provider setup and removal of Heroku MCP references.
- [ ] CHK045 Add/update HelixCode provider documentation for HelixLLM.
- [ ] CHK046 Update Claude Toolkit README with provider installation, local/LAN/remote configuration, and alias setup.
- [ ] CHK047 Document the `kimi1`/`kimi2` alias configuration and the fix.
- [ ] CHK048 Add a live verification guide with copy-paste commands and expected evidence outputs.
- [ ] CHK049 Update relevant submodule documentation (HelixLLM, HelixAgent, constitution, etc.) to reflect changes.
- [ ] CHK050 Ensure every doc change is reachable from its repository's README per §11.4.212.

## 8. Cross-Repository Commit & Merge Discipline

- [ ] CHK051 Commit HelixCode changes with descriptive messages citing this spec.
- [ ] CHK052 Commit Claude Toolkit changes with descriptive messages citing this spec.
- [ ] CHK053 Commit any touched submodule changes with descriptive messages.
- [ ] CHK054 Fetch and merge upstream changes before each push (§11.4.113).
- [ ] CHK055 Push all commits to every configured upstream for HelixCode, Claude Toolkit, and touched submodules.
- [ ] CHK056 Verify no force-push was used and no conflict markers remain.

## Notes

- Check items off as completed: `[x]`
- Link evidence artifacts inline next to each verification item.
- Items are numbered sequentially for easy reference in commit messages and evidence logs.
