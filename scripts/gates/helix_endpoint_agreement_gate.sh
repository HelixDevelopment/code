#!/usr/bin/env bash
# =============================================================================
# CM-HELIX-ENDPOINT-AGREEMENT
#
# Guards the endpoint-DRIFT class: a configured Helix service endpoint that
# points at a port nothing serves, while the service itself is alive on a
# different port — and nothing anywhere keeps the several records of that one
# fact equal to each other.
#
# THE DEFECT (measured 2026-09-02, before the fix)
# ------------------------------------------------
# `claude-providers list` showed 21 verified providers and NOT ONE HelixAgent or
# HelixLLM entry. The Claude Toolkit's three Helix provider definitions named
# ports nothing listened on:
#
#     configured                                    measured
#     helixagent         http://127.0.0.1:18434/v1  curl -> 000
#     helixagent-native  http://127.0.0.1:18435     curl -> 000
#     helixllm-gateway   http://127.0.0.1:18435/v1  curl -> 000
#
# while `ss -ltnp` showed helixagent on :7061 and helixllm on :8443. The two
# configured ports were not even invented — :18434 is the llama.cpp CODER
# container and :18435 the TEI embeddings container, so each row named a REAL
# port belonging to a DIFFERENT service. helixllm-gateway was doubly wrong:
# wrong port AND wrong scheme (plain http against the TLS :8443 answers
# "400 Client sent an HTTP request to an HTTPS server").
#
# WHY IT WAS INVISIBLE — the part this gate exists to fix
# -------------------------------------------------------
# `claude-providers list` filters to verified-only. Verification could not open
# a connection, so no `*_verified.json` was ever written, so the providers
# simply VANISHED from the operator's view. Nothing failed loudly. A dead
# endpoint and a provider that was never configured at all are indistinguishable
# in that output — the §11.4.1 absence-of-error PASS, in the one place an
# operator looks to answer "is my model reachable".
#
# It was DRIFT, not ignorance. The rest of the tree already knew the right
# ports (`scripts/systemd/helixagent.service` declares :8111, the agentic
# challenge drives :7061, `helixllm-gateway.service` declares :8443) — only the
# toolkit's rows carried the wrong numbers. One fact, recorded in several
# places, with no mechanism holding the copies equal. That is the gap.
#
# WHAT THIS GUARD ASSERTS — TWO INDEPENDENT HALVES
# ------------------------------------------------
# (A) AGREEMENT (static; runs with every service DOWN). For each declared
#     service, every record of its endpoint across the tracked sources must
#     name the SAME host:port. Two sources disagreeing IS the drift, and it is
#     checkable on a laptop with nothing running. This half alone would have
#     caught the defect above: the toolkit said helixagent=:18434 while the
#     tracked unit and challenge said :8111/:7061.
#
# (B) LIVENESS CORRESPONDENCE (runtime; three-state). For each declared
#     endpoint, does it actually reach the service it names?
#
# WHY THE PASS/SKIP/FAIL LINES SIT WHERE THEY DO
# ----------------------------------------------
# A naive "curl it and require 200" gate would be WRONG twice over, and a gate
# that cries wolf gets disabled — taking the real signal with it (§11.4.201:
# a false-positive refusal is as forbidden as a false pass).
#
#   (a) SERVICE NOT RUNNING AT ALL  -> SKIP (§11.4.3), never FAIL.
#       A developer machine legitimately runs none of this. The coder container
#       is on-demand by design. Failing because a service is not started is the
#       false refusal, so absence is keyed to PROVABLE absence: the service's
#       owner — derived from its unit's ExecStart, not guessed — is not running
#       and nothing owned answers on the port.
#
#   (b) CONFIGURED ENDPOINT REACHES THE SERVICE -> PASS.
#       Note carefully: reaching it is the whole assertion. The gateway's
#       /internal/health answered 503 when this gate was written, because the
#       coder it depends on is down — and that is a CORRECTLY WIRED endpoint
#       reporting an honest degraded state. Requiring 200, or "healthy", would
#       red-line a correct configuration. Service HEALTH is G30-G32's job; this
#       gate asks only whether the wire lands on the right service, so a 503
#       carrying a well-formed health envelope PASSES.
#
#   (c) SERVICE UP, CONFIGURED ENDPOINT DOES NOT REACH IT, ANOTHER PORT DOES
#       -> FAIL, naming BOTH the configured value and the observed one.
#       This is the actual defect and the only state that goes red.
#
# NO EXPECTED PORT IS PINNED TO A LITERAL. :7061 and :8443 appear nowhere below
# except in this header's forensic record. Expectations are derived from the
# tracked declarations plus what is observably listening, so deliberately
# moving a service cannot false-FAIL this gate; it fails only on DISAGREEMENT.
# Hardcoding the answer would merely relocate the drift into the guard.
#
# STATUS CODE IS NOT ENOUGH — BODY SHAPE IS ASSERTED
# --------------------------------------------------
# Two ports on this host serve SPAs that return 200 for ANY path (:4096
# OpenCode, :7187 a dashboard), so a bare status-code check produces false
# POSITIVES — "something answered, ship it". Each probe therefore asserts the
# response ENVELOPE matches the protocol the URL implies (an OpenAI model list
# has object="list" + data[]; a health endpoint has a status field), and an
# HTML body is an outright FAIL: it means some other service answered.
#
# PROCESS OWNERSHIP IS VERIFIED (§11.4.174)
# -----------------------------------------
# This is a SHARED host with unrelated workloads (helixterm-*, penpot-*,
# qbittorrent/jackett, a `kfl` process). A port being open does not make it
# ours. Every listener is attributed only if /proc/<pid>/exe or cwd resolves
# INSIDE this repository, so a foreign process cannot satisfy this gate and a
# foreign port cannot be blamed on us.
#
# POLARITY (§11.4.115) — one source, two roles
#   RED_MODE=1  Reproduce the defect on a synthesized PRE-FIX source set
#               (copies only; the real tree is never touched) and assert this
#               guard FAILs on it. Proves the guard is not a blind test.
#   RED_MODE=0  DEFAULT. The standing GREEN regression guard.
#
# EXIT CODES  (the G30-G32 live-service contract)
#   0  GREEN — a verdict was reached and the endpoints agree / correspond
#   1  FAIL  — records contradict each other, or an endpoint misses a live
#              service that answers elsewhere (a regression)
#   2  SKIP  — nothing could be certified (no sources, no python3). Counted as
#              neither PASS nor failure, always printed with its reason.
# =============================================================================
set -uo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
GATE="CM-HELIX-ENDPOINT-AGREEMENT"
RED_MODE="${RED_MODE:-0}"

