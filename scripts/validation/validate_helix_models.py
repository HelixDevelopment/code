#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
validate_helix_models.py — deterministic validation harness for every model
advertised by the HelixAgent / HelixLLM / coder endpoints, as consumed through
Claude Toolkit provider aliases.

WHY THIS EXISTS
---------------
Every pre-existing verifier on this stack reports green while the system is
substantially broken. This harness is built so that a broken model CANNOT
report green. Concretely it was calibrated against four live defects and each
check below was proven to FAIL on the corresponding real defect and PASS on a
measured honest control (see docs/qa/2026-09-07-helix-models-toolkit/).

CHECKS (per advertised model id, per endpoint)
  A  reachability      id in /v1/models AND a completion returns HTTP 200
  B  real generation   non-empty content AND prompt_tokens>0 AND completion_tokens>0
  C  prompt-dependence two DIFFERENT prompts produce DIFFERENT responses
  D  context fidelity  reported prompt_tokens SCALES with an 8x input increase
  E  needle-in-haystack a unique token buried at 25%/50%/75% depth is retrievable
  F  alias durability  route still works after the toolkit's `sync` (opt-in)

DETERMINISM (Constitution 11.4.50)
  Model WORDING is not deterministic even at temperature 0. This harness
  therefore NEVER asserts on wording. It asserts only on properties that are
  deterministic: HTTP status, usage counters, response non-emptiness,
  byte-INEQUALITY between two different prompts, token-count scaling ratios,
  and needle substring PRESENCE.
  Every verdict-bearing probe is repeated N times (default 3) and the VERDICT
  must be identical across all repeats. A verdict that varies run-to-run is
  reported NON-DETERMINISTIC (a distinct failing state), never majority-voted.

SELF-VALIDATION (Constitution 11.4.107(10))
  `--self-test` runs the whole check battery against an in-process mock server
  serving one golden-GOOD fixture and six golden-BAD fixtures. Each golden-BAD
  fixture declares the exact set of checks it MUST fail. A harness that passes
  its golden-bad fixtures is a bluff gate and exits non-zero.

USAGE
  ./validate_helix_models.py                 # validate the live system
  ./validate_helix_models.py --self-test     # prove the harness can fail
  ./validate_helix_models.py --json out.json # machine-readable matrix
  ./validate_helix_models.py --with-sync     # additionally run check F (MUTATES config)

EXIT CODE
  0 only if every discovered model passed every applicable check AND
  self-validation passed. Any FAIL, ERROR, or NON-DETERMINISTIC verdict -> 1.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import ssl
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field, asdict
from http.server import BaseHTTPRequestHandler, HTTPServer

# --------------------------------------------------------------------------
# Tunables. Endpoints are infrastructure and may be overridden by env; model
# ids are NEVER hardcoded -- they are discovered from /v1/models at run time
# (CONST-036: no hardcoded model lists).
# --------------------------------------------------------------------------

DEFAULT_ENDPOINTS = [
    # name,            base URL
    ("helixagent", os.environ.get("HELIX_AGENT_BASE", "http://127.0.0.1:7061/v1")),
    ("helixllm-gateway", os.environ.get("HELIX_LLM_BASE", "https://127.0.0.1:8443/v1")),
    ("coder", os.environ.get("HELIX_CODER_BASE", "http://127.0.0.1:18434/v1")),
]

WORDS_SMALL = 500
WORDS_LARGE = 4000
NOMINAL_RATIO = WORDS_LARGE / WORDS_SMALL  # 8.0

# Check D pass band. Derived from MEASURED honest controls on this stack
# (coder 7.55x, helixagent-llm 7.77x for an 8x input change) and the MEASURED
# broken route (gateway 1.22x). A route below TRUNCATION_RATIO is provably
# dropping input. The band between is "degraded" and also fails: honest routes
# on this stack sit above 7.5x, so anything under 4x is nowhere near honest.
TRUNCATION_RATIO = 2.0   # below this => silent truncation, hard fail
DEGRADED_RATIO = 4.0     # below this => degraded, still a fail

NEEDLE_DEPTHS = (0.25, 0.50, 0.75)

FILLER = "alpha bravo charlie delta echo foxtrot golf hotel india juliet "

# Distinct, tokenizer-hostile sentinels. Presence is a substring test, never an
# exact-match of the whole reply (wording is not deterministic).
NEEDLE_TOKEN = "MIDKEY-QW3RM8"

PROMPT_A = "What is the capital of France? Reply with the single word only."
PROMPT_B = "Write a Python function that reverses a string. Code only."

DEFAULT_REPEATS = 3
DEFAULT_TIMEOUT = 300

CHECK_IDS = ["A", "B", "C", "D", "E", "F"]
CHECK_NAMES = {
    "A": "reachability",
    "B": "real-generation",
    "C": "prompt-dependence",
    "D": "context-fidelity",
    "E": "needle-in-haystack",
    "F": "alias-durability",
}

PASS, FAIL, ERROR, NONDET, SKIP = "PASS", "FAIL", "ERROR", "NON-DETERMINISTIC", "SKIPPED"
# UNTESTED: the endpoint was not reachable at all (process not listening), so no
# claim about the model can be made in either direction. Distinct from FAIL (the
# model misbehaved) and never counted as a pass.
UNTESTED = "UNTESTED"

