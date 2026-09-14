#!/usr/bin/env bash
# Paired §1.1 mutations for CM-ACTIVE-SKILL-CEILING + similarity self-validation.
# Each mutation MUST flip the gate to FAIL; restoring MUST return PASS.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
GATE="$ROOT/scripts/gates/cm_active_skill_ceiling_gate.sh"
BENCH="$ROOT/scripts/benchmark_skills"
PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); echo "  MUT-OK: $1"; }
bad() { FAIL=$((FAIL+1)); echo "  MUT-FAIL: $1"; }

echo "[1] similarity analyzer self-validation (golden-good/golden-bad)"
if python3 "$BENCH/similarity.py" --self-test; then ok "self-test green"; else bad "self-test red"; fi

echo "[2] ceiling+1 active set MUST FAIL the gate"
python3 - "$BENCH/skill_corpus.json" "$BENCH/distractors.json" <<'EOF' > /tmp/p2_mut_big.json
import json, sys
corpus = json.load(open(sys.argv[1]))
d = json.load(open(sys.argv[2]))
ceil = json.load(open("scripts/benchmark_skills/ceiling.json"))["ceiling"]
skills = [{"name": s["name"], "description": s["description"]} for s in corpus["skills"]]
for x in d["distractors"]:
    skills.append({"name": x["name"], "description": x["description"]})
k = 0
while len(skills) < ceil + 1:  # flat-curve case: ceiling may exceed corpus; pad with synthetic overflow entries (count-check test only)
    k += 1
    skills.append({"name": "synthetic-overflow-%d" % k, "description": "synthetic overflow probe skill for ceiling enforcement testing"})
json.dump(skills[:ceil + 1], open("/tmp/p2_mut_big.json", "w"), indent=2)
EOF
if "$GATE" --skills-json /tmp/p2_mut_big.json >/tmp/p2_mut_big.out 2>&1; then
  bad "ceiling+1 passed the gate"
else
  grep -q "CEILING-EXCEEDED" /tmp/p2_mut_big.out && ok "ceiling+1 refused ($(head -2 /tmp/p2_mut_big.out | tail -1))" \
    || bad "wrong refusal reason"
fi

echo "[3] gate outcome MUST equal the calibrated verdict on the confusable pair"
python3 - <<'EOF' > /tmp/p2_mut_pair.json
import json
corpus = json.load(open("scripts/benchmark_skills/skill_corpus.json"))
want = {"reporting-workable-items", "workable-item-lifecycle"}
json.dump([{"name": s["name"], "description": s["description"]} for s in corpus["skills"] if s["id"] in want],
          open("/tmp/p2_mut_pair.json", "w"), indent=2)
EOF
PAIR_EXPECT="$(python3 - <<'EOF'
import json, re
corpus = json.load(open("scripts/benchmark_skills/skill_corpus.json"))
thr = json.load(open("scripts/benchmark_skills/threshold.json"))["threshold"]
desc = {s["id"]: s["description"] for s in corpus["skills"]}
STOP = set("a an the and or of to in on for with as by at from is are was were be been it its this that these those you your we our they their he she him her them his hers ours theirs my mine use using used when whenever where which who whom whose what how why not no nor if then than so such can could should would may might must will shall do does did done have has had having any all each every both either neither one two more most other some over under into out about above below between through during before after per via also only just very too either vs etc including include includes within without name skill system".split())
def toks(t):
    return {w for w in re.findall(r"[a-z0-9]+", t.lower()) if len(w) > 1 and w not in STOP}
def jac(a, b):
    ta, tb = toks(a), toks(b)
    return len(ta & tb) / len(ta | tb)
s = jac(desc["reporting-workable-items"], desc["workable-item-lifecycle"])
print(("FAIL" if s > thr else "PASS") + " score=%.4f thr=%.4f" % (s, thr))
EOF
)"
echo "  calibrated expectation: gate should $PAIR_EXPECT"
if "$GATE" --skills-json /tmp/p2_mut_pair.json >/tmp/p2_mut_pair.out 2>&1; then
  ACT="PASS"; else ACT="FAIL"
fi
if [ "$ACT" = "$(echo "$PAIR_EXPECT" | awk '{print $1}')" ] && { [ "$ACT" = "PASS" ] || grep -q "SIMILARITY-EXCEEDED" /tmp/p2_mut_pair.out; }; then
  ok "gate $ACT matches calibration ($PAIR_EXPECT)"
else
  bad "gate $ACT contradicts calibration ($PAIR_EXPECT)"
fi

echo "[4] gate on a hard-distinct set MUST match calibration AND any refusal must be covered by disclosed FP risk"
python3 - <<'EOF' > /tmp/p2_mut_small.json
import json
corpus = json.load(open("scripts/benchmark_skills/skill_corpus.json"))
keep = {"media-validator", "action-prefix-system", "skill-catalog", "session-sync"}
json.dump([{"name": s["name"], "description": s["description"]} for s in corpus["skills"] if s["id"] in keep],
          open("/tmp/p2_mut_small.json", "w"), indent=2)
EOF
if "$GATE" --skills-json /tmp/p2_mut_small.json >/tmp/p2_mut_small.out 2>&1; then
  ok "distinct small set passes"
else
  # A refusal here is a false positive by construction (the 4 skills are
  # distinct domains). It is acceptable ONLY if calibration honestly declares
  # fp_risk — a refusal with separable=true would be a hidden-RISK bluff.
  FPRISK="$(python3 -c "import json;print(json.load(open('scripts/benchmark_skills/threshold.json'))['fp_risk_accepted'])")"
  SEP="$(python3 -c "import json;print(json.load(open('scripts/benchmark_skills/threshold.json'))['separable'])")"
  if [ "$FPRISK" = "True" ] && [ "$SEP" = "False" ]; then
    ok "refusal covered by honestly-declared NON-SEPARABLE calibration (fp_risk_accepted=true): $(grep -c SIMILARITY-EXCEEDED /tmp/p2_mut_small.out) known-FP pair(s)"
  else
    bad "refusal NOT covered by calibration disclosure (separable=$SEP fp_risk=$FPRISK): $(cat /tmp/p2_mut_small.out)"
  fi
fi

echo "META-TEST: pass=$PASS fail=$FAIL"
[ "$FAIL" -eq 0 ]
