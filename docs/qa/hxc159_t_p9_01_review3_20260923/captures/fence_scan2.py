#!/usr/bin/env python3
"""Review-3 SECOND fence instrument (independent of fence_audit.py; §11.4.273 two-instrument rule).
Tracks CommonMark fenced-code state. Reports:
  N1 nested-opener: a fence line WITH an info string appears while a block is open and its
     fence char/length would be accepted as the outer's closer — i.e. the inner block's own
     bare closer will terminate the OUTER block early (the recurring defect class).
  N2 leaked-directive: a Mermaid/diagram directive at line start OUTSIDE any block.
  N3 eof-open: fence still open at EOF.
  N4 mermaid-unbalanced: count of '```mermaid' openers vs the number of mermaid blocks that
     closed on a bare fence (should be equal).
Exit 1 on any N1/N2/N3 or N4 mismatch."""
import re, sys, json
F = re.compile(r'^ {0,3}(`{3,}|~{3,})(.*)$')
D = re.compile(r'^\s*(graph\s+(TD|TB|LR|RL|BT)\b|flowchart\s+(TD|TB|LR|RL|BT)\b|sequenceDiagram\b|stateDiagram(-v2)?\b|classDiagram\b|erDiagram\b|gantt\b|xychart-beta\b|mindmap\b|timeline\b|pie\b|gitGraph\b)')
def scan(path):
    out=[]; openf=None; mer_open=0; mer_closed=0
    for i,line in enumerate(open(path,encoding='utf-8',errors='replace').read().split('\n'),1):
        m=F.match(line)
        if openf is None:
            if m:
                fence,info=m.group(1),m.group(2).strip()
                if fence[0]=='`' and '`' in info: continue
                openf=(fence[0],len(fence),info,i)
                if info.split()[:1]==['mermaid']: mer_open+=1
                continue
            if D.match(line): out.append(('N2',i,line.strip()[:70]))
        else:
            ch,ln,info,st=openf
            if m and m.group(1)[0]==ch and len(m.group(1))>=ln:
                if m.group(2).strip()=='':
                    if info.split()[:1]==['mermaid']: mer_closed+=1
                    openf=None; continue
                out.append(('N1',i,f'inner opener {line.strip()[:40]!r} inside {ch*ln}{info} opened at {st}'))
    if openf: out.append(('N3',openf[3],'open at EOF'))
    if mer_open!=mer_closed: out.append(('N4',0,f'mermaid openers={mer_open} closed={mer_closed}'))
    return out
tot=0; rep={}
for p in sys.argv[1:]:
    r=scan(p); rep[p]=len(r); tot+=len(r)
    for s,l,t in r: print(f'{p}:{l}: {s} {t}')
print(json.dumps({'files':len(rep),'total':tot,'per_file':rep},indent=1)); sys.exit(1 if tot else 0)
