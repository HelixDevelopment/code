#!/usr/bin/env python3
"""T-P2.01.4 re-runnability proof: per-task verdicts identical across reps."""
import glob
import json
import os
import sys

out = sys.argv[1]
runs = {}
for fn in glob.glob(os.path.join(out, "run_*.json")):
    with open(fn, encoding="utf-8") as fh:
        d = json.load(fh)
    runs.setdefault(d["size"], {})[d["repetition"]] = {
        r["task_id"]: (r["verdict"], r["failure_mode"], r["selected"])
        for r in d["results"]}
report = {"sizes": {}}
identical = True
for size, reps in runs.items():
    keys = sorted(reps)
    base = reps[keys[0]]
    same = all(reps[k] == base for k in keys[1:])
    report["sizes"][size] = {"repetitions": keys,
                             "identical_verdicts": same}
    identical = identical and same
report["identical_all"] = identical
with open(os.path.join(out, "rerun_report.json"), "w",
          encoding="utf-8") as fh:
    json.dump(report, fh, indent=2)
print(json.dumps(report, indent=2))
sys.exit(0 if identical else 1)