_SSL_CTX = ssl.create_default_context()
_SSL_CTX.check_hostname = False
_SSL_CTX.verify_mode = ssl.CERT_NONE


# --------------------------------------------------------------------------
# Result model
# --------------------------------------------------------------------------

@dataclass
class CheckResult:
    check: str
    name: str
    verdict: str
    detail: str = ""
    measurements: dict = field(default_factory=dict)

    @property
    def ok(self) -> bool:
        return self.verdict in (PASS, SKIP)


@dataclass
class ModelResult:
    endpoint: str
    base_url: str
    model: str
    checks: list = field(default_factory=list)

    @property
    def ready(self) -> bool:
        return all(c.ok for c in self.checks)


# --------------------------------------------------------------------------
# HTTP plumbing
# --------------------------------------------------------------------------

class HttpResult:
    __slots__ = ("status", "body", "error")

    def __init__(self, status=None, body=None, error=None):
        self.status = status
        self.body = body
        self.error = error


def _url_origin(url: str) -> str:
    """scheme://host:port -- the granularity at which credentials are scoped."""
    try:
        p = urllib.parse.urlsplit(url)
        return "%s://%s" % (p.scheme, p.netloc)
    except Exception:
        return url


def resolve_api_key(base_url: str, endpoint_name: str) -> str | None:
    """
    Resolve the bearer token for an endpoint WITHOUT committing any credential
    to the repository (CONST-042 / Article XII 12.1). Resolution order:

      1. HELIX_<ENDPOINT>_KEY   (e.g. HELIX_HELIXAGENT_KEY)
      2. HELIX_API_KEY          (applies to every endpoint)
      3. the local Claude-Toolkit / claude-code-router provider records, which
         already hold the loopback token for this exact origin

    These endpoints began requiring `Authorization` part-way through this work.
    Without this resolution the harness reports 401 as an endpoint ERROR, which
    would be a false negative dressed up as a finding.
    """
    slug = endpoint_name.upper().replace("-", "_")
    for var in ("HELIX_%s_KEY" % slug, "HELIX_API_KEY"):
        val = os.environ.get(var)
        if val:
            return val
    origin = _url_origin(base_url)
    ccr = os.path.join(os.path.expanduser("~"), ".claude-code-router")

    # Collect EVERY candidate for this origin, then choose by MEASUREMENT.
    #
    # Returning the first directory that mentioned the origin was a real defect,
    # not a theoretical one. Several provider records can point at the same
    # endpoint, each holding the key that was live when THAT alias last
    # launched, so a record for a provider that has since stopped launching
    # keeps a key that has since been rotated -- and directory order decided
    # which one won. Measured 2026-09-07: two records pointed at :8443, the
    # alphabetically-first held a rotated-out key, and this harness reported
    # the endpoint as 401-unreachable while the endpoint was in fact serving
    # completions to the correct key. That is a FALSE FAILURE, and it is
    # exactly as damaging as a false pass: it sends the reader to debug an
    # endpoint that was never broken.
    #
    # Guessing which copy is fresher (mtime, path depth, name specificity) is
    # still a guess. The endpoint itself is the authority on which credential
    # it accepts, so ask it: probe each candidate and keep the first that is
    # not rejected. Order is preserved so the outcome stays deterministic when
    # more than one candidate is valid.
    candidates = []
    try:
        for entry in sorted(os.listdir(ccr)):
            cfg = os.path.join(ccr, entry, "config.json")
            if not os.path.isfile(cfg):
                continue
            try:
                with open(cfg) as f:
                    doc = json.load(f)
            except Exception:
                continue
            for prov in doc.get("Providers", []) or []:
                if _url_origin(prov.get("api_base_url", "")) == origin:
                    key = prov.get("api_key")
                    if key and key not in candidates:
                        candidates.append(key)
    except Exception:
        pass

    if not candidates:
        return None
    if len(candidates) == 1:
        return candidates[0]

    probe = origin.rstrip("/") + "/v1/models"
    for key in candidates:
        try:
            req = urllib.request.Request(
                probe, headers={"Authorization": "Bearer " + key,
                                "Accept": "application/json"}, method="GET")
            with urllib.request.urlopen(req, timeout=10, context=_SSL_CTX):
                return key
        except urllib.error.HTTPError as e:
            if e.code in (401, 403):
                continue          # rejected credential -- try the next
            return key            # reached and authenticated; other codes are
                                  # the endpoint's business, not the key's
        except Exception:
            # Transport-level failure says nothing about the credential, so it
            # must not silently disqualify one. Fall back to declared order.
            return candidates[0]
    # Every candidate was rejected. Return the first so the caller reports the
    # endpoint's own 401 rather than a misleading "no credential found".
    return candidates[0]


# origin -> bearer token, populated during discovery. Never logged.
_API_KEYS: dict = {}


