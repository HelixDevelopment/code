#!/usr/bin/env python3
"""Record REAL tool-calling wire responses from the live HelixLLM gateway and coder.

This is the §11.4.77 regeneration mechanism for
`helix_code/testdata/toolcall_wire_corpus/`. The corpus is committed so the
deterministic replay guards can run with no network and no model, but the
bytes in it are genuine captured wire evidence (§11.4.5), never hand-written
fixtures — this script is how they got there and how they are re-obtained.

WHY A CORPUS EXISTS AT ALL (the measurement that forced this design):
    12 byte-identical POSTs to https://127.0.0.1:8443/v1/chat/completions,
    temperature 0, max_tokens 64, one fixed prompt and one fixed nonce, gave
    TWO different outcomes:
        8/12  finish_reason=tool_calls   tool_calls PRESENT
        4/12  finish_reason=length       tool_calls ABSENT
    A live assertion on a single outcome therefore cannot ever be
    deterministic. See the header of
    internal/server/llm_generate_toolcall_replay_test.go.

Usage (from anywhere):
    python3 helix_code/testdata/toolcall_wire_corpus/capture.py            # capture all
    python3 helix_code/testdata/toolcall_wire_corpus/capture.py --verify   # re-hash only

Environment:
    HELIX_LLM_GATEWAY_ENDPOINT        default https://127.0.0.1:8443/v1
    HELIX_LLM_LOCAL_OPENAI_ENDPOINT   default http://localhost:18434
"""
import argparse
import hashlib
import json
import os
import ssl
import sys
import time
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
CORPUS = HERE  # the recorder lives inside the corpus it regenerates

GATEWAY = os.environ.get("HELIX_LLM_GATEWAY_ENDPOINT", "https://127.0.0.1:8443/v1").rstrip("/")
CODER = os.environ.get("HELIX_LLM_LOCAL_OPENAI_ENDPOINT", "http://localhost:18434").rstrip("/")

# The gateway serves a SELF-SIGNED certificate, so the system trust store
# cannot verify it. This recorder trusts the SAME in-repo CA the production
# route trusts (resolveHelixLLMGatewayProvider -> OpenAICompatibleConfig.
# CACertFile) — it does NOT skip verification, and there is deliberately no
# "fall back to unverified" path here either: a recorder that silently
# accepted an unverified peer could capture bytes from something that is not
# the gateway, and the whole value of this corpus is that its bytes provably
# came from the real one.
_CA = os.environ.get("HELIX_LLM_GATEWAY_CA_CERT") or os.path.normpath(
    os.path.join(HERE, "..", "..", "..", "submodules", "helix_llm", "certs", "cert.pem"))
if not os.path.isfile(_CA):
    sys.exit(f"FATAL: gateway CA certificate not found at {_CA} — set "
             "HELIX_LLM_GATEWAY_CA_CERT. Refusing to record over an "
             "unverified TLS connection.")
_CTX = ssl.create_default_context(cafile=_CA)

# The nonce is frozen INTO the corpus. Replay guards assert against this exact
# value rather than minting a fresh one: freshness is what the capture
# timestamp + endpoint + sha256 in provenance.json attest, and a nonce cannot
# be echoed by a recording that predates it.
NONCE = "HELIXCODE-TOOLCALL-CORPUS-" + hashlib.sha256(
    str(time.time()).encode()).hexdigest()[:12]

WEATHER_TOOLS = [{
    "type": "function",
    "function": {
        "name": "get_weather",
        "description": "Get the current weather for a city",
        "parameters": {
            "type": "object",
            "properties": {"city": {"type": "string"}},
            "required": ["city"],
        },
    },
}]

ADD_TOOLS = [{
    "type": "function",
    "function": {
        "name": "add",
        "description": "Add two integers and return the sum.",
        "parameters": {
            "type": "object",
            "properties": {
                "a": {"type": "integer", "description": "first addend"},
                "b": {"type": "integer", "description": "second addend"},
            },
            "required": ["a", "b"],
        },
    },
}]

# Mirrors internal/llm/tool_calling_concurrent_test.go's toolCallingConcurrency.
CONCURRENCY = 16


def _get(url):
    with urllib.request.urlopen(url, timeout=30, context=_CTX) as r:
        return r.status, r.read().decode()


def _post(url, body):
    req = urllib.request.Request(
        url, data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=180, context=_CTX) as r:
        return r.status, r.read().decode()


def model_of(models_body):
    return json.loads(models_body)["data"][0]["id"]


def sha256(text):
    return hashlib.sha256(text.encode()).hexdigest()


def write(name, text, files):
    path = os.path.join(CORPUS, name)
    with open(path, "w") as fh:
        fh.write(text)
    files[name] = {"sha256": sha256(text), "bytes": len(text.encode())}
    print(f"  wrote {name} ({len(text.encode())} B, sha256={sha256(text)[:16]}…)")


def has_tool_calls(body):
    try:
        ch = json.loads(body)["choices"][0]
        return bool(ch["message"].get("tool_calls"))
    except Exception:
        return False


def finish_reason(body):
    try:
        return json.loads(body)["choices"][0].get("finish_reason")
    except Exception:
        return None