# The service-declaration layer: one unit per service. Tracked, so this
# vocabulary is stable with every service down.
UNIT_DIR="${UNIT_DIR_OVERRIDE:-$ROOT/scripts/systemd}"

# Optional EXTERNAL source: the Claude Toolkit's provider definitions, the site
# of the forensic defect. $HOME-relative, never a hardcoded user path
# (§11.4.177 — shared tooling must not hardcode one checkout). Absent is
# REPORTED, never silently passed over.
TOOLKIT_DIR="${HELIX_TOOLKIT_DIR:-$HOME/Projects/claude_toolkit}"

# Additional same-line sources, colon-separated, overridable.
EXTRA_SOURCES="${HELIX_ENDPOINT_SOURCES:-$ROOT/constitution/scripts/helix_code/helix_code_services.sh:$ROOT/submodules/challenges/challenges/scripts/agentic_subagents_challenge.sh}"

echo "$GATE  RED_MODE=$RED_MODE"

command -v python3 >/dev/null 2>&1 || {
  echo "$GATE: SKIP — python3 not on PATH; the record parser cannot run" >&2; exit 2; }
[[ -d "$UNIT_DIR" ]] || {
  echo "$GATE: SKIP — service-declaration dir absent ($UNIT_DIR); no service vocabulary to check" >&2; exit 2; }

# --- RED_MODE: §11.4.115 reproduce-the-defect-on-a-broken-artifact -----------
# Synthesize the PRE-FIX toolkit provider set on COPIES and assert this same
# checker FAILs on it. The mutation is the exact historical shape — the three
# rows pointing at :18434/:18435 — not an arbitrary break.
if [[ "$RED_MODE" == "1" ]]; then
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  mkdir -p "$TMP/toolkit/scripts/providers"
  cat >"$TMP/toolkit/scripts/providers/helixagent.json" <<'EOF'
{ "bin": "helixagent", "id": "helixagent",
  "base_url": "http://127.0.0.1:18434/v1", "transport": "router" }
EOF
  cat >"$TMP/toolkit/scripts/providers/helixllm-gateway.json" <<'EOF'
{ "bin": "helixllm", "id": "helixllm-gateway",
  "base_url": "http://127.0.0.1:18435/v1", "transport": "router" }
