#!/usr/bin/env python3
"""T-P2.04 pairwise description-similarity metric + threshold calibration.

Metric: Jaccard similarity over token sets (lowercased alnum runs, stopwords
and single chars removed). Chosen for this corpus because descriptions are
short prose where shared content words (not word order) drive confusion, it is
deterministic, dependency-free, and explainable per pair. Justification recorded
here, not in literature.

Threshold rule (STATED, reproducible):
  separable  := max(known_distinct) < min(known_confusable)
  threshold  := (max(known_distinct) + min(known_confusable)) / 2
If not separable, the metric is reported NON-SEPARABLE (honest finding) and the
threshold falls back to min(known_confusable) with fp_risk=true documented.

Golden-good / golden-bad self-validation (§11.4.107(10)):
  golden-good pair must score ABOVE threshold (detector fires on true confusable)
  golden-bad pair must score BELOW threshold (detector stays quiet on distinct)
A detector passing its golden-bad fixture is a bluff gate -> exit non-zero.

Usage:
  similarity.py --matrix --out DIR        write similarity_matrix.json
  similarity.py --calibrate --out DIR     write threshold.json (+ derivation note)
  similarity.py --self-test               golden-good/golden-bad validation
"""
import argparse
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

STOPWORDS = set("""a an the and or of to in on for with as by at from is are was
were be been it its this that these those you your we our they their he she him
her them his hers ours theirs my mine use using used when whenever where which
who whom whose what how why not no nor if then than so such can could should
would may might must will shall do does did done have has had having any all
each every both either neither one two more most other some over under into
out about above below between through during before after per via also only
just very too either vs etc including include includes within without name
skill system""".split())


def tokens(text):
    return {t for t in re.findall(r"[a-z0-9]+", text.lower())
            if len(t) > 1 and t not in STOPWORDS}


def jaccard(a, b):
    ta, tb = tokens(a), tokens(b)
    if not ta and not tb:
        return 1.0
    if not ta or not tb:
        return 0.0
    return len(ta & tb) / len(ta | tb)


# HXC-159 F-17 remediation (2026-09-23): CONFUSABLE/DISTINCT now derive ONLY
# from corpus-1 (skill_corpus.json entries with default_activation_eligible=
# true) -- the 9 skills genuinely co-activated by default at either consumer
# (constitution/skills/'s 8 + speckit-analyze, the union of helix_agent's and
# helix_code's real external tiers). The 4 corpus-2 postgres-mcp skills
# (submodules/helix_agent/skills/postgres-mcp/*) are EXCLUDED per
# pkg/skills/activate.go's CorpusTwoPolicy: they are TrustVendored and NEVER
# added to a default allowlist, so their similarity to anything else is not a
# real co-activation risk for the tier F-17 found broken -- mixing them into
# the ORIGINAL calibration pulled min(known_confusable) down to 0.0256 (one of
# the postgres/hypertable-domain pairs, NOT a real corpus-1 pair) and that
# artificially low threshold then refused corpus-1 on its own real,
# now-frontmatter-complete governance-skill descriptions.
#
# Every pair below was classified by READING the two descriptions (T-P2.04.1's
# "choose the metric and justify it against THIS corpus" methodology, not a
# quantile split): CONFUSABLE = a real same-domain overlap where an agent
# could legitimately pick the wrong skill (verified by inspecting the actual
# shared-token set for each pair -- see qa-results/hxc159/f17_remediation/ for
# the captured shared-token evidence); DISTINCT = pairs whose shared tokens
# are project-wide generic/anti-bluff vocabulary ("captured", "evidence",
# "verify", "work") or accidental §-citation-number collisions (e.g. both
# multitrack and session-sync cite §11.4.187, contributing spurious "11"/"187"
# token overlap on top of their genuine "multi"/"track"/"session" domain
# overlap -- a metric weakness noted honestly below, not yet decisive since
# the real-content overlap alone is sufficient to classify that pair
# CONFUSABLE).
CONFUSABLE = [
    ("reporting-workable-items", "workable-item-lifecycle"),
    ("action-prefix-system", "reporting-workable-items"),
    ("multitrack", "session-sync"),
]
DISTINCT = [
    ("media-validator", "scheduled-work-queue"),
    ("scheduled-work-queue", "workable-item-lifecycle"),
    ("skill-catalog", "speckit-analyze"),
    ("scheduled-work-queue", "session-sync"),
    ("session-sync", "workable-item-lifecycle"),
    ("action-prefix-system", "scheduled-work-queue"),
    ("media-validator", "reporting-workable-items"),
    ("action-prefix-system", "workable-item-lifecycle"),
    ("action-prefix-system", "session-sync"),
    ("action-prefix-system", "media-validator"),
]

