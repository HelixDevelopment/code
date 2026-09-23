# HXC-159 T-P9.02 final-retest harness

`run_p9_02_retest.sh` is the reusable orchestrator for T-P9.02 ("Full-suite
retest on a clean target") from
`specs/001-helixskills-incorporation/tasks.md` — §11.4.40 full retest from
the last tag, §11.4.108/§11.4.139 runtime-on-clean-target, §11.4.132
risk-ordered (most-reopened first), §11.4.200 (isolate exactly one target).

## What it does

Given `--target <path-to-an-isolated-clean-checkout>` (main repo +
`constitution`, `submodules/helix_agent`, `submodules/skills` submodules at
whatever commits that checkout has), it:

1. Refuses to run if the target is not genuinely clean (`git status
   --porcelain` non-empty, or a required submodule uninitialized/dirty).
2. Runs `scripts/register_skills.sh --root <target>` from scratch.
3. Re-observes each T-P9.01 runtime signature (RS-01 .. RS-22) as its own
   `rs_NN_*` function, rebuilding any Go binary it needs *immediately*
   before invoking it (see the "artifact-freshness policy" comment at the
   top of the script for why "always rebuild" was chosen over
   timestamp/hash bookkeeping).
4. Writes one PASS/FAIL/SKIP verdict per RS, with raw command output, to
   `<target>/qa-results/hxc159/final_retest/<run-id>/{report.json,report.md}`.
5. Exits 0 iff every requested RS is PASS or an honestly-reasoned SKIP;
   non-zero on any FAIL or pre-flight refusal.

It does **not** itself close T-P9.02 — per T-P9.02.4, the automated result
still hands off to manual QA (§11.4.185); this script produces evidence, a
human closes the task.

## Invocation

```bash
# Real run, once a clean checkout exists elsewhere on disk:
bash scripts/final_retest/run_p9_02_retest.sh --target /path/to/clean/clone

# Re-run only the highest-risk (F-17-touched) checks:
bash scripts/final_retest/run_p9_02_retest.sh --target /path/to/clean/clone \
    --only RS-01,RS-05,RS-06,RS-08,RS-17

# Skip the registrar step (target's .claude/skills/ already provisioned):
bash scripts/final_retest/run_p9_02_retest.sh --target /path/to/clean/clone \
    --skip-registrar
```

`--target` **must not** be the checkout this script itself lives in — the
script refuses (`hxc159_guard_not_self`) if `--target` resolves to the same
git toplevel it was invoked from, precisely because this harness exists to
run against a separate, disposable clean clone, never the live shared
development checkout.

## Risk ordering (§11.4.132)

T-P9.01's most-reopened/most-regressed finding, **F-17**, touched RS-01,
RS-05, RS-06, RS-08 and RS-17 across review rounds (see the finding ledger
in `specs/001-helixskills-incorporation/tasks.md`). Those five run first;
every other RS follows in ascending numeric order.

## Honest scope note

RS-07 (the P2-phase shadowing-curve measurement exercise) has no dedicated
`rs_07_*` function — it is a one-shot curve-sampling artefact from an
earlier phase, not a re-runnable pass/fail runtime signature, and re-running
it is out of scope for this retest harness.

RS-19 (bridge install) is located but deliberately **not** auto-invoked: the
install script it points at can perform network/submodule-registration side
effects against the target and unconditionally refuses one of its layers by
supply-chain policy; auto-running an installer from inside a retest
orchestrator is out of scope here. It is reported SKIP with the located
script's path so an operator can invoke it explicitly and read its own
`install_report.json`.

Every other RS check runs a real command (a `go build` + binary invocation,
a `go test -run`, or an existing project script) against the target only —
none of them fabricate a PASS from the mere absence of an error.