EOF
  if RED_MODE=0 HELIX_TOOLKIT_DIR="$TMP/toolkit" "$0" >"$TMP/red.out" 2>&1; then
    echo "$GATE: RED FAIL — the guard PASSed on the pre-fix provider set (:18434/:18435 for services that serve elsewhere). It is a blind test." >&2
    sed 's/^/    /' "$TMP/red.out" >&2
    exit 1
  fi
  if ! grep -q 'CONTRADICTION\|does not reach' "$TMP/red.out"; then
    echo "$GATE: RED FAIL — the guard FAILed on the pre-fix set, but for neither of the reasons it exists to detect (no contradiction and no unreachable-endpoint finding). Output:" >&2
    sed 's/^/    /' "$TMP/red.out" >&2
    exit 1
  fi
  echo "$GATE: RED OK — guard FAILs on the pre-fix provider set, citing the drift itself:"
  grep -E 'CONTRADICTION|does not reach' "$TMP/red.out" | sed 's/^/    /'
  exit 0
fi

# --- The analyzer -----------------------------------------------------------
# Exits: 0 green, 3 FAIL (a real finding), 4 SKIP (nothing certifiable).
# Anything else means the INSTRUMENT broke, and a broken instrument reports
# nothing about the subject (§11.4.107(10)) — handled as a hard FAIL below,
# deliberately NOT overlapping the finding codes.
VERDICT="$(ROOT="$ROOT" UNIT_DIR="$UNIT_DIR" TOOLKIT_DIR="$TOOLKIT_DIR" \
           EXTRA_SOURCES="$EXTRA_SOURCES" python3 - <<'PY' 2>&1
import os, re, json, glob, ssl, socket, sys
import urllib.request, urllib.error

ROOT     = os.environ["ROOT"]
UNIT_DIR = os.environ["UNIT_DIR"]
TOOLKIT  = os.environ["TOOLKIT_DIR"]
EXTRA    = [p for p in os.environ["EXTRA_SOURCES"].split(":") if p]

def norm(s):
    """Collapse to comparable identity: lowercase, alphanumerics only.
    'HelixLLM gateway' and 'helixllm-gateway' must read as one name."""
    return re.sub(r'[^a-z0-9]', '', s.lower())

# ---------------------------------------------------------------------------
# 1. SERVICE VOCABULARY + OWNER, both derived from the tracked unit files.
#    The owner is what makes the (a)-vs-(c) call possible. Deriving it from
#    ExecStart rather than matching name prefixes is load-bearing: the process
#    named `helixllm` is the GATEWAY, and a prefix match would also claim
#    `helixllm-coder` (a podman container), turning that container's honest
#    absence into a fabricated "wrong port" FAIL.
# ---------------------------------------------------------------------------
services = {}   # norm(name) -> {name, owner_kind, owner_path}
for unit in sorted(glob.glob(os.path.join(UNIT_DIR, "*.service"))):
    name = os.path.basename(unit)[:-len(".service")]
    try:
        text = open(unit, encoding="utf-8", errors="replace").read()
    except OSError:
        continue
    m = re.search(r'^ExecStart=(.*)$', text, re.M)
    execline = (m.group(1) if m else "").strip()
    if re.search(r'podman|docker|compose|boot_coder', execline):
        kind, path = "container", name
    else:
        # Last absolute-ish token that looks like an executable path.
        toks = [t for t in execline.split() if "/" in t and not t.endswith(".yml")]
        real = [t for t in toks if not t.endswith(".sh")] or toks
        path = real[-1].replace("@HELIX_ROOT@", ROOT) if real else ""
        if "@" in path or not path:
            kind, path = "unknown", path      # e.g. @HELIXLLM_BIN@
        else:
            kind = "binary"
    services[norm(name)] = {"name": name, "owner_kind": kind, "owner_path": path,
                            "unit": unit, "execline": execline}

if not services:
    print("SKIP|no service declarations found in %s" % UNIT_DIR); sys.exit(4)

# ---------------------------------------------------------------------------
# 2. ENDPOINT RECORDS.  A record is (service, scheme, host, port, file, line).
#    Attribution rules, chosen so nothing is ever guessed (§11.4.6):
#      * SAME-LINE   — the line carrying the endpoint also names the service.
#      * FILE-IDENTITY — only for single-endpoint provider definitions (one
#        object, one base_url, an id/bin field). A multi-endpoint config file
#        is NEVER attributed by its path: config/llmsverifier/config.yaml
#        records the llamacpp CODER's endpoint, and blaming that on the
#        verifier would invent a contradiction that does not exist.
# ---------------------------------------------------------------------------
# `{` and `}` are excluded from the path class on purpose: these records live in
# shell defaults like ${HELIXAGENT_ENDPOINT:-http://host:7061/v1/chat/completions}
# and swallowing the closing brace made the probe request a path with a stray
# `}`, which answered 404 and was reported as a drifted endpoint. The parser's
# own artefact must never become a finding (§11.4.201).
URL_RE  = re.compile(r'''(https?)://([A-Za-z0-9_.-]+):(\d+)(/[^\s"'\),;{}]*)?''')
PAREN_RE = re.compile(r'\(:(\d+)\)')     # the `HelixLLM gateway (:8443)` shape