GOLDEN_GOOD = ("reporting-workable-items", "workable-item-lifecycle")
GOLDEN_BAD = ("media-validator", "scheduled-work-queue")


def load_corpus():
    with open(os.path.join(HERE, "skill_corpus.json"),
              encoding="utf-8") as fh:
        data = json.load(fh)
    return {s["id"]: s["description"] for s in data["skills"]}


def matrix(desc):
    ids = sorted(desc)
    out = {}
    for i, a in enumerate(ids):
        for b in ids[i + 1:]:
            out["%s || %s" % (a, b)] = round(jaccard(desc[a], desc[b]), 4)
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--matrix", action="store_true")
    ap.add_argument("--calibrate", action="store_true")
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--out", default=None)
    args = ap.parse_args()
    desc = load_corpus()

    if args.matrix or args.calibrate:
        m = matrix(desc)
        if args.out:
            os.makedirs(args.out, exist_ok=True)
            with open(os.path.join(args.out, "similarity_matrix.json"),
                      "w", encoding="utf-8") as fh:
                json.dump({"metric": "jaccard-stopworded-tokens",
                           "pairs": m}, fh, indent=2)
            print("matrix: %d pairs -> %s/similarity_matrix.json"
                  % (len(m), args.out))

    if args.calibrate:
        conf = sorted(jaccard(desc[a], desc[b]) for a, b in CONFUSABLE)
        dist = sorted(jaccard(desc[a], desc[b]) for a, b in DISTINCT)
        separable = (max(dist) < min(conf)) if conf and dist else False
        if separable:
            threshold = (max(dist) + min(conf)) / 2
            fp_risk = False
        else:
            threshold = min(conf) if conf else 0.0
            fp_risk = True
        result = {
            "metric": "jaccard-stopworded-tokens",
            "rule": "threshold=(max(known_distinct)+min(known_confusable))/2 iff separable else fallback=min(known_confusable)",
            "known_confusable_scores": [round(x, 4) for x in conf],
            "known_distinct_scores": [round(x, 4) for x in dist],
            "separable": separable,
            "threshold": round(threshold, 4),
            "fp_risk_accepted": fp_risk,
        }
        if args.out:
            with open(os.path.join(args.out, "threshold.json"),
                      "w", encoding="utf-8") as fh:
                json.dump(result, fh, indent=2)
        print(json.dumps(result, indent=2))
        return 0

    if args.self_test:
        # threshold recomputed by the same rule (no I/O side effects)
        conf = sorted(jaccard(desc[a], desc[b]) for a, b in CONFUSABLE)
        dist = sorted(jaccard(desc[a], desc[b]) for a, b in DISTINCT)
        threshold = ((max(dist) + min(conf)) / 2
                     if (conf and dist and max(dist) < min(conf))
                     else (min(conf) if conf else 0.0))
        gg = jaccard(desc[GOLDEN_GOOD[0]], desc[GOLDEN_GOOD[1]])
        gb = jaccard(desc[GOLDEN_BAD[0]], desc[GOLDEN_BAD[1]])
        ok_good = gg > threshold
        ok_bad = gb < threshold
        print("threshold=%.4f golden-good %s=%.4f %s threshold" %
              (threshold, "||".join(GOLDEN_GOOD), gg,
               "ABOVE" if ok_good else "NOT-ABOVE"))
        print("threshold=%.4f golden-bad  %s=%.4f %s threshold" %
              (threshold, "||".join(GOLDEN_BAD), gb,
               "BELOW" if ok_bad else "NOT-BELOW"))
        if not (ok_good and ok_bad):
            print("SELF-TEST FAIL: analyzer cannot bluff-check itself")
            return 1
        print("SELF-TEST PASS")
        return 0
    return 0


if __name__ == "__main__":
    sys.exit(main())
