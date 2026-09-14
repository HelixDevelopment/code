#!/usr/bin/env python3
"""T-P2.03 derive the active-count ceiling FROM the measured curve.

STATED REPRODUCIBLE RULE (not by eye):
  base  = pass_rate(small)
  knee  = smallest N with pass_rate(N) <= base - 0.10 (10-point absolute drop)
          OR mis-selection count at N >= ceil(0.25 * n_tasks)
  ceiling = the largest sampled N strictly below the knee
            (previous sampled size; if knee == small, ceiling = small N - 1)
  margin: one full sampled step below the knee, reason: sizes are coarse
          (12/16/20) so the true knee lies somewhere inside the step; the
          previous sampled size is the largest MEASURED-safe point.
  flat  = (max(pass_rate) - min(pass_rate)) < 0.05 -> verdict FLAT, reported
          honestly; ceiling = largest tested N, flagged provisional.

Reads curve.json, writes derivation.md + ceiling.json {ceiling, ...}.
"""
import argparse
import json
import os
import sys

DROP = 0.10
FLAT_SPREAD = 0.05
MISSEL_FRAC = 0.25


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--curve", required=True)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    with open(args.curve, encoding="utf-8") as fh:
        curve = json.load(fh)
    sizes = curve["sizes"]  # [{size, n, pass_rate, failures_by_mode, n_tasks}]
    base = next(s for s in sizes if s["size"] == "small")["pass_rate"]
    n_tasks = sizes[0]["n_tasks"]
    knee = None
    knee_why = ""
    for s in sizes:
        missel = s["failures_by_mode"].get("mis-selection", 0)
        if s["pass_rate"] <= base - DROP:
            knee, knee_why = s, \
                "pass_rate %.3f <= base %.3f - %.2f" % (
                    s["pass_rate"], base, DROP)
            break
        if missel >= MISSEL_FRAC * n_tasks:
            knee, knee_why = s, \
                "mis-selections %d >= %.0f%% of %d tasks" % (
                    missel, MISSEL_FRAC * 100, n_tasks)
            break
    spread = max(s["pass_rate"] for s in sizes) - \
        min(s["pass_rate"] for s in sizes)
    os.makedirs(args.out, exist_ok=True)
    if spread < FLAT_SPREAD and knee is None:
        verdict = "FLAT"
        ceiling = max(s["n"] for s in sizes)
        ceiling_src = ("no knee observed (spread %.3f < %.2f); ceiling is "
                       "provisional at the largest tested N=%d — an honest "
                       "finding per T-P2.03.3, not a buried one."
                       % (spread, FLAT_SPREAD, ceiling))
    elif knee is None:
        verdict = "NO-KNEE-IN-RANGE"
        ceiling = max(s["n"] for s in sizes)
        ceiling_src = ("no knee inside the sampled range but spread %.3f "
                       ">= %.2f; ceiling provisional at tested max N=%d, "
                       "larger N untested." % (spread, FLAT_SPREAD, ceiling))
    else:
        verdict = "KNEE"
        below = [s for s in sizes if s["n"] < knee["n"]]
        if below:
            ceiling = max(s["n"] for s in below)
            ceiling_src = ("knee at N=%d (%s); ceiling = largest sampled N "
                           "below knee = %d (one full step margin: true knee "
                           "lies inside the step)."
                           % (knee["n"], knee_why, ceiling))
        else:
            ceiling = knee["n"] - 1
            ceiling_src = ("knee already at smallest size N=%d (%s); "
                           "ceiling = %d." % (knee["n"], knee_why, ceiling))
    result = {"rule": "knee=smallest N with pass_rate<=base-0.10 OR mis-selections>=25% of tasks; ceiling=largest sampled N below knee; flat iff spread<0.05",
              "base_pass_rate": round(base, 4),
              "verdict": verdict, "ceiling": ceiling,
              "knee": knee["size"] if knee else None,
              "knee_why": knee_why or None,
              "margin": ceiling_src}
    with open(os.path.join(args.out, "ceiling.json"), "w",
              encoding="utf-8") as fh:
        json.dump(result, fh, indent=2)
    md = ["# T-P2.03 ceiling derivation (measured, not eyed)",
          "",
          "Rule: %s" % result["rule"],
          "",
          "Base (small) pass_rate: %.4f" % base,
          "Per-size: " + "; ".join(
              "%s N=%d rate=%.3f modes=%s" % (
                  s["size"], s["n"], s["pass_rate"],
                  json.dumps(s["failures_by_mode"], sort_keys=True))
              for s in sizes),
          "",
          "Verdict: **%s**" % verdict,
          "Ceiling: **%d**" % ceiling,
          "",
          ceiling_src,
          ""]
    if verdict == "FLAT":
        md.append("Flat curve reported honestly per T-P2.03.3: shadowing not "
                  "observed on this corpus/model in N∈{%s}."
                  % ",".join(str(s["n"]) for s in sizes))
    with open(os.path.join(args.out, "derivation.md"), "w",
              encoding="utf-8") as fh:
        fh.write("\n".join(md))
    print(json.dumps(result, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