def http_json(url: str, payload: dict | None, timeout: int) -> HttpResult:
    """POST (or GET when payload is None) returning status + parsed JSON."""
    data = json.dumps(payload).encode() if payload is not None else None
    headers = {"Content-Type": "application/json", "Accept": "application/json"}
    key = _API_KEYS.get(_url_origin(url)) or os.environ.get("HELIX_API_KEY")
    if key:
        headers["Authorization"] = "Bearer " + key
    req = urllib.request.Request(url, data=data, headers=headers,
                                 method="POST" if data else "GET")
    try:
        with urllib.request.urlopen(req, timeout=timeout, context=_SSL_CTX) as f:
            raw = f.read()
            try:
                return HttpResult(status=f.status, body=json.loads(raw))
            except json.JSONDecodeError:
                return HttpResult(status=f.status, body=None,
                                  error="non-JSON body: %r" % raw[:200])
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            body = json.loads(raw)
        except Exception:
            body = None
        return HttpResult(status=e.code, body=body,
                          error="HTTP %d: %r" % (e.code, raw[:200]))
    except Exception as e:  # connection refused, timeout, TLS, ...
        return HttpResult(error="%s: %s" % (type(e).__name__, e))


def completion(base_url: str, model: str, content: str, timeout: int,
               max_tokens: int = 48) -> HttpResult:
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": content}],
        "temperature": 0,      # determinism knob 1
        "seed": 12345,         # determinism knob 2 (ignored by servers that lack it)
        "max_tokens": max_tokens,
        "stream": False,
    }
    return http_json(base_url.rstrip("/") + "/chat/completions", payload, timeout)


def resp_text(body) -> str:
    try:
        msg = body["choices"][0]["message"]
        return (msg.get("content") or "").strip()
    except Exception:
        return ""


def resp_usage(body) -> tuple:
    """-> (prompt_tokens, completion_tokens) or (None, None) when absent."""
    try:
        u = body.get("usage") or {}
        return u.get("prompt_tokens"), u.get("completion_tokens")
    except Exception:
        return None, None


# --------------------------------------------------------------------------
# Prompt builders
# --------------------------------------------------------------------------

def filler_words(nwords: int) -> list:
    reps = nwords // 10 + 1
    return (FILLER * reps).split()[:nwords]


def scaling_prompt(nwords: int) -> str:
    """Same SHAPE at both sizes; only the filler volume changes."""
    return ("Reply with the single word OK.\n" + " ".join(filler_words(nwords)))


def needle_prompt(nwords: int, depth: float, token: str) -> str:
    words = filler_words(nwords)
    cut = max(0, min(len(words), int(len(words) * depth)))
    return ("Below is a long log. Exactly one line contains a secret key.\n"
            + " ".join(words[:cut])
            + "\n>>> The secret key is " + token + " <<<\n"
            + " ".join(words[cut:])
            + "\n\nWhat is the secret key contained in the log above? "
              "Reply with only the key.")


# --------------------------------------------------------------------------
# Determinism wrapper
# --------------------------------------------------------------------------

def stable(probe, repeats: int):
    """
    Run `probe` (a zero-arg callable returning (verdict, detail, measurements))
    `repeats` times. Return the single agreed result, or a NON-DETERMINISTIC
    result naming the disagreeing verdicts. Verdicts are never majority-voted.
    """
    runs = []
    for _ in range(repeats):
        try:
            runs.append(probe())
        except Exception as e:
            runs.append((ERROR, "probe raised %s: %s" % (type(e).__name__, e), {}))
    verdicts = {r[0] for r in runs}
    if len(verdicts) == 1:
        return runs[0][0], runs[0][1], dict(runs[0][2], repeats=repeats)
    return (NONDET,
            "verdict varied across %d repeats: %s" % (repeats, sorted(verdicts)),
            {"repeats": repeats, "verdicts": [r[0] for r in runs],
             "details": [r[1] for r in runs]})


# --------------------------------------------------------------------------
# Checks
# --------------------------------------------------------------------------

def check_A(base_url, model, advertised, timeout, repeats) -> CheckResult:
    """Reachability: advertised in /v1/models AND a completion returns 200."""
    if model not in advertised:
        return CheckResult("A", CHECK_NAMES["A"], FAIL,
                           "model id not present in /v1/models", {})

    def probe():
        r = completion(base_url, model, "ping", timeout, max_tokens=8)
        if r.error and r.status is None:
            # No HTTP response at all: the service is not listening. That is an
            # environment fact, not evidence about the model. Reporting it as
            # FAIL would blame the model for someone else's stopped process;
            # reporting it as PASS would be a bluff. It is UNTESTED.
            return UNTESTED, ("endpoint not reachable (%s) -- service appears "
                              "down; no claim can be made about this model"
                              % r.error), {"status": None}
        if r.status != 200:
            return FAIL, "completion returned HTTP %s (%s)" % (r.status, r.error), \
                   {"status": r.status}
        return PASS, "advertised and HTTP 200", {"status": r.status}

    v, d, m = stable(probe, repeats)
    return CheckResult("A", CHECK_NAMES["A"], v, d, m)


