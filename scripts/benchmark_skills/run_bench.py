#!/usr/bin/env python3
"""T-P2.01/T-P2.02 shadowing benchmark runner — live model ONLY, stdlib only.

Presents N skill {name, description} + a task with a known-correct oracle skill,
asks the live OpenAI-compatible model to pick one, records
{selected, expected, pass, failure_mode}.

Failure modes (closed set):
  pass          selected == expected
  mis-selection model named a different active skill (or several)
  abandonment   model named no active skill (NONE / prose / empty)
  model-error   transport error, timeout, or unparseable HTTP response.
                Recorded as an outcome, NEVER retried-into-success silently.

Determinism: temperature 0 + fixed seed per call. Same tasks, same prompts,
same model tier at every size (T-P2.02.2).

Usage:
  run_bench.py --size small|mid|large --repetition N --out DIR [--seed S]
               [--endpoint URL] [--model ID] [--timeout SEC]
Emits DIR/run_<size>_rep<N>.json
"""
import argparse
import json
import os
import sys
import time
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))

PROMPT_TEMPLATE = """You are a skill router. Pick exactly one skill for the task.

Active skills (name: description):
{skills}

Task: {task}

Reply with EXACTLY one line containing only the name of the single most appropriate skill from the list above. If no skill fits, reply with exactly: NONE. No explanation."""


def load_json(path):
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


def build_active(corpus, distractors_cfg, size):
    by_id = {s["id"]: s for s in corpus["skills"]}
    active = list(corpus["skills"])
    want = distractors_cfg["sizes"][size]["distractor_ids"]
    dmap = {d["id"]: d for d in distractors_cfg["distractors"]}
    for did in want:
        active.append({"id": did, "name": dmap[did]["name"],
                       "description": dmap[did]["description"],
                       "source": dmap[did]["source"]})
    return active


def chat(endpoint, model, prompt, seed, timeout):
    body = json.dumps({
        "model": model,
        "temperature": 0,
        "seed": seed,
        "max_tokens": 128,
        "messages": [{"role": "user", "content": prompt}],
    }).encode()
    req = urllib.request.Request(
        endpoint.rstrip("/") + "/chat/completions",
        data=body, headers={"Content-Type": "application/json"})
    t0 = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            payload = json.loads(resp.read().decode())
    except Exception as exc:  # transport error / timeout: a recorded outcome
        return None, "model-error: %s: %s" % (type(exc).__name__, exc), \
            time.monotonic() - t0
    latency = time.monotonic() - t0
    try:
        text = payload["choices"][0]["message"]["content"]
    except (KeyError, IndexError, TypeError) as exc:
        return None, "model-error: bad-payload: %s" % exc, latency
    return (text or ""), None, latency


def classify(raw, active, expected_id):
    """Return (selected_id_or_None, verdict, failure_mode_or_None)."""
    lowered = raw.strip().lower()
    if lowered == "none":
        return None, "fail", "abandonment"
    hits = []
    for sk in active:
        for key in (sk["name"], sk["id"]):
            if key.lower() in lowered:
                hits.append(sk["id"])
                break
    # name-vs-id double count guard: dedupe preserving order
    seen, uniq = set(), []
    for h in hits:
        if h not in seen:
            seen.add(h)
            uniq.append(h)
    if not uniq:
        return None, "fail", "abandonment"
    if uniq == [expected_id]:
        return expected_id, "pass", None
    return uniq[0], "fail", "mis-selection"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--size", required=True, choices=["small", "mid", "large"])
    ap.add_argument("--repetition", type=int, default=1)
    ap.add_argument("--out", required=True)
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument("--endpoint", default=None)
    ap.add_argument("--model", default=None)
    ap.add_argument("--timeout", type=int, default=90)
    args = ap.parse_args()

    corpus = load_json(os.path.join(HERE, "skill_corpus.json"))
    distractors_cfg = load_json(os.path.join(HERE, "distractors.json"))
    taskset = load_json(os.path.join(HERE, "taskset.json"))
    endpoint = args.endpoint or corpus.get("endpoint")
    model = args.model or corpus.get("model")

    active = build_active(corpus, distractors_cfg, args.size)
    skill_block = "\n".join("- %s: %s" % (s["name"], s["description"])
                            for s in active)
    by_id = {s["id"]: s for s in active}

    results = []
    for task in taskset["tasks"]:
        prompt = PROMPT_TEMPLATE.format(skills=skill_block,
                                        task=task["prompt"])
        raw, err, latency = chat(endpoint, model, prompt, args.seed,
                                 args.timeout)
        if err is not None:
            results.append({
                "task_id": task["id"], "expected": task["expected"],
                "selected": None, "verdict": "fail",
                "failure_mode": "model-error", "detail": err,
                "latency_s": round(latency, 3),
                "prompt_chars": len(prompt),
            })
            continue
        selected, verdict, mode = classify(raw, active, task["expected"])
        results.append({
            "task_id": task["id"], "expected": task["expected"],
            "selected": selected, "verdict": verdict,
            "failure_mode": mode, "raw_output": raw.strip(),
            "latency_s": round(latency, 3),
            "prompt_chars": len(prompt),
        })

    passes = sum(1 for r in results if r["verdict"] == "pass")
    modes = {}
    for r in results:
        if r["failure_mode"]:
            modes[r["failure_mode"]] = modes.get(r["failure_mode"], 0) + 1
    out = {
        "size": args.size, "n_active": len(active),
        "active_ids": [s["id"] for s in active],
        "model": model, "endpoint": endpoint,
        "temperature": 0, "seed": args.seed,
        "repetition": args.repetition,
        "n_tasks": len(results), "n_pass": passes,
        "pass_rate": passes / len(results) if results else 0.0,
        "failures_by_mode": modes,
        "results": results,
    }
    os.makedirs(args.out, exist_ok=True)
    path = os.path.join(args.out,
                        "run_%s_rep%d.json" % (args.size, args.repetition))
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(out, fh, indent=2)
    print("%s: %d/%d pass (%.3f) modes=%s -> %s"
          % (args.size, passes, len(results), out["pass_rate"], modes, path))
    return 0


if __name__ == "__main__":
    sys.exit(main())
