#!/usr/bin/env bash
# Re-runnable performance test wiring the shadowing curve (T-P2.05.3).
# Self-driving: no human action after start (§11.4.98). Asserts the harness ran
# and the artefact is well-formed (>=3 sizes, pass_rate per size). It does NOT
# assert a high pass rate — that would be a bluff; quality verdicts live in
# derivation.md, produced from measurement.
set -u
OUT="qa-results/hxc159/shadowing_curve/perf_$(date -u +%Y%m%dT%H%M%SZ)"
bash scripts/benchmark_skills/run_curve.sh "$(basename "$OUT")" 1
python3 - "$OUT" <<'EOF'
import json, os, sys
out = sys.argv[1]
with open(os.path.join(out, "curve.json")) as fh:
    curve = json.load(fh)
assert len(curve["sizes"]) >= 3, "fewer than 3 sampled sizes"
for s in curve["sizes"]:
    assert 0.0 <= s["pass_rate"] <= 1.0, s
    assert s["n_tasks"] >= 12, s
print("PERF-TEST PASS: curve well-formed at %s" % out)
EOF
