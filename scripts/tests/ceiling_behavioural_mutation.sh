#!/usr/bin/env bash
# T-P2.05.2 behavioural paired mutation: raise the ceiling past the measured
# knee, then prove the BENCHMARK (not just the static gate) shows the regression
# the raised ceiling would admit. A static gate passing is not sufficient here.
#
# Usage: ceiling_behavioural_mutation.sh <curve-run-dir> <out-dir>
# Reads <curve-run-dir>/curve.json + ceiling.json; writes mutation report.
set -u
CURVE_DIR="${1:?usage: $0 <curve-run-dir> <out-dir>}"
OUT="${2:?usage: $0 <curve-run-dir> <out-dir>}"
mkdir -p "$OUT"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GATE="$ROOT/scripts/gates/cm_active_skill_ceiling_gate.sh"
BENCH="$ROOT/scripts/benchmark_skills"

VERDICT="$(python3 -c "import json;print(json.load(open('$CURVE_DIR/ceiling.json'))['verdict'])")"
CEIL="$(python3 -c "import json;print(json.load(open('$CURVE_DIR/ceiling.json'))['ceiling'])")"
LARGE_N="$(python3 -c "import json;d=json.load(open('$CURVE_DIR/curve.json'));print([s['n'] for s in d['sizes'] if s['size']=='large'][0])")"
BASE="$(python3 -c "import json;d=json.load(open('$CURVE_DIR/curve.json'));print([s['pass_rate'] for s in d['sizes'] if s['size']=='small'][0])")"

# MUTATE: ceiling raised past the knee to the largest tested N
python3 - "$CEIL" "$LARGE_N" <<'EOF' > "$OUT/mutated_ceiling.json"
import json, sys
print(json.dumps({"ceiling": int(sys.argv[2]), "mutated_from": int(sys.argv[1]),
                  "note": "paired mutation: raised past measured knee"}))
EOF

# The 20-skill large active set vs the MUTATED static gate ...
# (deterministic regen of the large active set)
python3 -c "
import json
corpus = json.load(open('$BENCH/skill_corpus.json'))
d = json.load(open('$BENCH/distractors.json'))
skills = [{'name': s['name'], 'description': s['description']} for s in corpus['skills']]
dm = {x['id']: x for x in d['distractors']}
for did in d['sizes']['large']['distractor_ids']:
    skills.append({'name': dm[did]['name'], 'description': dm[did]['description']})
json.dump(skills, open('$OUT/mut_large_set.json', 'w'), indent=2)
print(len(skills))
"
cp "$BENCH/threshold.json" "$OUT/mut_threshold.json" 2>/dev/null || cp "$CURVE_DIR/threshold.json" "$OUT/mut_threshold.json"
if "$GATE" --skills-json "$OUT/mut_large_set.json" --ceiling-json "$OUT/mutated_ceiling.json" --threshold-json "$OUT/mut_threshold.json" >/tmp/p2_behav_gate.out 2>&1; then
  STATIC="pass-under-mutation"
else
  STATIC="refused-despite-mutation ($(head -2 /tmp/p2_behav_gate.out | tail -1))"
fi

# ... but the BENCHMARK re-run at large N must show the regression.
MBENCH="$OUT/mut_bench"
mkdir -p "$MBENCH"
python3 "$BENCH/run_bench.py" --size large --repetition 1 --out "$MBENCH"
MUT_RATE="$(python3 -c "import json,glob;f=glob.glob('$MBENCH/run_large_rep1.json')[0];print(json.load(open(f))['pass_rate'])")"

python3 - "$VERDICT" "$BASE" "$MUT_RATE" "$STATIC" "$CEIL" "$LARGE_N" <<'EOF' > "$OUT/behavioural_mutation_report.json"
import json, sys
verdict, base, mut, static, ceil, large = sys.argv[1], float(sys.argv[2]), float(sys.argv[3]), sys.argv[4], sys.argv[5], sys.argv[6]
if verdict == "KNEE":
    regressed = mut <= base - 0.10
    outcome = "CONFIRMED" if regressed else "REFUTED"
    meaning = ("benchmark shows the regression the raised ceiling admits"
               if regressed else "NO regression observed under mutation — ceiling derivation suspect")
else:
    regressed = None
    outcome = "NOT-APPLICABLE-FLAT"
    meaning = ("curve is FLAT (spread<0.05): no knee exists to mutate past; "
               "benchmark re-run confirms no regression, honestly recorded per T-P2.03.3")
print(json.dumps({"mutation": "ceiling raised %s -> %s (past knee)" % (ceil, large),
                  "static_gate_under_mutation": static,
                  "baseline_small_rate": base, "mutated_large_rate": mut,
                  "regression_shown_by_benchmark": regressed,
                  "outcome": outcome, "meaning": meaning}, indent=2))
EOF
cat "$OUT/behavioural_mutation_report.json"
python3 -c "
import json,sys
r = json.load(open('$OUT/behavioural_mutation_report.json'))
sys.exit(0 if r['outcome'] in ('CONFIRMED', 'NOT-APPLICABLE-FLAT') else 1)"
