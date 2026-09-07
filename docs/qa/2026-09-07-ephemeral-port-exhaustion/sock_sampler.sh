#!/usr/bin/env bash
# Sample socket pressure DURING a suite run — the measurement I never took.
#
# The question this settles: when a bind to :0 fails with EADDRINUSE, is the
# ephemeral range actually exhausted, or is something else producing that errno?
# Sampling after the fact (as I did) cannot answer it: the sockets are gone.
#
# Records, every 5s: total sockets by state, how many local ports in the
# ephemeral range are occupied, and the top port-holding processes.
set -uo pipefail
S="$(dirname "$0")"
OUT="$S/sock_samples.tsv"
read -r EPH_LO EPH_HI < /proc/sys/net/ipv4/ip_local_port_range
RANGE=$(( EPH_HI - EPH_LO + 1 ))

printf 'ts\testab\ttw\tlisten\teph_used\teph_range\tpct\ttop_holders\n' > "$OUT"
for _ in $(seq 1 400); do
  pgrep -x "go\|go1" >/dev/null 2>&1 || pgrep -f 'go test|\.test$' >/dev/null 2>&1 || true
  snap=$(ss -ant 2>/dev/null)
  estab=$(printf '%s\n' "$snap" | awk 'NR>1 && $1=="ESTAB"'   | wc -l)
  tw=$(   printf '%s\n' "$snap" | awk 'NR>1 && $1=="TIME-WAIT"' | wc -l)
  lis=$(  printf '%s\n' "$snap" | awk 'NR>1 && $1=="LISTEN"'  | wc -l)
  # Count DISTINCT local ports inside the ephemeral range — that is the
  # quantity that actually runs out, not the raw socket count.
  eph=$(printf '%s\n' "$snap" | awk -v lo="$EPH_LO" -v hi="$EPH_HI" '
        NR>1 { n=split($4,a,":"); p=a[n]+0; if (p>=lo && p<=hi) print p }' \
        | sort -un | wc -l)
  pct=$(awk -v u="$eph" -v r="$RANGE" 'BEGIN{printf "%.1f", (r?100*u/r:0)}')
  top=$(ss -antp 2>/dev/null | grep -oP 'users:\(\("\K[^"]+' | sort | uniq -c \
        | sort -rn | head -3 | awk '{printf "%s=%s ",$2,$1}')
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$(date +%H:%M:%S)" "$estab" "$tw" "$lis" "$eph" "$RANGE" "$pct" "${top:-none}" >> "$OUT"
  sleep 5
done