def role_of(upath):
    """One service legitimately serves SEVERAL endpoints — helixagent answers
    its OpenAI API on one port and its liveness probe on another — so
    agreement must compare like with like. Comparing per SERVICE alone flagged
    that correct arrangement as drift (:7061 vs :8111), which is the
    cries-wolf failure this gate must not have. Records are therefore grouped
    by (service, ROLE), and only same-role records can contradict."""
    p = (upath or "").rstrip("/")
    if not p:
        return "base"
    low = p.lower()
    if "health" in low or "ready" in low:
        return "health"
    if low.startswith("/v1"):
        return "api-v1"
    return p.split("/")[1] if "/" in p[1:] else low

records = []
def add(svc, scheme, host, port, upath, path, line_no, why):
    records.append({"svc": svc, "scheme": scheme, "host": host, "port": int(port),
                    "upath": upath or "", "role": role_of(upath),
                    "file": path, "line": line_no, "attr": why})

def scan_same_line(path):
    try:
        lines = open(path, encoding="utf-8", errors="replace").read().splitlines()
    except OSError:
        return False
    for i, line in enumerate(lines, 1):
        if line.lstrip().startswith("#"):
            continue
        nline = norm(line)
        hit = None
        # Longest name first, so `helixllm-gateway` wins over any shorter name
        # that happens to be a substring of the same line.
        for key in sorted(services, key=len, reverse=True):
            if key in nline:
                hit = key
                break
        if not hit:
            continue
        for scheme, host, port, upath in URL_RE.findall(line):
            add(hit, scheme, host, port, upath, path, i, "same-line")
        for port in PAREN_RE.findall(line):
            add(hit, "", "localhost", port, "", path, i, "same-line")
    return True

def scan_provider_json(path):
    """Single-endpoint provider definition: attribute by its own identity."""
    try:
        doc = json.load(open(path, encoding="utf-8", errors="replace"))
    except Exception:
        return
    if not isinstance(doc, dict):
        return
    urls = [v for k, v in doc.items()
            if isinstance(v, str) and re.match(r'https?://', v)
            and ("url" in k.lower() or "endpoint" in k.lower())]
    if len(urls) != 1:
        return                        # not the single-endpoint shape; skip
    # EXACT normalized identity only — never a substring. `helixagent-native`
    # is its own provider id, not the `helixagent` service, and claiming it
    # under that service asserted a mapping this guard cannot actually
    # establish (§11.4.6 no-guessing). A provider row naming no declared
    # service is left UNATTRIBUTED and out of scope, which is honest.
    ident = {norm(str(doc.get(k))) for k in ("id", "bin", "name") if doc.get(k)}
    ident.add(norm(os.path.basename(path).rsplit(".", 1)[0]))
    hit = next((k for k in services if k in ident), None)
    if not hit:
        return
    m = URL_RE.match(urls[0])
    if m:
        add(hit, m.group(1), m.group(2), m.group(3), m.group(4), path, 0, "file-identity")

sources_seen, sources_absent = [], []

# The units themselves. ONLY the readiness probe counts as the service's own
# address: `ExecStartPost=.../wait-http-ready.sh <URL>` is systemd waiting for
# THIS service to answer, so that URL is definitionally its own.
#
# `Environment=..._URL=` lines are deliberately EXCLUDED. A unit records its
# DEPENDENCIES' endpoints too — helixllm-gateway.service:59 carries
# `HELIX_LLM_VERIFIER_URL=http://localhost:8100`, the VERIFIER's address — and
# reading every URL in the file as the unit's own manufactured a contradiction
# (:8100 vs :8443) out of a perfectly correct unit. A guard that invents drift
# is the §11.4.201 false refusal.
READY_RE = re.compile(r'^ExecStartPost=.*wait-http-ready\.sh\s+(\S+)')
for key, s in services.items():
    text = open(s["unit"], encoding="utf-8", errors="replace").read()
    for line_no, line in enumerate(text.splitlines(), 1):
        if line.lstrip().startswith("#"):
            continue
        rm = READY_RE.match(line.strip())
        if not rm:
            continue
        # The unit's own readiness BUDGET: wait-http-ready.sh <url> <retries>
        # <interval>. systemd waits this long for the service to answer, so
        # the gate must not call the endpoint drifted any sooner.
        bits = line.strip().split()
        try:
            s["ready_budget"] = max(s.get("ready_budget", 0),
                                    int(bits[-2]) * int(bits[-1]))
        except (ValueError, IndexError):
            pass
        for scheme, host, port, upath in URL_RE.findall(rm.group(1)):
            add(key, scheme, host, port, upath, s["unit"], line_no, "unit-declaration")
    sources_seen.append(s["unit"])