def check_B(base_url, model, timeout, repeats) -> CheckResult:
    """Real generation: non-empty content AND both usage counters > 0."""
    def probe():
        r = completion(base_url, model, PROMPT_A, timeout)
        if r.status != 200:
            return ERROR, "HTTP %s (%s)" % (r.status, r.error), {"status": r.status}
        text = resp_text(r.body)
        pt, ct = resp_usage(r.body)
        meas = {"prompt_tokens": pt, "completion_tokens": ct, "content_len": len(text)}
        problems = []
        if not text:
            problems.append("empty content")
        if not isinstance(pt, int) or pt <= 0:
            problems.append("prompt_tokens=%r (expected >0)" % (pt,))
        if not isinstance(ct, int) or ct <= 0:
            problems.append("completion_tokens=%r (expected >0)" % (ct,))
        if problems:
            return FAIL, "; ".join(problems) + \
                   " -- all-zero usage beside HTTP 200 means no LLM ran", meas
        return PASS, "content %d chars, prompt_tokens=%d completion_tokens=%d" \
               % (len(text), pt, ct), meas

    v, d, m = stable(probe, repeats)
    return CheckResult("B", CHECK_NAMES["B"], v, d, m)


def check_C(base_url, model, timeout, repeats) -> CheckResult:
    """
    Prompt-dependence: two DIFFERENT prompts must produce DIFFERENT responses.
    Byte-INEQUALITY only -- never an assertion about what the model said.
    """
    def probe():
        ra = completion(base_url, model, PROMPT_A, timeout)
        rb = completion(base_url, model, PROMPT_B, timeout)
        if ra.status != 200 or rb.status != 200:
            return ERROR, "HTTP %s / %s" % (ra.status, rb.status), {}
        ta, tb = resp_text(ra.body), resp_text(rb.body)
        meas = {"reply_a_len": len(ta), "reply_b_len": len(tb),
                "reply_a": ta[:160], "reply_b": tb[:160]}
        if ta == tb:
            return FAIL, "byte-identical reply to two different questions " \
                         "-- canned string, not a model", meas
        return PASS, "distinct replies to distinct prompts", meas

    v, d, m = stable(probe, repeats)
    return CheckResult("C", CHECK_NAMES["C"], v, d, m)


def check_D(base_url, model, timeout, repeats) -> CheckResult:
    """
    Context fidelity: reported prompt_tokens must SCALE with input size.
    THE CHECK EVERY EXISTING VERIFIER MISSES -- every other probe is small
    enough to fit under a truncating route's cap, so the route looks healthy.
    """
    def probe():
        rs = completion(base_url, model, scaling_prompt(WORDS_SMALL), timeout, 8)
        rl = completion(base_url, model, scaling_prompt(WORDS_LARGE), timeout, 8)
        if rs.status != 200 or rl.status != 200:
            return ERROR, "HTTP %s / %s" % (rs.status, rl.status), {}
        ps, _ = resp_usage(rs.body)
        pl, _ = resp_usage(rl.body)
        if not isinstance(ps, int) or not isinstance(pl, int) or ps <= 0:
            return FAIL, "usable prompt_tokens not reported (small=%r large=%r); " \
                         "context fidelity cannot be accounted" % (ps, pl), \
                   {"prompt_tokens_small": ps, "prompt_tokens_large": pl}
        ratio = pl / ps
        meas = {"words_small": WORDS_SMALL, "words_large": WORDS_LARGE,
                "nominal_input_ratio": NOMINAL_RATIO,
                "prompt_tokens_small": ps, "prompt_tokens_large": pl,
                "measured_token_ratio": round(ratio, 3)}
        if ratio < TRUNCATION_RATIO:
            return FAIL, ("SILENT TRUNCATION: input grew %.1fx (%d->%d words) but "
                          "reported prompt_tokens grew only %.2fx (%d->%d). "
                          "~%.0f%% of the prompt is being dropped and the route "
                          "still answers HTTP 200."
                          % (NOMINAL_RATIO, WORDS_SMALL, WORDS_LARGE, ratio, ps, pl,
                             100.0 * (1.0 - ratio / NOMINAL_RATIO))), meas
        if ratio < DEGRADED_RATIO:
            return FAIL, ("DEGRADED CONTEXT: input grew %.1fx but reported "
                          "prompt_tokens grew only %.2fx (%d->%d); honest routes on "
                          "this stack measure >=%.1fx."
                          % (NOMINAL_RATIO, ratio, ps, pl, DEGRADED_RATIO)), meas
        return PASS, ("prompt_tokens scaled %.2fx for a %.1fx input increase "
                      "(%d->%d)" % (ratio, NOMINAL_RATIO, ps, pl)), meas

    v, d, m = stable(probe, repeats)
    return CheckResult("D", CHECK_NAMES["D"], v, d, m)


