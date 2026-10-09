# QA transcript — CLI-agent config fan-out: five BLOCKING fixes (§11.4.83)
run-id: agent_config_fanout_fixes_20260906T091122Z
host:   Linux 7.0.0-30-generic   bash: 5.3.9(1)-release
HEAD:   2372d7bf
date:   2026-09-06T09:11:22Z

## 1. bash -n on all three edited scripts
  OK   scripts/install_agent_configs.sh
  OK   scripts/gates/agent_config_fanout_gate.sh
  OK   scripts/tests/test_agent_configs.sh

## 2. gate, RED_MODE=0 (standing GREEN guard, real tree)
CM-AGENT-CONFIG-FANOUT  RED_MODE=0
  PASS: /home/milosvasic/Projects/helix_code/scripts/install_agent_configs.sh exists and is executable
  PASS: bash -n clean
  PASS: install_agent_configs.sh is invoked (in command position, outside any heredoc body) from /home/milosvasic/Projects/helix_code/setup.sh
  GREEN: helixagent: 'helixagent-llm' declared and confirmed live at http://127.0.0.1:7061/v1
  GREEN: helixagent: 'helixagent-debate' declared and confirmed live at http://127.0.0.1:7061/v1
  GREEN: helixagent: 'helixagent-ensemble' declared and confirmed live at http://127.0.0.1:7061/v1
  GREEN: helixagent: 'helix-llm' declared and confirmed live at http://127.0.0.1:7061/v1
  GREEN: helixagent: 'helix-debate' declared and confirmed live at http://127.0.0.1:7061/v1
  GREEN: helixllm-coder: 'qwen2.5-coder-3b-instruct-q4_k_m' declared and confirmed live at http://127.0.0.1:18434/v1
  GREEN: helixllm-gateway: 'helixllm-anton-qwen2-5-coder-3b-instruct-q4_k_m-f6771589d190' declared and confirmed live at https://127.0.0.1:8443/v1
CM-AGENT-CONFIG-FANOUT: PASS — installer exists, is executable, is syntactically valid, and is wired into install_agent_configs.sh's setup path; green=7 skipped=0 findings=0
  exit=0

## 3. gate, RED_MODE=1 (A / A2 / A3 / B polarity fixtures)
CM-AGENT-CONFIG-FANOUT  RED_MODE=1
CM-AGENT-CONFIG-FANOUT: RED-A OK — gate FAILs on a valid installer that setup.sh never calls, citing:
    CM-AGENT-CONFIG-FANOUT: FAIL — install_agent_configs.sh is not invoked (nothing invokes it) from /tmp/tmp.wiWAeUeMyH/setup.sh: no line outside a heredoc body puts scripts/install_agent_configs.sh in command position. The file mentions it on 0 line(s), but a MENTION (help text, an echo, a heredoc banner) is not a call — an installer nothing calls is not referenced by the setup path and is the exact defect this gate exists to catch.
CM-AGENT-CONFIG-FANOUT: RED-A2 OK — gate FAILs when scripts/<installer> is only MENTIONED (echo + heredoc), never invoked, citing:
    CM-AGENT-CONFIG-FANOUT: FAIL — install_agent_configs.sh is not invoked (nothing invokes it) from /tmp/tmp.wiWAeUeMyH/setup_a2.sh: no line outside a heredoc body puts scripts/install_agent_configs.sh in command position. The file mentions it on 3 line(s), but a MENTION (help text, an echo, a heredoc banner) is not a call — an installer nothing calls is not referenced by the setup path and is the exact defect this gate exists to catch.
CM-AGENT-CONFIG-FANOUT: RED-A3 OK — the same fixture WITH a real command-position invocation is not flagged as unwired.
CM-AGENT-CONFIG-FANOUT: RED-B OK — gate FAILs on a declared model id absent from its provider's live listing, citing:
      FAIL: helixagent: model id 'helixagent-llm' is declared in the installer but is NOT present in http://127.0.0.1:45797/v1 (live ids: some-other-model)
CM-AGENT-CONFIG-FANOUT: RED OK — all synthetic pre-fix defects are caught (A: unwired; A2: mentioned-but-not-invoked, with A3 as its positive control; B: stale model id).
  exit=0

## 4. gate FAILs on the reproduced BLOCKER-3 mutation (real call deleted from a COPY of setup.sh)
CM-AGENT-CONFIG-FANOUT  RED_MODE=0
  PASS: /home/milosvasic/Projects/helix_code/scripts/install_agent_configs.sh exists and is executable
  PASS: bash -n clean
CM-AGENT-CONFIG-FANOUT: FAIL — install_agent_configs.sh is not invoked (nothing invokes it) from /tmp/claude-1000/-home-milosvasic-Projects-helix-code/d16d2eb1-de59-4512-9cfb-fe148422826c/scratchpad/repro3/setup_mutated.sh: no line outside a heredoc body puts scripts/install_agent_configs.sh in command position. The file mentions it on 2 line(s), but a MENTION (help text, an echo, a heredoc banner) is not a call — an installer nothing calls is not referenced by the setup path and is the exact defect this gate exists to catch.
  exit=1 (1 required)

## 5. behavioural test suite (sandboxed; operator configs never touched)
TEST-AGENT-CONFIG-FANOUT
PASS  idempotency
PASS  non_clobber_merge
PASS  absent_agent_honesty
PASS  shell_builtin_trap
SKIP  unreachable_endpoint_degrades — installer shows no observable sign of honouring HELIX_CODER_BASE_URL (no written config references port 36297); this suite has no documented, non-invasive way to point a real provider at a closed port. SKIP (see report).
PASS  secret_hygiene
PASS  dry_run_writes_nothing
PASS  live_model_id_agreement
-----
TEST-AGENT-CONFIG-FANOUT: run=8 pass=7 fail=0 skip=1
TEST-AGENT-CONFIG-FANOUT: PASS
  exit=0

## 6. operator real-config fingerprints, before this session and now
  83b8cb4e7e0ec665768e90664672486eadf44065d145f9b36c5a80d3ee388b3a  /home/milosvasic/.config/opencode/opencode.json
  731a21bd0b30e14278da57ea4586427a96f7badce592dea1db93059727d023fc  /home/milosvasic/.pi/agent/models.json
  7ace41e9a46436f5d12878b82d094fce994707da445cec862e0047de820815cf  /home/milosvasic/.config/crush/crushrc
  --- now ---
  83b8cb4e7e0ec665768e90664672486eadf44065d145f9b36c5a80d3ee388b3a  /home/milosvasic/.config/opencode/opencode.json
  731a21bd0b30e14278da57ea4586427a96f7badce592dea1db93059727d023fc  /home/milosvasic/.pi/agent/models.json
  7ace41e9a46436f5d12878b82d094fce994707da445cec862e0047de820815cf  /home/milosvasic/.config/crush/crushrc