for p in EXTRA:
    (sources_seen if scan_same_line(p) else sources_absent).append(p)

prov_glob = os.path.join(TOOLKIT, "scripts", "providers", "*.json")
prov = sorted(glob.glob(prov_glob))
if prov:
    for p in prov:
        scan_provider_json(p)
        sources_seen.append(p)
else:
    sources_absent.append(prov_glob)

if not records:
    print("SKIP|no endpoint records found across %d source(s)" % len(sources_seen))
    sys.exit(4)

# ---------------------------------------------------------------------------
# 3. OBSERVED LISTENERS, ownership-verified (§11.4.174).
#    A listener counts as OURS only if its exe or cwd resolves inside this
#    repository. This host runs unrelated workloads; an open port proves
#    nothing about whose it is.
# ---------------------------------------------------------------------------
owned = []      # {port, pid, exe, cwd}
def read_listeners():
    out = []
    # BOTH families. Every Helix listener on this host binds the IPv6 wildcard
    # (`*:7061` in ss), so reading only /proc/net/tcp finds NOTHING and every
    # endpoint silently degrades to SKIP — a fail-open the §11.4.69
    # CM-NO-FAIL-OPEN-SKIP rule forbids. Measured: 17 LISTEN rows in tcp, 49 in tcp6.
    for proto in ("tcp", "tcp6"):
        try:
            data = open("/proc/net/%s" % proto, encoding="utf-8").read().splitlines()[1:]
        except OSError:
            return out
        # Map inode -> port for sockets in LISTEN state (0A).
        inodes = {}
        for row in data:
            f = row.split()
            if len(f) < 10 or f[3] != "0A":
                continue
            try:
                inodes[f[9]] = int(f[1].split(":")[1], 16)
            except (ValueError, IndexError):
                continue
        if not inodes:
            return out
        for fd_dir in glob.glob("/proc/[0-9]*/fd/*"):
            try:
                tgt = os.readlink(fd_dir)
            except OSError:
                continue
            m = re.match(r'socket:\[(\d+)\]', tgt)
            if not m or m.group(1) not in inodes:
                continue
            pid = fd_dir.split("/")[2]
            try:
                exe = os.readlink("/proc/%s/exe" % pid)
            except OSError:
                exe = ""
            try:
                cwd = os.readlink("/proc/%s/cwd" % pid)
            except OSError:
                cwd = ""
            out.append({"port": inodes[m.group(1)], "pid": pid, "exe": exe, "cwd": cwd})
    return out

for l in read_listeners():
    inside = (l["exe"].startswith(ROOT + os.sep) or l["cwd"].startswith(ROOT + os.sep))
    if inside:
        owned.append(l)

owned_ports = {l["port"] for l in owned}

# Every process of ours, listening or not. Needed to tell "the service is not
# running" (absence) from "the service is running but has bound nothing yet",
# which are different states and must not share one verdict.
def owned_processes():
    out = []
    for pdir in glob.glob("/proc/[0-9]*"):
        try:
            exe = os.readlink(os.path.join(pdir, "exe"))
        except OSError:
            continue
        if exe.startswith(ROOT + os.sep):
            out.append({"pid": os.path.basename(pdir), "exe": exe})
    return out

OURS = owned_processes()

# A unit whose ExecStart is a build-time template (@HELIXLLM_BIN@) yields no
# owner path. Rather than guess, resolve it against our OWN running processes:
# the service's distinguishing token must match exactly ONE of them, and
# container-owned services are excluded from the contest (so `helixllm-coder`,
# a podman container, can never be confused with the `helixllm` binary that
# owns the gateway). Still ambiguous => stays unknown and is reported as such.
for key, s in services.items():
    if s["owner_kind"] != "unknown":
        continue
    tok = norm(s["name"]).replace("gateway", "").replace("server", "")
    cands = [p for p in OURS if tok and tok in norm(p["exe"])]
    if len(cands) == 1:
        s["owner_kind"], s["owner_path"] = "binary", cands[0]["exe"]
        s["owner_note"] = "resolved from a running process (unit ExecStart is templated)"

def service_owner_pids(svc):
    """PIDs of THIS service's owner. Exact exe-path match for binaries — never
    a name prefix — so sibling services are never confused."""
    s = services[svc]
    if s["owner_kind"] == "binary" and s["owner_path"]:
        want = os.path.realpath(s["owner_path"])
        return [p["pid"] for p in OURS
                if os.path.realpath(p["exe"]) == want]
    return []