def check_E(base_url, model, timeout, repeats) -> CheckResult:
    """
    Needle-in-haystack at three depths. Stronger than D: D trusts the route's
    own accounting, which can lie; a needle cannot. Tests DELIVERED context.

    Three depths make the check truncation-direction agnostic. This stack's
    broken gateway keeps head+tail and drops the MIDDLE, so a start-only needle
    survives on the broken route -- a single-ended needle would be a bluff gate.
    """
    def probe():
        missing, found, meas = [], [], {}
        for depth in NEEDLE_DEPTHS:
            token = "%s-D%02d" % (NEEDLE_TOKEN, int(depth * 100))
            r = completion(base_url, model,
                           needle_prompt(WORDS_LARGE, depth, token), timeout, 32)
            if r.status != 200:
                return ERROR, "HTTP %s at depth %.2f" % (r.status, depth), meas
            text = resp_text(r.body)
            pt, _ = resp_usage(r.body)
            meas["depth_%02d" % int(depth * 100)] = {
                "prompt_tokens": pt, "found": token in text, "reply": text[:120]}
            (found if token in text else missing).append("%.0f%%" % (depth * 100))
        if missing:
            return FAIL, ("buried token NOT returned at depth(s) %s (retrieved at %s) "
                          "-- context delivered to the model is incomplete; this is "
                          "the user-visible impact of truncation"
                          % (", ".join(missing), ", ".join(found) or "none")), meas
        return PASS, "buried token retrieved at all depths (%s)" \
               % ", ".join("%.0f%%" % (d * 100) for d in NEEDLE_DEPTHS), meas

    v, d, m = stable(probe, repeats)
    return CheckResult("E", CHECK_NAMES["E"], v, d, m)


def check_F(base_url, model, timeout, repeats, enabled: bool) -> CheckResult:
    """
    Alias round-trip durability: run the toolkit's `sync`, then re-probe.
    A route that stops working after sync FAILS.

    Default DISABLED: `claude-providers sync` MUTATES shared provider config
    that other agents may be actively repairing. Running it unbidden would
    clobber in-flight work. Opt in with --with-sync when you own that config.
    """
    if not enabled:
        return CheckResult("F", CHECK_NAMES["F"], SKIP,
                           "not run: `claude-providers sync` mutates shared provider "
                           "config; enable with --with-sync when you own it", {})
    cmd = os.environ.get("HELIX_SYNC_CMD", "claude-providers sync")
    try:
        proc = subprocess.run(cmd, shell=True, capture_output=True,
                              text=True, timeout=timeout)
    except Exception as e:
        return CheckResult("F", CHECK_NAMES["F"], ERROR,
                           "sync command failed to run: %s" % e, {"cmd": cmd})
    meas = {"cmd": cmd, "sync_returncode": proc.returncode,
            "sync_stderr_tail": (proc.stderr or "")[-400:]}

    def probe():
        r = completion(base_url, model, PROMPT_A, timeout)
        if r.status != 200:
            return FAIL, "route broken after sync: HTTP %s (%s)" % (r.status, r.error), {}
        if not resp_text(r.body):
            return FAIL, "route returns empty content after sync", {}
        return PASS, "route still serving after sync", {}

    v, d, m = stable(probe, repeats)
    meas.update(m)
    return CheckResult("F", CHECK_NAMES["F"], v, d, meas)


# --------------------------------------------------------------------------
# Discovery + orchestration
# --------------------------------------------------------------------------

def discover_models(base_url: str, timeout: int) -> tuple:
    """-> (list_of_model_ids, error_or_None). Never a hardcoded list."""
    r = http_json(base_url.rstrip("/") + "/models", None, timeout)
    if r.status != 200 or not isinstance(r.body, dict):
        return [], "GET /v1/models -> status=%s err=%s" % (r.status, r.error)
    data = r.body.get("data")
    if not isinstance(data, list):
        return [], "/v1/models has no 'data' array"
    ids = [d.get("id") for d in data if isinstance(d, dict) and d.get("id")]
    return ids, None


def validate_model(endpoint, base_url, model, advertised, timeout, repeats,
                   with_sync) -> ModelResult:
    mr = ModelResult(endpoint=endpoint, base_url=base_url, model=model)
    a = check_A(base_url, model, advertised, timeout, repeats)
    mr.checks.append(a)
    if a.verdict != PASS:
        # Unreachable: downstream checks cannot produce meaningful evidence.
        # Propagate UNTESTED (service down) rather than SKIPPED, because SKIPPED
        # is treated as benign and would let a whole endpoint outage read as a
        # clean run.
        down = a.verdict == UNTESTED
        verdict = UNTESTED if down else SKIP
        why = ("not evaluated: endpoint unreachable" if down
               else "not evaluated: check A did not pass")
        for cid in ("B", "C", "D", "E", "F"):
            mr.checks.append(CheckResult(cid, CHECK_NAMES[cid], verdict, why, {}))
        return mr
    mr.checks.append(check_B(base_url, model, timeout, repeats))
    mr.checks.append(check_C(base_url, model, timeout, repeats))
    mr.checks.append(check_D(base_url, model, timeout, repeats))
    mr.checks.append(check_E(base_url, model, timeout, repeats))
    mr.checks.append(check_F(base_url, model, timeout, repeats, with_sync))
    return mr


