#!/usr/bin/env python3
"""Diagnostic shim: flattens Anthropic `system` block-arrays to a plain string.

PURPOSE (diagnosis only, NOT a proposed deliverable): the HelixCode Anthropic
facade declares `System string` (wire_facade.go:290), so Claude Code's
block-array `system` is rejected with HTTP 400. This shim sits between Claude
Code and the facade and performs ONLY that one translation, so we can measure
what — if anything — still blocks Claude Code AFTER that single defect is
fixed. The real fix belongs in wire_facade.go (a custom UnmarshalJSON on the
System field, exactly mirroring the existing openAIMessageContent pattern in
the same file); this shim is not a shipping artefact.

It is otherwise a byte-faithful pass-through: same method, path+query, body,
and all headers except Host/Content-Length; streaming responses are relayed
chunk-by-chunk unbuffered.
"""
import http.server
import json
import os
import sys
import urllib.request
import urllib.error

UPSTREAM = os.environ["SHIM_UPSTREAM"].rstrip("/")
LOGPATH = os.environ.get("SHIM_LOG", "")

HOP = {"host", "content-length", "connection", "accept-encoding"}


def log(msg):
    if LOGPATH:
        with open(LOGPATH, "a") as fh:
            fh.write(msg + "\n")


def flatten_system(body: bytes) -> bytes:
    try:
        doc = json.loads(body)
    except Exception:
        return body
    sysval = doc.get("system")
    if isinstance(sysval, list):
        parts = [b.get("text", "") for b in sysval if isinstance(b, dict) and b.get("type") == "text"]
        doc["system"] = "\n\n".join(p for p in parts if p)
        log(f"  [shim] flattened system: {len(sysval)} block(s) -> {len(doc['system'])} chars")
        return json.dumps(doc).encode()
    return body


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def _proxy(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n) if n else None
        if body and self.path.startswith("/v1/messages"):
            body = flatten_system(body)
        hdrs = {k: v for k, v in self.headers.items() if k.lower() not in HOP}
        req = urllib.request.Request(UPSTREAM + self.path, data=body, headers=hdrs, method=self.command)
        try:
            resp = urllib.request.urlopen(req, timeout=300)
        except urllib.error.HTTPError as e:
            resp = e
        except Exception as e:
            log(f"  [shim] upstream error: {e}")
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            payload = json.dumps({"type": "error", "error": {"type": "api_error", "message": str(e)}}).encode()
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        log(f"  [shim] {self.command} {self.path} -> {resp.status}")
        self.send_response(resp.status)
        streaming = "event-stream" in (resp.headers.get("Content-Type") or "")
        for k, v in resp.headers.items():
            if k.lower() in ("content-length", "transfer-encoding", "connection"):
                continue
            self.send_header(k, v)
        if streaming:
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            dump = os.environ.get("SHIM_SSE_DUMP", "")
            buf = bytearray()
            while True:
                chunk = resp.read(1)
                if not chunk:
                    self.wfile.write(b"0\r\n\r\n")
                    break
                buf += chunk
                self.wfile.write(b"%x\r\n" % len(chunk) + chunk + b"\r\n")
                self.wfile.flush()
            if dump:
                with open(dump, "ab") as fh:
                    fh.write(b"\n===== SSE RESPONSE (%d bytes) =====\n" % len(buf) + bytes(buf))
        else:
            data = resp.read()
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            if resp.status >= 400:
                log(f"  [shim] error body: {data[:400].decode('utf-8', 'replace')}")

    do_POST = do_GET = do_HEAD = do_PUT = do_DELETE = _proxy


if __name__ == "__main__":
    port = int(sys.argv[1])
    http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