def service_listeners(svc):
    """Ports held by THIS service's owner."""
    pids = set(service_owner_pids(svc))
    if not pids:
        return []
    return sorted({l["port"] for l in owned if l["pid"] in pids})

# ---------------------------------------------------------------------------
# 4. HALF (A): AGREEMENT. Runs with everything down.
# ---------------------------------------------------------------------------
findings, notes = [], []
by_role = {}
for r in records:
    by_role.setdefault((r["svc"], r["role"]), []).append(r)

def cite(r):
    f = os.path.relpath(r["file"], ROOT) if r["file"].startswith(ROOT) else r["file"]
    return "%s:%s=%s:%d%s" % (f, r["line"] or "-", r["host"], r["port"], r["upath"])

agreed = {}
for (svc, role), rs in sorted(by_role.items()):
    # localhost and 127.0.0.1 are the same endpoint, so compare on PORT and
    # surface the host spelling only for context.
    ports = {r["port"] for r in rs}
    if len(ports) > 1:
        findings.append("CONTRADICTION for '%s' (%s endpoint): %d different ports recorded for one "
                        "endpoint (%s). One fact, several records, nothing holding them equal — "
                        "this is the drift."
                        % (services[svc]["name"], role, len(ports),
                           "; ".join(cite(r) for r in sorted(rs, key=lambda r: (r["port"], r["file"])))))
        continue
    agreed[(svc, role)] = rs

# ---------------------------------------------------------------------------
# 5. HALF (B): LIVENESS CORRESPONDENCE, three-state.
# ---------------------------------------------------------------------------
CTX = ssl.create_default_context()
CTX.check_hostname = False
CTX.verify_mode = ssl.CERT_NONE
# Bypass any proxy: without this an unreachable PROXY reads as an unreachable
# TARGET, and the SKIP branch below depends entirely on the failure being a
# fact about the target.
OPENER = urllib.request.build_opener(
    urllib.request.ProxyHandler({}),
    urllib.request.HTTPSHandler(context=CTX))

def probe(scheme, host, port, path):
    url = "%s://%s:%d%s" % (scheme or "http", host, port, path)
    try:
        with OPENER.open(url, timeout=10) as resp:
            return resp.status, resp.read(4096).decode("utf-8", "replace"), url, ""
    except urllib.error.HTTPError as e:
        # A 4xx/5xx is a real answer and must be inspected, not discarded:
        # the gateway answers 503 while correctly wired.
        return e.code, (e.read(4096).decode("utf-8", "replace") if e.fp else ""), url, ""
    except Exception as e:
        return None, "", url, "%s: %s" % (type(e).__name__, e)

def envelope_ok(role, path, body):
    """Assert the response SHAPE for roles that have a defined protocol.

    Body shape is a SECOND line of defence here, not the first: ownership is
    already proven from /proc, which defeats the same-port-different-service
    trap more strongly than any heuristic. Shape is what catches a port that
    our own process holds while answering the wrong protocol.

    The `base` role asserts NO shape. A service's root URL legitimately serves
    an HTML console — helixcode does exactly that on :8080 — and demanding
    JSON there reported a correctly-wired service as "another service holds
    this port" (§11.4.201 false refusal)."""
    if role not in ("api-v1", "health"):
        return True, "reachable (no protocol asserted for the '%s' role)" % role
    head = body.lstrip()[:200].lower()
    if head.startswith("<!doctype html") or head.startswith("<html"):
        return False, ("answered with an HTML page where %s is expected — "
                       "another service is answering on this port" % role)
    try:
        doc = json.loads(body)
    except Exception:
        return False, "body is not JSON (%r...)" % body[:60]
    if not isinstance(doc, dict):
        return False, "body is JSON but not an object"
    if role == "api-v1":
        if doc.get("object") == "list" and isinstance(doc.get("data"), list):
            return True, "OpenAI model list, %d model(s)" % len(doc["data"])
        return False, "not an OpenAI model list (no object=list + data[])"
    if "status" in doc:
        return True, "health envelope, status=%r" % doc.get("status")
    return False, "not a health envelope (no status field)"

def probe_path(role, recorded):
    """GET-safe path for the role. An api-v1 record often points at
    /v1/chat/completions, which is POST-only and answers 404/405 to a GET —
    so probing it verbatim manufactures a finding. /v1/models is the OpenAI
    discovery endpoint and is precisely what provider verification calls, i.e.
    the exact request whose silent failure hid the forensic defect."""
    if role == "api-v1":
        return "/v1/models"
    return recorded or "/"