def run_suite(endpoints, timeout, repeats, with_sync, log=print) -> tuple:
    results, discovery = [], []
    for name, base in endpoints:
        # Register the endpoint's bearer token (if any) before any probe, so a
        # protected endpoint is not mis-reported as a 401 failure.
        key = resolve_api_key(base, name)
        if key:
            _API_KEYS[_url_origin(base)] = key
        ids, err = discover_models(base, timeout)
        discovery.append({"endpoint": name, "base_url": base, "models": ids,
                          "error": err, "authenticated": bool(key)})
        if err:
            log("  [%s] %s  -- DISCOVERY FAILED: %s" % (name, base, err))
            # An endpoint that refuses discovery must NOT silently vanish from
            # the matrix. Without this row a 401'ing or dead endpoint simply
            # contributes no models, and the report reads as though there was
            # nothing there to test -- an absence that looks like a clean run.
            # Emit a visible, counted row instead.
            transport_down = "status=None" in err or "URLError" in err
            mr = ModelResult(endpoint=name, base_url=base,
                             model="<endpoint discovery failed>")
            mr.checks.append(CheckResult(
                "A", CHECK_NAMES["A"],
                UNTESTED if transport_down else FAIL,
                ("endpoint unreachable: %s" % err) if transport_down else
                ("endpoint refused model discovery: %s -- every model behind "
                 "this endpoint is unusable through its alias" % err),
                {"authenticated": bool(key)}))
            for cid in ("B", "C", "D", "E", "F"):
                mr.checks.append(CheckResult(
                    cid, CHECK_NAMES[cid], UNTESTED,
                    "not evaluated: endpoint discovery failed", {}))
            results.append(mr)
            continue
        log("  [%s] %s  -- %d model(s): %s" % (name, base, len(ids), ", ".join(ids)))
        for mid in ids:
            log("      probing %s ..." % mid)
            results.append(validate_model(name, base, mid, ids, timeout,
                                          repeats, with_sync))
    return results, discovery


# --------------------------------------------------------------------------
# Self-validation: in-process mock server + golden fixtures
# --------------------------------------------------------------------------

# model id -> the EXACT set of check ids that MUST fail. Empty set == golden-good.
#
# These sets are asserted exactly, not as a subset: a fixture that fails MORE
# checks than declared is as much a bug as one that fails fewer, because it
# means a check is firing for a reason the fixture did not intend to model.
# Each set below was reconciled against the MEASURED behaviour of the real
# defect it mirrors (see docs/qa/2026-09-07-helix-models-toolkit/VALIDATION.md).
GOLDEN_FIXTURES = {
    # honest route: passes everything
    "golden-good": set(),
    # 503 dead pin (defect 3): nothing downstream is evaluable
    "bad-dead-pin": {"A"},
    # canned IDENTICAL string + all-zero usage (helixagent-ensemble / helix-debate
    # shape). Zero usage also makes context unaccountable (D) and the stub cannot
    # retrieve a buried token (E) -- all four failures are correct, not spurious.
    "bad-stub-identical": {"B", "C", "D", "E"},
    # canned but VARYING status strings + all-zero usage (helixagent-debate shape).
    # C PASSES here. This fixture is the standing proof that byte-inequality alone
    # can never be trusted as the stub detector -- only B exposes this route.
    "bad-stub-varying": {"B", "D", "E"},
    # genuine prompt-dependent text, but the route reports all-zero usage: B fails,
    # and D cannot account scaling without usable counters. E still passes because
    # context IS delivered -- isolating "lies in its usage block" from "drops input".
    "bad-zero-usage": {"B", "D"},
    # flat prompt_tokens + middle-of-prompt dropped (the measured live gateway
    # shape): answers HTTP 200 with real, prompt-dependent text, so A/B/C all pass
    # and ONLY the context checks expose it. This is the defect every existing
    # verifier misses.
    "bad-truncating": {"D", "E"},
    # honest accounting, context NOT delivered: proves E is independent of D and
    # catches a route whose usage block is truthful-looking but whose prompt is not.
    "bad-needle-dropped": {"E"},
}


