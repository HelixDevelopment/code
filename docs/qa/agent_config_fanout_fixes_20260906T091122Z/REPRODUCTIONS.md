# Reproductions — before (pre-fix) vs after (fixed), all five blockers
generated: 2026-09-06T09:14:16Z   HEAD: 2372d7bf

----------------------------------------------------------------------
## BLOCKER 1a — apply_file reports SUCCESS when every write fails
Setup: an unwritable target directory (mode 555 parent), --sandbox under it.

### BEFORE  (exit=0)
      ✓ opencode: updated <SANDBOX>/opencode/opencode.json
      ✓ pi: updated <SANDBOX>/pi/agent/models.json
      ✓ crush: updated <SANDBOX>/crush/crushrc
      ✓ opencode  CONFIGURED     <SANDBOX>/opencode/opencode.json
      ✓ pi        CONFIGURED     <SANDBOX>/pi/agent/models.json
      ✓ crush     CONFIGURED     <SANDBOX>/crush/crushrc
    files actually on disk under the sandbox: 0

### AFTER  (exit=1)
      ✗ opencode: cannot create <SANDBOX>/opencode — nothing written
      ✗ pi: cannot create <SANDBOX>/pi/agent — nothing written
      ✗ crush: cannot create <SANDBOX>/crush — nothing written
      ✗ opencode  ERROR          write to <SANDBOX>/opencode/opencode.json failed (see the ✗ line above)
      ✗ pi        ERROR          write to <SANDBOX>/pi/agent/models.json failed (see the ✗ line above)
      ✗ crush     ERROR          write to <SANDBOX>/crush/crushrc failed (see the ✗ line above)
      ✗ 3 agent(s) reported ERROR — exiting non-zero so the caller sees the failure
    files actually on disk under the sandbox: 0

VERDICT: BEFORE = exit 0 + three CONFIGURED records + 0 files written (CONST-035 false success).
         AFTER  = exit 1 + three ERROR records + 0 files written (honest).

----------------------------------------------------------------------
## BLOCKER 1b — truncated write installed over the operator config
Setup: the REAL apply_file, extracted verbatim from each version, run under
`ulimit -f 4` (2 KiB write ceiling) — the disk-full shape. mv is a rename,
so it needs no free space and happily installs a half-written file.

### BEFORE   (target before: 7019 bytes, begins "OPERATOR-ORIGINAL-")
        backup: <DIR>/target.helix-backup.20260906T091421Z
      ✓ demo: updated <DIR>/target
    apply_file returned: 0
    result: target                                     4096 bytes, begins "XXXXXXXXXXXXXXXXXX"
    result: target.helix-backup.20260906T091421Z       4096 bytes, begins "OPERATOR-ORIGINAL-"

### AFTER   (target before: 7019 bytes, begins "OPERATOR-ORIGINAL-")
      ✗ demo: backup of <DIR>/target failed — refusing to write; the existing config is untouched
    apply_file returned: 12
    result: target                                     7019 bytes, begins "OPERATOR-ORIGINAL-"

VERDICT: BEFORE = printed "✓ demo: updated", returned 0, and left BOTH the
         operator config AND its backup truncated to 4096 bytes — the only
         complete copy was gone.
         AFTER  = refuses at the first failed step, returns 12, target intact
         at 7019 bytes with its original content, no stray tmp/backup file.

----------------------------------------------------------------------
## BLOCKER 2 — redact() missed the secret shapes real configs contain
Setup: --dry-run over a sandbox opencode.json seeded with env-var-shaped
credentials, written COMPACT so the installer's indent=2 re-serialisation
makes the diff cover the whole file (the widening the review predicted).

### BEFORE
    +        "NOTION_API_KEY": "sk-notionAAAAAAAAAAAAAAAAAAAA"
    +        "TAVILY_API_KEY": "sk-tavilyBBBBBBBBBBBBBBBBBBBB"
    +        "AWS_ACCESS_KEY_ID": "AKIAIOSFODNN7EXAMPLE"
    +    "Authorization": "Bearer liveTokenCCCCCCCCCCCCCCCCCCCCCC"
    credential VALUES printed verbatim to stdout: 4 of 4

### AFTER
    +        "NOTION_API_KEY": "***REDACTED***"
    +        "TAVILY_API_KEY": "***REDACTED***"
    +        "AWS_ACCESS_KEY_ID": "***REDACTED***"
    +    "Authorization": "***REDACTED***"
    credential VALUES printed verbatim to stdout: 0 of 4

VERDICT: BEFORE = 4 of 4 credentials printed verbatim. AFTER = 0 of 4.

----------------------------------------------------------------------
## BLOCKER 3 — gate check (c) could not detect deletion of the invocation
Setup: a COPY of the real setup.sh with line 214 (the only real call)
removed, leaving only the two banner-heredoc mentions at 251-252.
    surviving mentions in the mutated copy:
      250:  Re-run / inspect   : ./scripts/install_agent_configs.sh --dry-run
      251:  Prove it works     : ./scripts/install_agent_configs.sh --verify