# A service's ports do not all appear at once. helixagent binds its liveness
# probe BEFORE its API port, so during startup the API role looks exactly like
# drift: configured port dead, service alive on another port. Measured live —
# the gate reported :7061 as drifted while the service's OWN health endpoint
# said status='starting' and :7061 came up moments later.
#
# So consult that declared readiness signal before calling drift. The unit
# itself treats this endpoint as the readiness gate (ExecStartPost waits on
# it), which is what makes it authoritative rather than a guess (§11.4.6).
# The transient set is CLOSED and deliberately narrow: 'unhealthy' or
# 'degraded' is NOT a startup excuse — a running service that lost its API
# port is a real finding and must stay red.
STARTING = {"starting", "initializing", "init", "pending", "booting",
            "warmup", "warming_up", "not_ready", "notready", "unavailable"}

try:
    CLK = os.sysconf("SC_CLK_TCK") or 100
except (ValueError, OSError):
    CLK = 100

def proc_age(pid):
    """Seconds since this process started, from /proc. Measured, not guessed."""
    try:
        with open("/proc/uptime") as f:
            up = float(f.read().split()[0])
        with open("/proc/%s/stat" % pid) as f:
            # Field 22 (1-based) is starttime; the comm field may contain
            # spaces/parens, so split after the closing paren.
            rest = f.read().rsplit(")", 1)[1].split()
        return up - (float(rest[19]) / CLK)
    except (OSError, ValueError, IndexError):
        return None

def within_ready_budget(svc):
    """Is the owner younger than the readiness budget its OWN unit declares?
    helixagent's unit waits 300x3s=900s for its API port, so calling that port
    drifted at second 30 would contradict the project's own definition of how
    long startup may take. Returns (age, budget) when inside the budget."""
    budget = services[svc].get("ready_budget", 0)
    if budget <= 0:
        return None
    ages = [a for a in (proc_age(p) for p in service_owner_pids(svc)) if a is not None]
    if not ages:
        return None
    age = min(ages)
    return (age, budget) if age < budget else None

def starting_up(svc):
    """Is this service's own readiness endpoint telling us it is still coming
    up? Returns the reported status when yes, else None."""
    for r in by_role.get((svc, "health"), []):
        if r["port"] not in owned_ports:
            continue
        status, body, _url, err = probe(r["scheme"] or "http", r["host"],
                                        r["port"], r["upath"] or "/")
        if status is None:
            continue
        try:
            doc = json.loads(body)
        except Exception:
            continue
        if isinstance(doc, dict):
            st = str(doc.get("status", "")).strip().lower()
            if st in STARTING:
                return st
    return None

green, skips = [], []
for (svc, role), rs in sorted(agreed.items()):
    sname = services[svc]["name"]
    live  = service_listeners(svc)
    host  = rs[0]["host"]
    port  = rs[0]["port"]
    # Probe the path and scheme the records themselves carry, so the request
    # speaks the protocol this endpoint actually serves. A unit's own readiness
    # declaration is preferred when present — it is the service's own word.
    pref = next((r for r in rs if r["attr"] == "unit-declaration" and r["upath"]),
                next((r for r in rs if r["upath"]), rs[0]))
    path   = probe_path(role, pref["upath"])
    scheme = next((r["scheme"] for r in rs if r["scheme"]), "http")
    owner  = os.path.basename(services[svc]["owner_path"] or "?")

    if port in owned_ports:
        # (b) An OWNED listener holds the configured port. Ownership is proven
        # from /proc, so this IS our service; now check it speaks the protocol.
        status, body, url, err = probe(scheme, host, port, path)
        if status is None:
            findings.append("'%s' (%s): %s is held by one of OUR processes but the probe failed "
                            "(%s). Something owned accepted the port and would not answer."
                            % (sname, role, url, err))
            continue
        ok, why = envelope_ok(role, path, body)
        if ok:
            green.append("%s [%s] -> %s://%s:%d%s (HTTP %s, %s)"
                         % (sname, role, scheme, host, port, path, status, why))
        else:
            findings.append("'%s' (%s): the configured endpoint %s answered (HTTP %s) but %s."
                            % (sname, role, url, status, why))
    elif live and within_ready_budget(svc):
        age, budget = within_ready_budget(svc)
        skips.append("%s [%s] (:%d) — owner %s (pid %s) has been up %.0fs, inside the %ds "
                     "readiness budget its own unit declares (wait-http-ready retries x "
                     "interval), and has not bound this port yet; a service still inside its "
                     "declared startup window has not drifted"
                     % (sname, role, port, owner, ",".join(service_owner_pids(svc)),
                        age, budget))
    elif live and starting_up(svc):
        # Alive and serving something, but its own readiness endpoint says it
        # is still coming up, so this port is expected to appear shortly. Not
        # drift — and calling it drift would red-line every restart window,
        # which with several agents restarting services means a permanently
        # red gate that gets muted (§11.4.201).
        skips.append("%s [%s] (:%d) — owner %s (pid %s) is still STARTING "
                     "(its own readiness endpoint reports status=%r) and has not bound this "
                     "port yet; a service mid-startup has not drifted"
                     % (sname, role, port, owner, ",".join(service_owner_pids(svc)),
                        starting_up(svc)))
    elif live:
        # (c) THE DEFECT. The service is alive, serving, and reports itself
        # READY — it is simply not where the records say. Both values are named
        # so the fix is unambiguous.
        findings.append("'%s' (%s): the configured endpoint is :%d, which NOTHING is listening on, "
                        "while this service's own process (%s, pid %s) is alive and serving :%s. "
                        "The configured value does not reach the running service. Recorded at: %s"
                        % (sname, role, port, owner, ",".join(service_owner_pids(svc)),
                           ",".join(str(p) for p in live), "; ".join(cite(r) for r in rs)))
    elif service_owner_pids(svc):
        # The owner is RUNNING but holds no port at all — starting up, or failed
        # to bind. That is a HEALTH question (G30-G32), not a drift question:
        # with no other port serving, there is nothing to disagree with. Calling
        # it drift would red-line every restart window.
        skips.append("%s [%s] (:%d) — owner %s (pid %s) is running but has bound NO port yet "
                     "(starting up, or failed to bind); nothing to compare against, so no drift "
                     "verdict is possible. Service health is G30-G32's question."
                     % (sname, role, port, owner, ",".join(service_owner_pids(svc))))
    else:
        # (a) Provable absence. Never a FAIL: a developer host legitimately runs
        # none of this, and the coder container is on-demand by design.
        kind = services[svc]["owner_kind"]
        skips.append("%s [%s] (:%d) — owner (%s%s) is not running and nothing owned holds the port; "
                     "an absent service has not drifted"
                     % (sname, role, port, kind,
                        "" if kind != "binary" else " " + owner))

