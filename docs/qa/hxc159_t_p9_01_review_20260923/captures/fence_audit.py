#!/usr/bin/env python3
"""Independent CommonMark fence-state audit (T-P9.01 reviewer-authored, not reused
from any prior pass). Walks every .md file given, simulates CommonMark fenced-code
state (a fence closes only on a bare fence of the SAME char and >= length with NO
info string), and reports three defect signatures:

  S1  INNER-OPENER-INSIDE-BLOCK : a line matching ^\\s{0,3}(```|~~~)\w+ occurs INSIDE an
      open block whose fence length is <= the inner fence length. In CommonMark the
      inner opener is literal content, but the inner block's intended CLOSER will
      close the OUTER block prematurely -> everything after leaks as body text and the
      outer's intended closer re-opens a stray block. This is exactly the defect class
      fixed 3 times in B3/P8.01/P8.04.
  S2  LEAKED-DIAGRAM-DIRECTIVE  : a line outside any code block that starts with a
      Mermaid / diagram directive (graph|flowchart|sequenceDiagram|stateDiagram|
      classDiagram|erDiagram|gantt|xychart-beta|mindmap|timeline|pie) — body-text leak.
  S3  UNCLOSED-AT-EOF           : file ends with a fence still open.

Exit 0 = zero findings; exit 1 = findings printed.
"""
import re, sys, json

FENCE_RE = re.compile(r'^( {0,3})(`{3,}|~{3,})(.*)$')
DIRECTIVE_RE = re.compile(r'^\s*(graph\s+(TD|TB|LR|RL|BT)\b|flowchart\s+(TD|TB|LR|RL|BT)\b|sequenceDiagram\b|stateDiagram(-v2)?\b|classDiagram\b|erDiagram\b|gantt\b|xychart-beta\b|mindmap\b|timeline\b|pie\b|gitGraph\b)')

def audit(path):
    findings = []
    open_fence = None  # (char, length, info, line_no)
    with open(path, encoding='utf-8', errors='replace') as fh:
        lines = fh.read().split('\n')
    for i, line in enumerate(lines, 1):
        m = FENCE_RE.match(line)
        if open_fence is None:
            if m:
                fence, info = m.group(2), m.group(3).strip()
                # backtick fence info string may not contain backticks
                if fence[0] == '`' and '`' in info:
                    pass
                else:
                    open_fence = (fence[0], len(fence), info, i)
                    continue
            if DIRECTIVE_RE.match(line):
                findings.append({'sig': 'S2', 'line': i, 'text': line.strip()[:80]})
        else:
            ch, ln, info, start = open_fence
            if m and m.group(2)[0] == ch and len(m.group(2)) >= ln and m.group(3).strip() == '':
                open_fence = None
                continue
            if m and m.group(2)[0] == ch and m.group(3).strip() != '' and len(m.group(2)) >= ln:
                findings.append({'sig': 'S1', 'line': i, 'outer_open': start,
                                 'outer_fence': ch * ln, 'outer_info': info,
                                 'text': line.strip()[:80]})
    if open_fence is not None:
        findings.append({'sig': 'S3', 'line': open_fence[3], 'text': 'fence still open at EOF'})
    return findings

def main():
    total = 0
    report = {}
    for p in sys.argv[1:]:
        f = audit(p)
        report[p] = f
        total += len(f)
        for x in f:
            print(f"{p}:{x['line']}: {x['sig']} {x}")
    print(json.dumps({'files': len(report), 'total_findings': total,
                      'per_file': {k: len(v) for k, v in report.items()}}, indent=1))
    sys.exit(1 if total else 0)

if __name__ == '__main__':
    main()