### BEFORE gate  (exit=0)
      PASS: /home/milosvasic/Projects/helix_code/scripts/install_agent_configs.sh exists and is executable
      PASS: bash -n clean
      PASS: install_agent_configs.sh is invoked from /tmp/claude-1000/-home-milosvasic-Projects-helix-code/d16d2eb1-de59-4512-9cfb-fe148422826c/scratchpad/repro_work/setup_mutated.sh

### AFTER gate  (exit=1)
      PASS: /home/milosvasic/Projects/helix_code/scripts/install_agent_configs.sh exists and is executable
      PASS: bash -n clean
    CM-AGENT-CONFIG-FANOUT: FAIL — install_agent_configs.sh is not invoked (nothing invokes it) from /tmp/claude-1000/-home-milosvasic-Projects-helix-code/d16d2eb1-de59-4512-9cfb-fe148422826c/scratchpad/repro_work/setup_mutated.sh: no line outside a heredoc body puts scripts/install_agent_configs.sh in command position. The file mentions it on 2 line(s), but a MENTION (help text, an echo, a heredoc banner) is not a call — an installer nothing calls is not referenced by the setup path and is the exact defect this gate exists to catch.

VERDICT: BEFORE = PASS (exit 0) — blind to the exact mutation its own FAIL
         message names. AFTER = FAIL (exit 1), citing the mentions explicitly.

----------------------------------------------------------------------
## BLOCKER 4 — the suite misread PI_CODING_AGENT_DIR
Fact check (pi's own --help on this host):
      PI_CODING_AGENT_DIR              - Config directory (default: ~/.pi/agent)
  => it is the AGENT dir; the file is $PI_CODING_AGENT_DIR/models.json.
  Both suites are run against the SAME (fixed) installer, so the only
  variable is the suite itself.

### BEFORE suite
    PASS  idempotency
    FAIL  non_clobber_merge
          pi models.json: no Helix provider content was added
    PASS  absent_agent_honesty
    PASS  shell_builtin_trap
    SKIP  unreachable_endpoint_degrades — installer shows no observable sign of honouring HELIX_CODER_BASE_URL (no written config references port 55007); this suite has no documented, non-invasive way to point a real provider at a closed port. SKIP (see report).
    PASS  secret_hygiene
    PASS  dry_run_writes_nothing
    PASS  live_model_id_agreement
    TEST-AGENT-CONFIG-FANOUT: run=8 pass=6 fail=1 skip=1
    TEST-AGENT-CONFIG-FANOUT: FAIL — non_clobber_merge

### AFTER suite
    PASS  idempotency
    PASS  non_clobber_merge
    PASS  absent_agent_honesty
    PASS  shell_builtin_trap
    SKIP  unreachable_endpoint_degrades — installer shows no observable sign of honouring HELIX_CODER_BASE_URL (no written config references port 41983); this suite has no documented, non-invasive way to point a real provider at a closed port. SKIP (see report).
    PASS  secret_hygiene
    PASS  dry_run_writes_nothing
    PASS  live_model_id_agreement
    TEST-AGENT-CONFIG-FANOUT: run=8 pass=7 fail=0 skip=1
    TEST-AGENT-CONFIG-FANOUT: PASS

VERDICT: BEFORE = non_clobber_merge (the suite's self-declared most
         important test) FAILs with "pi models.json: no Helix provider
         content was added" — a fixture bug, not an installer defect, so
         pi's non-clobber contract was never actually exercised.
         AFTER = it PASSes and the contract is genuinely checked.

----------------------------------------------------------------------
## BLOCKER 5 — the sandbox was escapable via XDG_CONFIG_HOME
Setup: a decoy directory standing in for the operator's real ~/.config,
exported as XDG_CONFIG_HOME, with HOME/PI_CODING_AGENT_DIR sandboxed
exactly as each version of sandbox_env_setup does.

### BEFORE
    decoy opencode.json changed: YES
    Helix provider references now in the decoy: 8
    files in the decoy tree: opencode.json opencode.json.helix-backup.20260906T091517Z 

### AFTER
    decoy opencode.json changed: NO
    Helix provider references now in the decoy: 0
    files in the decoy tree: opencode.json 

VERDICT: BEFORE = the write landed in XDG_CONFIG_HOME — the decoy standing
         in for the operator's real config was rewritten and a
         .helix-backup.* was created beside it. (The merge preserved the
         pre-existing key, so nothing was lost — but the sandbox did not
         hold, which is what it promises.) XDG_CONFIG_HOME is unset on
         this host, so no real config was ever reached; that was luck.
         AFTER = the decoy is untouched.

----------------------------------------------------------------------
END OF REPRODUCTIONS