for p in sources_absent:
    notes.append("source not present on this host: %s" % p)

print("SOURCES|%d present, %d absent" % (len(sources_seen), len(sources_absent)))
for n in notes:
    print("NOTE|%s" % n)
for g in green:
    print("GREEN|%s" % g)
for s in skips:
    print("SKIPPED|%s" % s)
for f in findings:
    print("FINDING|%s" % f)
print("TALLY|green=%d skipped=%d findings=%d records=%d services=%d"
      % (len(green), len(skips), len(findings), len(records), len(agreed)))

if findings:
    sys.exit(3)
if not green and not skips:
    print("SKIP|no endpoint reached a decidable state")
    sys.exit(4)
sys.exit(0)
PY
)"
ANALYZER_RC=$?

# The analyzer must not be able to bluff by CRASHING. Its findings are 3 and 4;
# a Python traceback exits 1, and treating that 1 as "defect found" would make
# a SyntaxError read as a regression against every input (§11.4.107(10)).
if [[ "$ANALYZER_RC" -ne 0 && "$ANALYZER_RC" -ne 3 && "$ANALYZER_RC" -ne 4 ]]; then
  echo "$GATE: FAIL — ANALYZER CRASHED (exit $ANALYZER_RC). This is a defect in the guard, not a verdict about the endpoints." >&2
  printf '%s\n' "$VERDICT" | sed 's/^/    /' >&2
  exit 1
fi

printf '%s\n' "$VERDICT" | grep -E '^(SOURCES|NOTE|GREEN|SKIPPED)\|' | sed 's/|/: /' | sed 's/^/  /'

if [[ "$ANALYZER_RC" -eq 4 ]]; then
  echo "$GATE: SKIP — $(printf '%s' "$VERDICT" | grep -m1 '^SKIP|' | cut -d'|' -f2-)" >&2
  exit 2
fi

if [[ "$ANALYZER_RC" -eq 3 ]]; then
  printf '%s\n' "$VERDICT" | grep '^FINDING|' | cut -d'|' -f2- | sed 's/^/  FAIL: /' >&2
  echo "$GATE: FAIL — $(printf '%s' "$VERDICT" | grep -c '^FINDING|') endpoint finding(s); $(printf '%s' "$VERDICT" | grep -m1 '^TALLY|' | cut -d'|' -f2-)" >&2
  exit 1
fi

echo "$GATE: PASS — $(printf '%s' "$VERDICT" | grep -m1 '^TALLY|' | cut -d'|' -f2-); every recorded endpoint agrees across sources and reaches its service (body shape verified, ownership confirmed)"
exit 0
