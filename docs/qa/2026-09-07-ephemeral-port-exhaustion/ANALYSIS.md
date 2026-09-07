# Ephemeral-port exhaustion during `go test ./...` — root-cause record

**Run id:** 2026-09-07-ephemeral-port-exhaustion
**Status:** root cause CONFIRMED by measurement; per-suite attribution UNCONFIRMED
**Governs:** §11.4.83 (QA evidence), §11.4.5/§11.4.69 (captured evidence),
§11.4.6 (FACT vs HYPOTHESIS), §11.4.50 (determinism), §11.4.102 (systematic debugging)

## Symptom

A full-suite run produced 9 failures that do **not** reproduce when the same
tests run in isolation:

```
TestHealthMonitor_CheckServiceHealth_TCP   listen tcp 127.0.0.1:0: bind: address already in use
TestHealthMonitor_ConcurrentAccess         (same)
TestHealthMonitor_MonitorLoop              (same)
TestHealthMonitor_MultipleServices         (same)
TestAllocatePort_RangeExhausted_WithEphemeral
        failed to allocate ephemeral port: listen tcp :0: bind: address already in use
TestConcurrentAllocations                  no ports available in configured range
TestConcurrentReleases                     (same)
TestFetcher_FetchMultiple                  "140.520457ms" is not less than "37.454908ms"
TestThroughputScalability                  (needs a live server on :8080)
```

`TestFetcher_FetchMultiple` looks unrelated but is the same cause: its
*sequential* baseline reuses one keep-alive connection while the *concurrent*
path must dial two more — and dialling was what was failing. Concurrent measured
**4x slower** than sequential, not merely un-parallel.

## Root cause — FACT (measured)

The host ephemeral range saturates completely for ~60-90 s, then drains:

| time     | ESTAB | TIME-WAIT | LISTEN | eph ports used | of 28,232 |
|----------|------:|----------:|-------:|---------------:|----------:|
| 09:41:14 |   403 |       367 |    119 |            567 |    2.0 %  |
| 09:42:44 |   ... |      1395 |    121 |            ... |   60.0 %  |
| 09:43:15 | 11010 |     20317 |    120 |         28,117 |   99.6 %  |
| 09:43:35 |   745 |     28450 |    119 |         28,109 |   99.6 %  |
| 09:57:20 |   617 |   **29625** |  119 |     **28,221** | **100.0 %** |
| 09:45:39 |   426 |        79 |    118 |            310 |    1.1 %  |

Raw corpus: `socket_samples.tsv` (179 rows), sampler: `sock_sampler.sh`.

**Why every "check it afterwards" measurement looked clean:** the range drains to
1.1 % within ~90 s. Every post-hoc sample landed in the trough. Only sampling
*during* the run exposed it.

### Why the errno is decisive

`bind()` to port **0** asks the kernel to choose. Linux `inet_csk_get_port()`
returns `-EADDRINUSE` **only after `inet_csk_find_open_port()` has scanned the
entire eligible range and found a conflict on every port.** It is therefore not
a transient or racy code — it is a completed exhaustive search that failed.

Three corollaries, all from kernel source:

1. **`SO_REUSEADDR` does not help.** At the port-0 call site
   `inet_csk_bind_conflict()` runs with `relax=false, reuseport_ok=false`;
   tracing `inet_bind_conflict()`, *both* branches return "conflict". Every
   socket occupying a candidate port — LISTEN, ESTABLISHED, **or TIME_WAIT** —
   is a hard conflict. The escape hatch is `ip_autobind_reuse`, which is `0`
   on this host.
2. **`tcp_tw_reuse=2` does not help.** It is a `connect()`-path facility
   (`tcp_twsk_unique()` via `__inet_check_established()`), not `bind()`.
3. **TIME_WAIT is 60 s and not tunable** (`TCP_TIMEWAIT_LEN` is hardcoded;
   `tcp_fin_timeout` governs FIN_WAIT_2, not TIME_WAIT). `tcp_max_tw_buckets`
   is 131,072 here — 4.6x the port range — so it never caps growth first.

### The arithmetic

```
28,232 ports / 60 s TIME_WAIT  ~=  470 close-cycles/sec sustained
                                    before total saturation
```

`GO_TEST_P ?= $(NPROC)` = **16** on this host, and `go test` runs packages in
parallel as separate processes with `-parallel` multiplying within each. 470/s
split 16 ways is only ~29/s per binary. `httptest.NewServer` + one round-trip
consumes **two** ephemeral ports, and `Server.Close()` makes the **server** the
active closer — so the server's port, drawn from `127.0.0.1:0`, enters TIME_WAIT.

This is exactly why the failures never reproduce in isolation: one package
generates a trivial fraction of 470/s; only the aggregate reaches saturation.

## The alternative hypothesis, RULED OUT

A **listener leak** was the one candidate that would have meant something worse
(the port allocator would be masking a resource leak, and fixing it would hide
the leak). Its discriminator is a growing LISTEN count.

**LISTEN stayed flat at 118-122 across all 179 samples while TIME-WAIT peaked at
29,625.** No leak.

Also ruled out, with reasons:

| Candidate | Verdict |
|---|---|
| fd exhaustion presenting as EADDRINUSE | **Ruled out** — surfaces as EMFILE/ENFILE on `socket()`, a different errno on a different syscall |
| conntrack table exhaustion | **Ruled out** — causes silent packet drops + a dmesg line, never a `bind()` errno |
| kernel bhash2 bind-conflict bug | **Ruled out** — the known regressions were false *negatives* (binds wrongly succeeding), fixed in 6.1.54; host is 7.0.0 |
| range shrunk by reserved ports | **Ruled out** — `ip_local_reserved_ports` measured empty |
| container netns with a small range | **Ruled out for host-netns processes** (`net:[4026531833]`) |

## What is NOT established (§11.4.6)

**Which suite floods the range.** TIME_WAIT sockets are *orphaned* — no owning
process — so `ss -antp` cannot attribute them. At the 100 % peak the named
holders were `helixcode=256 rootlessport=212 performance.tes=75`, totalling ~543
of 28,221. That is correlation, not attribution.

Four load-generating suites carry **no build tag** and therefore run inside
`go test ./...` beside ~190 unit packages: `tests/ddos`, `tests/performance`,
`tests/scaling`, `tests/stresschaos`. `tests/integration` is 28/35 tagged, so
the convention already exists — these four sit outside it.

`tests/ddos` has **zero** body-drain/close calls across all three files;
`tests/performance` drains correctly with `MaxIdleConnsPerHost: 100`. So the
obvious suspect is not obviously guilty, and the attribution experiment
(`attribute_flood.sh` — each suite run alone, peak port usage recorded) is the
measurement that would settle it.

## A correction worth recording

An initial reading called the allocator's `net.Listen("tcp", ":0")` wildcard bind
"a real defect" and proposed narrowing it to `127.0.0.1:0`. **That is wrong for
this consumer.** The single production caller is
`internal/discovery/client.go:247` -> `Register()`, which hands the allocated
port to a *registry* — it is advertised to other services as a connection
target, and nothing in that path binds a listener. Narrowing the probe to
loopback would advertise a port free on loopback but possibly taken on the LAN
interface, where consumers actually connect. That would be worse.

The genuine defect is different: the fallback draws **advertised service ports
from the ephemeral range** (32768-60999), which the kernel may hand to any
outbound connection at any moment. The allocator already owns static ranges
outside that range; the ephemeral fallback is the wrong part.

## Open items

1. Run `attribute_flood.sh` to convert per-suite attribution from hypothesis to
   fact.
2. Decide whether the four load suites get build tags. This changes what
   `make test` covers, so it is an operator decision under §11.4.122 — prepared,
   held.
3. Fix the allocator's ephemeral fallback to draw from a reserved range outside
   `ip_local_port_range`, not `net.Listen(":0")`.

## Honest boundary

This record establishes the *mechanism* with measured evidence and excludes the
worse alternatives. It does **not** establish which suite is responsible, and it
does not by itself make the suite deterministic — a subsequent run showed 198
packages ok and 1 failure only because the flood peaked during the compile phase
and drained before the test packages ran. That is timing, not a fix.

## Addendum — 150 leaked load generators (self-inflicted host load)

While attributing host load per §11.4.174 (verify a process is OURS before
concluding anything about it), 150 orphaned busy-loop processes were found:

```
bash -c HXLOADGEN=1; while :; do :; done
bash -c HXLOADGEN=1; while :; do /bin/true; /bin/echo x >/dev/null; done
```

All `ppid=1` (parent died, systemd adopted them), all ~4,600 s old, all doing
nothing but spinning. They are leftover scaffolding from an earlier
run-under-contention determinism experiment whose parent exited without reaping
them — a §11.4.14 cleanup failure in that experiment.

Effect of reaping them (matched strictly on the `HXLOADGEN` marker plus the
busy-loop body, with the matcher excluding its own pid/ppid):

| metric | before | after |
|---|---:|---:|
| runnable processes | 157 | **10** |
| idle CPU | ~0 % | **33.9 %** |
| load average (1 min) | 171 | 125 (decaying from 215) |
| swap used | 7/7 GiB | 6/7 GiB |

**An initial pass counted 7 of these, because `ps --sort=-pcpu | head -12`
showed only the top slice. The true count was 150.** Counting from a truncated
listing is the same class of error as sampling sockets in the drain trough.

### What this does and does not change

- It does **NOT** invalidate the port-exhaustion root cause. That was measured
  directly as socket counts (TIME-WAIT peaking at 29,625; ephemeral usage at
  100.0 %), which is independent of CPU load.
- It **DOES** mean the "host under heavy load" condition treated as
  environmental throughout this investigation was substantially self-inflicted.
  Any conclusion that leaned on host load as a *given* should be re-read with
  that in mind.
- Timing-sensitive assertions would have been aggravated by it. This is a second,
  independent argument for the §11.4.50 rule that a test must never infer
  behaviour from wall-clock duration: the clock was being distorted by our own
  leaked scaffolding.

### Follow-up owed

Any experiment that spawns load generators must reap them in a
`trap '...' EXIT` (§11.4.14). The experiment that leaked these did not.