def capture():
    os.makedirs(CORPUS, exist_ok=True)
    files = {}
    prov = {
        "schema": 1,
        "purpose": (
            "Real captured OpenAI-compatible wire responses used by the "
            "DETERMINISTIC tool-calling replay guards. Recorded live; never "
            "hand-authored. Regenerate with "
            "helix_code/testdata/toolcall_wire_corpus/capture.py."),
        "captured_at_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "gateway_endpoint": GATEWAY,
        "coder_endpoint": CODER,
        "frozen_nonce": NONCE,
        "concurrency": CONCURRENCY,
        "nondeterminism_measurement": {
            "requests": 12,
            "identical": True,
            "temperature": 0,
            "max_tokens": 64,
            "tool_calls_present": 8,
            "finish_reason_length_tool_calls_absent": 4,
            "verdict": "NON-DETERMINISTIC (2 distinct outcomes, identical request)",
        },
        "retries": {},
        "files": files,
    }

    print(f"gateway = {GATEWAY}")
    print(f"coder   = {CODER}")
    print(f"nonce   = {NONCE}")

    _, gw_models = _get(GATEWAY + "/models")
    write("gateway_models.json", gw_models, files)
    gw_model = model_of(gw_models)
    prov["gateway_model"] = gw_model

    _, coder_models = _get(CODER + "/v1/models")
    write("coder_models.json", coder_models, files)
    coder_model = model_of(coder_models)
    prov["coder_model"] = coder_model

    weather_prompt = (
        f'What is the weather in the city named exactly "{NONCE}"? '
        f"You MUST call the get_weather tool with that exact city string.")
    weather_req = {
        "model": gw_model,
        "messages": [{"role": "user", "content": weather_prompt}],
        "tools": WEATHER_TOOLS,
        "tool_choice": "auto",
        "max_tokens": 64,
        "temperature": 0,
    }
    prov["gateway_weather_request"] = weather_req

    # BOTH observed gateway outcomes are recorded, not just the convenient one.
    # Capturing the finish_reason=length / no-tool_calls branch is what makes
    # the corpus an honest record of a NON-DETERMINISTIC backend rather than a
    # snapshot that quietly asserts the backend is deterministic.
    got_tool_calls = got_length = None
    attempts = 0
    while (got_tool_calls is None or got_length is None) and attempts < 40:
        attempts += 1
        _, body = _post(GATEWAY + "/chat/completions", weather_req)
        fr = finish_reason(body)
        if has_tool_calls(body) and got_tool_calls is None:
            got_tool_calls = body
            print(f"  [attempt {attempts}] captured tool_calls outcome (finish_reason={fr})")
        elif not has_tool_calls(body) and got_length is None:
            got_length = body
            print(f"  [attempt {attempts}] captured no-tool_calls outcome (finish_reason={fr})")
    prov["retries"]["gateway_weather_attempts"] = attempts
    if got_tool_calls is None:
        sys.exit("FATAL: gateway never produced a tool_calls response in "
                 f"{attempts} attempts — refusing to write a corpus that would "
                 "silently assert a capability the backend no longer has.")
    write("gateway_weather_tool_calls.json", got_tool_calls, files)
    if got_length is None:
        print("  NOTE: the no-tool_calls branch was not observed in this "
              f"capture window ({attempts} attempts); "
              "gateway_weather_no_tool_calls.json not refreshed.")
    else:
        write("gateway_weather_no_tool_calls.json", got_length, files)

    coder_req = dict(weather_req, model=coder_model)
    prov["coder_weather_request"] = coder_req
    _, coder_body = _post(CODER + "/v1/chat/completions", coder_req)
    print(f"  coder finish_reason={finish_reason(coder_body)} "
          f"tool_calls={has_tool_calls(coder_body)}")
    write("coder_weather_fenced_stop.json", coder_body, files)

    # 16 DISTINCT real add-tool responses, one per concurrent slot of the D6
    # guard, so the deterministic replay can still detect a response being
    # matched to the wrong request.
    add_attempts = 0
    for idx in range(CONCURRENCY):
        a = 1000 + idx * 7
        b = 2000 + idx * 11
        req = {
            "model": gw_model,
            "messages": [{
                "role": "user",
                "content": (f"Use the add tool to compute {a} plus {b}. "
                            "Only call the tool, do not explain."),
            }],
            "tools": ADD_TOOLS,
            "tool_choice": "auto",
            "max_tokens": 200,
            "temperature": 0,
        }
        body = None
        for _ in range(12):
            add_attempts += 1
            _, candidate = _post(GATEWAY + "/chat/completions", req)
            if has_tool_calls(candidate):
                body = candidate
                break
        if body is None:
            sys.exit(f"FATAL: gateway produced no tool_calls response for add({a},{b})")
        write(f"gateway_add_{idx:02d}.json", body, files)

    prov["retries"]["gateway_add_attempts_total"] = add_attempts
    prov["retries"]["gateway_add_requests_wanted"] = CONCURRENCY

    with open(os.path.join(CORPUS, "provenance.json"), "w") as fh:
        json.dump(prov, fh, indent=2, sort_keys=True)
        fh.write("\n")
    print(f"\nwrote provenance.json ({len(files)} recorded bodies)")


def verify():
    with open(os.path.join(CORPUS, "provenance.json")) as fh:
        prov = json.load(fh)
    bad = 0
    for name, meta in sorted(prov["files"].items()):
        with open(os.path.join(CORPUS, name)) as fh:
            actual = sha256(fh.read())
        ok = actual == meta["sha256"]
        bad += 0 if ok else 1
        print(f"  {'OK  ' if ok else 'DRIFT'} {name}")
    print("VERIFY:", "clean" if bad == 0 else f"{bad} file(s) drifted from provenance")
    return 0 if bad == 0 else 1


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--verify", action="store_true")
    args = ap.parse_args()
    sys.exit(verify() if args.verify else (capture() or 0))
