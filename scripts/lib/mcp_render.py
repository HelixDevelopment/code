#!/usr/bin/env python3
"""mcp_render.py — render a constitution-relative MCP fragment to its
root-relative generated form (HXC-159 T-P1.05).

Usage: mcp_render.py <fragment.json> <name> <root> <constdir>
Prints the canonical JSON of .mcpServers.<name> as it must appear in the
generated target. Fragments without the wrapper, absolute command/args[0],
or missing files exit non-zero (fail closed). Single source of the rewrite
rule used by register_mcp.sh (merge + --check) and the gate.
"""
import json
import os
import sys


def main() -> int:
    fpath, name, root, constdir = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
    try:
        f = json.load(open(fpath))
    except Exception as e:
        print("[mcp-render] %s: JSON parse error: %s" % (fpath, e))
        return 1
    if not isinstance(f.get("mcpServers"), dict) or name not in f["mcpServers"]:
        print("[mcp-render] %s: no .mcpServers.%s wrapper (flat refused)" % (fpath, name))
        return 1
    entry = dict(f["mcpServers"][name])
    rel = os.path.relpath(constdir, root)
    cmd = entry.get("command", "")
    if "/" in cmd:
        if os.path.isabs(cmd):
            print("[mcp-render] %s: absolute command refused" % name)
            return 1
        entry["command"] = os.path.join(rel, cmd)
    args = list(entry.get("args", []))
    # args[0] is prefixed ONLY when path-like (contains a /), e.g. the
    # media-validator script path. Bare subcommands (e.g. `mcp-stdio`) are
    # left untouched — prefixing them produces garbage (measured 2026-09-14).
    if args:
        if os.path.isabs(args[0]):
            print("[mcp-render] %s: absolute args[0] refused" % name)
            return 1
        if "/" in args[0]:
            args[0] = os.path.join(rel, args[0])
            entry["args"] = args
    print(json.dumps(entry, sort_keys=True, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
