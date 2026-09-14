#!/usr/bin/env bash
# T-P2.02 curve harness — self-driving, re-runnable (§11.4.98: no human action
# after start). Runs the fixed task set at {small, mid, large} x REPETITIONS,
# aggregates per-size means into curve.json + curve.csv + curve.svg (plot data).
#
# Usage: run_curve.sh <run-id> [repetitions]   (default repetitions=3)
# Artefacts: qa-results/hxc159/shadowing_curve/<run-id>/
set -u
RUN_ID="${1:?usage: run_curve.sh <run-id> [repetitions]}"
REPS="${2:-3}"
HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="qa-results/hxc159/shadowing_curve/${RUN_ID}"
mkdir -p "$OUT"
cp "$HERE/taskset.json" "$HERE/skill_corpus.json" "$HERE/distractors.json" "$OUT/"

for size in small mid large; do
  for ((r = 1; r <= REPS; r++)); do
    python3 "$HERE/run_bench.py" --size "$size" --repetition "$r" --out "$OUT" \
      || echo "RUNNER-FAILED size=$size rep=$r (recorded, continuing)"
  done
done

python3 - "$OUT" <<'EOF'
import json, os, sys
out = sys.argv[1]
runs = {}
for fn in sorted(os.listdir(out)):
    if not (fn.startswith("run_") and fn.endswith(".json")):
        continue
    with open(os.path.join(out, fn)) as fh:
        d = json.load(fh)
    runs.setdefault(d["size"], []).append(d)
sizes = []
for size in ("small", "mid", "large"):
    reps = runs.get(size, [])
    assert reps, "no runs for size %s" % size
    n = reps[0]["n_active"]
    rates = [r["pass_rate"] for r in reps]
    modes = {}
    for r in reps:
        for k, v in r["failures_by_mode"].items():
            modes[k] = modes.get(k, 0) + v
    # mean counts per repetition for readability
    mean_modes = {k: round(v / len(reps), 3) for k, v in modes.items()}
    sizes.append({"size": size, "n": n, "n_tasks": reps[0]["n_tasks"],
                  "repetitions": len(reps),
                  "pass_rate": round(sum(rates) / len(rates), 4),
                  "pass_rate_per_rep": [round(x, 4) for x in rates],
                  "failures_by_mode": mean_modes})
curve = {"run_id": os.path.basename(out), "sizes": sizes}
with open(os.path.join(out, "curve.json"), "w") as fh:
    json.dump(curve, fh, indent=2)
with open(os.path.join(out, "curve.csv"), "w") as fh:
    fh.write("size,n_active,pass_rate,abandonment,mis-selection,model-error\n")
    for s in sizes:
        m = s["failures_by_mode"]
        fh.write("%s,%d,%.4f,%s,%s,%s\n" % (
            s["size"], s["n"], s["pass_rate"],
            m.get("abandonment", 0), m.get("mis-selection", 0),
            m.get("model-error", 0)))
# minimal dependency-free SVG line plot (plot data, not decoration)
W, H, P = 520, 320, 48
ns = [s["n"] for s in sizes]
n0, n1 = min(ns), max(ns)
xs = [P + (W - 2 * P) * ((n - n0) / (n1 - n0) if n1 > n0 else 0.5) for n in ns]
ys = [H - P - (H - 2 * P) * s["pass_rate"] for s in sizes]
pts = " ".join("%d,%d" % (x, y) for x, y in zip(xs, ys))
dots = "".join(
    '<circle cx="%d" cy="%d" r="5" fill="#c0392b"/>'
    '<text x="%d" y="%d" font-size="12">%s N=%d %.3f</text>'
    % (x, y, x + 8, y - 8, s["size"], s["n"], s["pass_rate"])
    for x, y, s in zip(xs, ys, sizes))
svg = ('<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">'
       '<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#888"/>'
       '<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#888"/>'
       '<polyline points="%s" fill="none" stroke="#c0392b" stroke-width="2"/>'
       '%s<text x="%d" y="%d" font-size="13">pass_rate vs active N</text></svg>'
       % (W, H, P, H - P, W - P, H - P, P, P, P, H - P, pts, dots, P, 24))
with open(os.path.join(out, "curve.svg"), "w") as fh:
    fh.write(svg)
print(json.dumps(curve, indent=2))
EOF
echo "CURVE-DONE -> $OUT/curve.json + curve.csv + curve.svg"