class _MockHandler(BaseHTTPRequestHandler):
    _counter = {"n": 0}

    def log_message(self, *a):
        pass

    def _send(self, code, obj):
        raw = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path.endswith("/models"):
            self._send(200, {"object": "list",
                             "data": [{"id": m, "object": "model"}
                                      for m in GOLDEN_FIXTURES]})
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        try:
            req = json.loads(self.rfile.read(n) or b"{}")
        except Exception:
            return self._send(400, {"error": "bad json"})
        model = req.get("model", "")
        prompt = ""
        for m in req.get("messages", []):
            prompt += m.get("content", "")
        words = prompt.split()
        nw = len(words)
        # Honest tokenizer emulation: ~1.5 tokens per filler word.
        honest_pt = int(nw * 1.5) + 10

        def needles_in(text):
            return [w.strip("<>. ") for w in text.split()
                    if w.strip("<>. ").startswith(NEEDLE_TOKEN)]

        found = needles_in(prompt)

        def echo(pt, ct=4, extra=""):
            # Prompt-dependent body: echo any needle, else a token derived from a
            # HASH of the whole prompt. Never a fixed string, so golden-good
            # passes check C honestly. The hash (rather than the word count) is
            # deliberate: two different probe prompts of equal length would
            # otherwise collide and make golden-good fail C spuriously, turning
            # the self-test into a false alarm.
            digest = hashlib.sha256(prompt.encode()).hexdigest()[:12]
            body = (" ".join(found) if found else "OK-%s%s" % (digest, extra))
            return {"id": "mock", "object": "chat.completion", "model": model,
                    "choices": [{"index": 0, "finish_reason": "stop",
                                 "message": {"role": "assistant", "content": body}}],
                    "usage": {"prompt_tokens": pt, "completion_tokens": ct,
                              "total_tokens": pt + ct}}

        if model == "golden-good":
            return self._send(200, echo(honest_pt))

        if model == "bad-dead-pin":
            return self._send(503, {"error": {"message": "model not available"}})

        if model == "bad-stub-identical":
            return self._send(200, {
                "id": "mock", "object": "chat.completion", "model": model,
                "choices": [{"index": 0, "finish_reason": "stop",
                             "message": {"role": "assistant",
                                         "content": "Comprehensive debate completed with 3 rounds"}}],
                "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}})

        if model == "bad-stub-varying":
            # The real helixagent-debate shape: canned STATUS strings that
            # differ between prompts, so byte-inequality (C) passes. Only the
            # all-zero usage (B) exposes it. This fixture is the reason C alone
            # can never be trusted as the stub detector.
            self._counter["n"] += 1
            canned = ["Comprehensive debate completed with 3 rounds",
                      "All agent tasks failed during execution."]
            return self._send(200, {
                "id": "mock", "object": "chat.completion", "model": model,
                "choices": [{"index": 0, "finish_reason": "stop",
                             "message": {"role": "assistant",
                                         "content": canned[self._counter["n"] % 2]}}],
                "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}})

        if model == "bad-zero-usage":
            r = echo(honest_pt)
            r["usage"] = {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
            return self._send(200, r)

        if model == "bad-truncating":
            # Keep head+tail, drop the middle, and cap reported tokens -- the
            # measured shape of the live gateway defect.
            capped = min(honest_pt, 1000)
            keep = 350
            visible = set(needles_in(" ".join(words[:keep]))) | \
                      set(needles_in(" ".join(words[-keep:])))
            # Prompt-DEPENDENT when no needle survived, mirroring the real
            # gateway: it answers short prompts perfectly well, so it passes
            # A/B/C and is exposed ONLY by the context checks. A constant here
            # would trip C and make the fixture unfaithful to the live defect.
            body = (" ".join(sorted(visible)) if visible
                    else "OK-" + hashlib.sha256(prompt.encode()).hexdigest()[:12])
            return self._send(200, {
                "id": "mock", "object": "chat.completion", "model": model,
                "choices": [{"index": 0, "finish_reason": "stop",
                             "message": {"role": "assistant", "content": body}}],
                "usage": {"prompt_tokens": capped, "completion_tokens": 3,
                          "total_tokens": capped + 3}})

        if model == "bad-needle-dropped":
            # Accounting looks perfect; delivered context is not. Proves E is
            # independent of D and catches a route that lies in its usage block.
            r = echo(honest_pt)
            r["choices"][0]["message"]["content"] = \
                "OK-" + hashlib.sha256(prompt.encode()).hexdigest()[:12]
            return self._send(200, r)

        return self._send(404, {"error": "unknown model"})


def run_self_test(repeats, log=print) -> bool:
    srv = HTTPServer(("127.0.0.1", 0), _MockHandler)
    port = srv.server_address[1]
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    base = "http://127.0.0.1:%d/v1" % port
    log("Self-validation: mock server on %s" % base)
    log("  A harness that PASSES its golden-bad fixtures is a bluff gate.\n")
    ok = True
    try:
        advertised, err = discover_models(base, 30)
        if err:
            log("  FATAL: mock discovery failed: %s" % err)
            return False
        for model, must_fail in GOLDEN_FIXTURES.items():
            mr = validate_model("mock", base, model, advertised, 30, repeats, False)
            actual = {c.check for c in mr.checks
                      if c.verdict in (FAIL, ERROR, NONDET)}
            kind = "golden-GOOD" if not must_fail else "golden-BAD "
            if actual == must_fail:
                log("  [OK]   %-12s %-20s failing checks == expected %s"
                    % (kind, model, sorted(must_fail) or "{} (none)"))
            else:
                ok = False
                log("  [BUG]  %-12s %-20s expected failing %s, got %s"
                    % (kind, model, sorted(must_fail), sorted(actual)))
                for c in mr.checks:
                    log("           %s %-18s %-18s %s"
                        % (c.check, c.name, c.verdict, c.detail[:110]))
    finally:
        srv.shutdown()
    log("")
    log("Self-validation: %s" % ("PASS -- harness provably fails broken fixtures "
                                "and passes the honest one" if ok
                                else "FAIL -- harness cannot be trusted"))
    return ok


# --------------------------------------------------------------------------
# Reporting
# --------------------------------------------------------------------------

def print_matrix(results, log=print):
    log("")
    log("=" * 100)
    log("RESULT MATRIX  (model x check)")
    log("=" * 100)
    hdr = "%-46s %-9s %-9s %-9s %-9s %-9s %-9s" % (
        "MODEL", "A reach", "B real", "C prompt", "D ctx", "E needle", "F sync")
    log(hdr)
    log("-" * 100)
    short = {PASS: "PASS", FAIL: "FAIL", ERROR: "ERROR",
             NONDET: "NONDET", SKIP: "skip", UNTESTED: "UNTEST"}
    for r in results:
        cells = []
        by = {c.check: c for c in r.checks}
        for cid in CHECK_IDS:
            c = by.get(cid)
            cells.append(short.get(c.verdict, "?") if c else "-")
        log("%-46s %-9s %-9s %-9s %-9s %-9s %-9s"
            % ((r.endpoint + "/" + r.model)[:46], *cells))
    log("-" * 100)
    log("")
    log("FAILURE DETAIL (measured numbers, not adjectives)")
    log("=" * 100)
    any_fail = False
    for r in results:
        bad = [c for c in r.checks if c.verdict in (FAIL, ERROR, NONDET, UNTESTED)]
        if not bad:
            continue
        any_fail = True
        log("")
        log("%s / %s   (%s)" % (r.endpoint, r.model, r.base_url))
        for c in bad:
            log("  [%s] %s -- %s" % (c.check, c.name, c.verdict))
            log("      %s" % c.detail)
            if c.measurements:
                log("      measurements: %s" % json.dumps(c.measurements)[:600])
    if not any_fail:
        log("  (none)")
    log("")
    log("=" * 100)
    log("PER-MODEL VERDICT")
    log("=" * 100)
    for r in results:
        failed = [c.check for c in r.checks if c.verdict in (FAIL, ERROR, NONDET)]
        untested = [c.check for c in r.checks if c.verdict == UNTESTED]
        skipped = [c.check for c in r.checks if c.verdict == SKIP]
        if r.ready:
            note = " (checks %s not evaluated)" % ",".join(skipped) if skipped else ""
            log("  READY      %s/%s%s" % (r.endpoint, r.model, note))
        elif untested and not failed:
            log("  UNTESTED   %s/%s  -- endpoint unreachable; NOT a pass and "
                "NOT a model defect" % (r.endpoint, r.model))
        else:
            extra = (" ; untested %s" % ",".join(untested)) if untested else ""
            log("  NOT-READY  %s/%s  -- failed %s%s"
                % (r.endpoint, r.model, ",".join(failed), extra))


def main() -> int:
    ap = argparse.ArgumentParser(
        description="Deterministic validation harness for HelixAgent/HelixLLM models.")
    ap.add_argument("--self-test", action="store_true",
                    help="run golden-good/golden-bad fixtures and exit")
    ap.add_argument("--no-self-test", action="store_true",
                    help="skip the self-validation preamble (not recommended)")
    ap.add_argument("--with-sync", action="store_true",
                    help="also run check F (MUTATES shared provider config)")
    ap.add_argument("--repeats", type=int, default=DEFAULT_REPEATS,
                    help="determinism repeats per verdict-bearing probe (default 3)")
    ap.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT,
                    help="per-request timeout in seconds (default 300)")
    ap.add_argument("--json", metavar="PATH", help="write the full matrix as JSON")
    args = ap.parse_args()

    started = time.time()
    print("HelixAgent / HelixLLM model validation harness")
    print("run started %s" % time.strftime("%Y-%m-%dT%H:%M:%S%z"))
    print("determinism: temperature=0, seed pinned, %d repeats per verdict; "
          "no assertion on model wording" % args.repeats)
    print("")

    if args.self_test:
        return 0 if run_self_test(args.repeats) else 1

    self_ok = True
    if not args.no_self_test:
        self_ok = run_self_test(args.repeats)
        print("")
        if not self_ok:
            print("ABORT: harness failed its own golden-bad fixtures; live results "
                  "from an unvalidated harness would be worthless.")
            return 1

    print("Discovering models (no hardcoded model lists):")
    results, discovery = run_suite(DEFAULT_ENDPOINTS, args.timeout,
                                   args.repeats, args.with_sync)
    print_matrix(results)

    ready = sum(1 for r in results if r.ready)
    total = len(results)
    untested_models = sum(
        1 for r in results
        if not r.ready and any(c.verdict == UNTESTED for c in r.checks)
        and not any(c.verdict in (FAIL, ERROR, NONDET) for c in r.checks))
    disc_err = [d for d in discovery if d.get("error")]
    print("")
    print("SUMMARY: %d/%d models READY; %d UNTESTED (endpoint down); "
          "%d endpoint discovery failure(s); self-validation %s; elapsed %.0fs"
          % (ready, total, untested_models, len(disc_err),
             "PASS" if self_ok else "FAIL", time.time() - started))
    if not args.with_sync:
        print("NOTE: check F (alias durability after `sync`) was NOT RUN. It is "
              "gated because sync mutates shared provider config. F is reported "
              "as SKIPPED, never as passing.")

    if args.json:
        payload = {
            "generated": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
            "self_validation_passed": self_ok,
            "repeats": args.repeats,
            "thresholds": {"truncation_ratio": TRUNCATION_RATIO,
                           "degraded_ratio": DEGRADED_RATIO,
                           "nominal_input_ratio": NOMINAL_RATIO},
            "discovery": discovery,
            "results": [asdict(r) for r in results],
        }
        with open(args.json, "w") as f:
            json.dump(payload, f, indent=2)
        print("JSON matrix written to %s" % args.json)

    all_ok = self_ok and total > 0 and ready == total and not disc_err
    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())
