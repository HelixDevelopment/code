#!/usr/bin/env bash
# Gate CM-ACTIVE-SKILL-CEILING (T-P2.05) — enforces the MEASURED ceiling (FR-016)
# and the calibrated description-similarity threshold (FR-017) at load time.
#
# Usage: cm_active_skill_ceiling_gate.sh --skills-json FILE [--ceiling-json FILE]
#        [--threshold-json FILE]
#   FILE: JSON array of {name, description} = the proposed ACTIVE set.
# Exits 0 on PASS, non-zero naming the offender on FAIL.
set -u
SKILLS_JSON=""
CEILING_JSON="scripts/benchmark_skills/ceiling.json"
THRESHOLD_JSON="scripts/benchmark_skills/threshold.json"
while [ $# -gt 0 ]; do
  case "$1" in
    --skills-json) SKILLS_JSON="$2"; shift 2;;
    --ceiling-json) CEILING_JSON="$2"; shift 2;;
    --threshold-json) THRESHOLD_JSON="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done
[ -n "$SKILLS_JSON" ] || { echo "usage: $0 --skills-json FILE" >&2; exit 2; }
[ -f "$CEILING_JSON" ] || { echo "GATE-ERROR: missing $CEILING_JSON (run calibration first)" >&2; exit 2; }
[ -f "$THRESHOLD_JSON" ] || { echo "GATE-ERROR: missing $THRESHOLD_JSON (run calibration first)" >&2; exit 2; }

python3 - "$SKILLS_JSON" "$CEILING_JSON" "$THRESHOLD_JSON" <<'EOF'
import json, re, sys
sj, cj, tj = sys.argv[1], sys.argv[2], sys.argv[3]
with open(sj, encoding="utf-8") as fh: skills = json.load(fh)
with open(cj, encoding="utf-8") as fh: ceiling = json.load(fh)["ceiling"]
with open(tj, encoding="utf-8") as fh: threshold = json.load(fh)["threshold"]
fails = []
if len(skills) > ceiling:
    offender = skills[ceiling]["name"]
    fails.append("CEILING-EXCEEDED: active=%d ceiling=%d offender=%s"
                 % (len(skills), ceiling, offender))
STOP = set("""a an the and or of to in on for with as by at from is are was
were be been it its this that these those you your we our they their he she him
her them his hers ours theirs my mine use using used when whenever where which
who whom whose what how why not no nor if then than so such can could should
would may might must will shall do does did done have has had having any all
each every both either neither one two more most other some over under into
out about above below between through during before after per via also only
just very too either vs etc including include includes within without name
skill system""".split())
def toks(t):
    return {w for w in re.findall(r"[a-z0-9]+", t.lower())
            if len(w) > 1 and w not in STOP}
def jac(a, b):
    ta, tb = toks(a), toks(b)
    if not ta and not tb: return 1.0
    if not ta or not tb: return 0.0
    return len(ta & tb) / len(ta | tb)
for i in range(len(skills)):
    for j in range(i + 1, len(skills)):
        s = jac(skills[i]["description"], skills[j]["description"])
        if s > threshold:
            fails.append("SIMILARITY-EXCEEDED: %.4f > %.4f between '%s' and '%s'"
                         % (s, threshold, skills[i]["name"], skills[j]["name"]))
if fails:
    print("CM-ACTIVE-SKILL-CEILING: FAIL")
    for f in fails: print("  " + f)
    sys.exit(1)
print("CM-ACTIVE-SKILL-CEILING: PASS (active=%d ceiling=%d threshold=%.4f)"
      % (len(skills), ceiling, threshold))
EOF
