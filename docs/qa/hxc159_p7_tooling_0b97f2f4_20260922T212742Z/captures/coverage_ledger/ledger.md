# HXC-159 T-P7.04 Coverage Ledger

Parsed 57 power-feature rows (§5, expected 57) + 23 extension rows (§6, expected 23) = 80 total.

| ID | Kind | Feature | Disposition | Evidence-state | Evidence / Reason |
|---|---|---|---|---|---|
| A1 | power-feature | Skill CRUD store | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| A2 | power-feature | Dependency-graph writes | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| A3 | power-feature | Tree traversal / closure resolver | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| A4 | power-feature | Cycle prevention | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| A5 | power-feature | Granularity / composition | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| A6 | power-feature | TOML import/export round-trip | UPS | skip | SKIP: upstream-only per spec.md §5 disposition; not surfaced to consumers this feature. |
| A7 | power-feature | Evidence attachment (10 methods) | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| A8 | power-feature | Resource attachment + revalidation | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| A9 | power-feature | Registry health / coverage | INC | covered | submodules/skills/pkg/skills/conformance.go (HXC-159 T-P7.01): LibrarySelfReport feeds the capability manifest behind the FR-035/T-P7.02 parity report; qa-results/hxc159/conformance/20260922T212742Z/02_green_library_conformance.txt. |
| A10 | power-feature | Scheduled registry review | DEF | skip | SKIP: deferred per spec.md's own named reason; see tasks.md/spec.md for the tracked follow-on. |
| B1 | power-feature | Hybrid text search (pg_trgm) | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| B2 | power-feature | Vector / semantic search (pgvector) | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| B3 | power-feature | `Embedder` interface | INC | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=INC. |
| B4 | power-feature | Batch embedding with progress | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| B5 | power-feature | Write-through embedding cache | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| C1 | power-feature | LLM auto-expansion pipeline | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| C2 | power-feature | Multi-provider LLM client | INC | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=INC. |
| C3 | power-feature | Validation pipeline | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| C4 | power-feature | LLM jury validation | INC | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=INC. |
| C5 | power-feature | Isolated/sandboxed execution | UPS | covered | submodules/skills/pkg/skills/sandbox_gate.go + sandbox_gate_test.go (HXC-159 T-P7.05.5): EnforceSandboxPrecondition refuses third-party activation while sandboxReady=false; qa-results/hxc159/security/20260922T212742Z/04_green_c5_gate.txt (4/4 PASS). |
| C6 | power-feature | Tree-sitter code analysis | DEF | skip | SKIP: deferred per spec.md's own named reason; see tasks.md/spec.md for the tracked follow-on. |
| C7 | power-feature | Project analyzer → skill mapping | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| C8 | power-feature | CodeGraph pattern extraction | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| C9 | power-feature | CodeGraph index manager + sync | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| C10 | power-feature | CodeGraph MCP client | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| D1 | power-feature | PostgreSQL pool + tx | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| D2 | power-feature | Cache abstraction | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| D3 | power-feature | Prometheus metrics | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| D4 | power-feature | Per-tenant metrics | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| D5 | power-feature | Audit log (10 functions) | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| D6 | power-feature | Multi-tenancy | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| D7 | power-feature | Tenant audit logger | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| D8 | power-feature | Background worker (6 job types) | DEF | skip | SKIP: deferred per spec.md's own named reason; see tasks.md/spec.md for the tracked follow-on. |
| D9 | power-feature | TOON serialization | N/S | skip | SKIP: explicitly out of scope per spec.md §5/§6 disposition (a scope decision, not a gap). |
| D10 | power-feature | TOML config + `${VAR}` interpolation | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| E1 | power-feature | Skill-source registry | INC | covered | pkg/skills/registry.go RegisterSource/Sources, exercised live via cmd/conformance-report; qa-results/hxc159/conformance/20260922T212742Z/04_library_cli_run_wellformed.txt (5 real skills registered from a real source). |
| E2 | power-feature | Sync orchestrator (3 seams) | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| E3 | power-feature | Source events | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| E4 | power-feature | Generic `Source` abstraction | INC | covered | pkg/skills/source.go Source{Name,Root,Dialect,Precedence,Trust} exercised the same way as E1 (same CLI run, same evidence file) — Source is the abstraction that made the constitution/skills fixture registrable. |
| E5 | power-feature | Filesystem source | **INC (degraded)** | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=**INC (degraded)**. |
| E6 | power-feature | GitHub source client | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| E7 | power-feature | Ingestion pipeline | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| E8 | power-feature | `SKILL.md` parser | INC | covered | pkg/skills/loader.go LoadSource/parseSkillFile parsed 5 real constitution SKILL.md files (well-formed) AND correctly fail-closed-refused the whole source on 3 malformed ones; qa-results/hxc159/conformance/20260922T212742Z/03_library_cli_run.txt (real failure) + 04_library_cli_run_wellformed.txt (real success). |
| E9 | power-feature | Source → model mapper | INC | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=INC. |
| E10 | power-feature | Dedup classifier | INC | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=INC. |
| F1 | power-feature | MCP server (13 tools) | DEP | skip | SKIP: MCP server liveness was already proven in Phase P6 (T-P6.02, RS-17, qa-results/hxc159/agent_mcp/20260922T192508Z/) — not re-exercised in this P7 pass; citing the P6 evidence rather than re-testing. |
| F2 | power-feature | MCP stdio transport | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| F3 | power-feature | MCP HTTP transport | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| F4 | power-feature | ACP adapter | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| F5 | power-feature | Agent config emitters | INC | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=INC. |
| F6 | power-feature | MCP system prompts | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| F7 | power-feature | REST API (12 handlers) | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| F8 | power-feature | HTTP middleware stack | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| F9 | power-feature | HTTP/2 + HTTP/3 + Brotli | N/S | skip | SKIP: explicitly out of scope per spec.md §5/§6 disposition (a scope decision, not a gap). |
| F10 | power-feature | Cobra CLI (27 subcommands) | DEP | skip | SKIP: out-of-process dependency (D-3): tested within HelixDevelopment/skills' own upstream test suite, not retested at the consumer seam this cycle. |
| F11 | power-feature | Bubbletea TUI | N/S | skip | SKIP: explicitly out of scope per spec.md §5/§6 disposition (a scope decision, not a gap). |
| F12 | power-feature | Catalogue generator (Layer 3) | INC | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=INC. |
| 6-REUSE-01 | extension | credential_scan_lib | REUSE | covered | credential_scan_lib.sh reused UNCHANGED per FR-027 (T-P7.05.3): qa-results/hxc159/security/20260922T212742Z/01_credential_scan_skills_library.txt (651 files scanned) + 02_credential_scan_helixagent_skills_corpus.txt (1330 files scanned, 1174-skill vendored corpus). |
| 6-REUSE-02 | extension | guard-forbidden-commands | REUSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=REUSE. |
| 6-REUSE-03 | extension | guard-track-branch-label | REUSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=REUSE. |
| 6-REUSE-04 | extension | guard-branch-consistency | REUSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=REUSE. |
| 6-REUSE-05 | extension | guard-work-track-binding | REUSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=REUSE. |
| 6-REUSE-06 | extension | SpecKit | REUSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=REUSE. |
| 6-REUSE-07 | extension | Superpowers | REUSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=REUSE. |
| 6-EXTEND-01 | extension | actions/registry.yaml | EXTEND | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=EXTEND. |
| 6-COMPOSE-01 | extension | action-prefix-system | COMPOSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=COMPOSE. |
| 6-COMPOSE-02 | extension | reporting-workable-items | COMPOSE | covered | reporting-workable-items (constitution skill) was a REAL fixture in every T-P7.01/T-P7.02 conformance run — registered, loaded, hashed; qa-results/hxc159/conformance/20260922T212742Z/library_report.json + parity/20260922T212742Z/helix_agent_report_samecorpus.json. |
| 6-COMPOSE-03 | extension | scheduled-work-queue | COMPOSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=COMPOSE. |
| 6-COMPOSE-04 | extension | workable-item-lifecycle | COMPOSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=COMPOSE. |
| 6-COMPOSE-05 | extension | scheduled-work-mcp.json | COMPOSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=COMPOSE. |
| 6-COMPOSE-06 | extension | `helix` plugin | COMPOSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=COMPOSE. |
| 6-COMPOSE-07 | extension | `scheduled-work` plugin | COMPOSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=COMPOSE. |
| 6-COMPOSE-08 | extension | action_prefix_expand.sh | COMPOSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=COMPOSE. |
| 6-COMPOSE-09 | extension | post-merge | COMPOSE | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=COMPOSE. |
| 6-ORTHOGONAL-01 | extension | media-validator | ORTHOGONAL | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=ORTHOGONAL. |
| 6-ORTHOGONAL-02 | extension | multitrack | ORTHOGONAL | covered | multitrack (constitution skill) was a REAL fixture in every T-P7.01/T-P7.02 conformance run alongside reporting-workable-items — same evidence files. |
| 6-ORTHOGONAL-03 | extension | session-sync | ORTHOGONAL | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=ORTHOGONAL. |
| 6-ORTHOGONAL-04 | extension | media-validator-mcp.json | ORTHOGONAL | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=ORTHOGONAL. |
| 6-ORTHOGONAL-05 | extension | subagent_tiering.yaml | ORTHOGONAL | skip | SKIP: not exercised by HXC-159 Phase P7 (T-P7.01/.02/.04/.05) this cycle; disposition=ORTHOGONAL. |
| 6-BLOCKED-01 | extension | Bridge L3/L4/L5 | BLOCKED | skip | SKIP: blocked per spec.md §7 Bridge-layer disposition. |
