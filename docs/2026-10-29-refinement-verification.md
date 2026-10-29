# Foundation refinement verification — 2026-10-29

This round implements [the approved refinement plan](2026-10-29-refinement-plan.md)
on top of the complete, uncommitted round-one sources. Baselines were frozen in
`/tmp/connector-refinement-20261029/{expect,connector}-baseline` with per-file
SHA256 manifests before editing. Comparisons are **not against Git HEAD**. Earlier
WIP, original checkouts, module versions and replacements remain separate.

## Implemented contracts

Callback contexts now include cancellation from `Device.Close`: `WithOperation`,
`AfterLogin` and `ConfigStrategy.Finish` use the new signatures described in
[API migration](api-migration.md#2026-10-29-callback-and-response-refinement).
AfterLogin returns `LoginState` by value. Close interrupts owned I/O without waiting
for application callbacks. Callbacks remain cooperative; operation scope cleanup
cancels and joins admitted nested calls. Operation, receipt, logging and cleanup
errors retain their causes; diagnostic classification follows the primary branch.
No command replay was introduced.

`Response` preserves exact Raw and RawPrompt alongside eagerly verified output,
rendering, prompt and encoding. Steps, attempts and callbacks share a frozen
snapshot; getters never decode or re-detect. Rejected commands retain converted
evidence in Attempts. Incomplete and invalid conversion are explicit errors.
Formatting remains metadata-only. Result snapshots copy their outer slice;
Command access still copies mutable fields.

Built-in vendor declarations reuse a finite compiled catalog and return independent
mutable copies. Device matchers use private snapshots. Plan capability lookups
are scoped to one vendor in one plan. Configuration strategies remain per
collection. Single-step output shares its string; unchanged Cisco/NXOS header
cleaning avoids copying the full configuration.

Expect adds a construction-time runtime-poller choice for deadline-capable
transports, an owned Poll request fence, optional PTY read-error normalization,
receive-buffer detachment and incremental long-secret redaction. Connector
transport wrappers forward both deadline and normalization capabilities. The
design follows the [RawConn callback lifetime contract](https://pkg.go.dev/syscall#RawConn)
and [File read-deadline contract](https://pkg.go.dev/os#File.SetReadDeadline).
The real connector module still pins its prior expect version; these new expect
behaviors have **source-pair evidence**, not published adoption.

## Correctness and integration

Connector passed:

- Darwin arm64 Go 1.26.0 and 1.27.3: full race/shuffle suite and vet.
- Linux arm64 Go 1.26.0 and 1.27.3: disposable Docker sources, offline network,
  read-only module cache, full race/shuffle suite and vet.
- `scripts/ci` on Go 1.27.3: formatting, module verification/tidy, all CLI builds,
  archive worker, temporary PostgreSQL 18.6, full race/shuffle/vet, three
  100,000-execution fuzz targets, source consumer and CLI execution outside checkout.
- Final review fixes: affected packages rechecked under race on both toolchains
  and both platforms, plus Linux vet; golangci-lint v2.15.0 reports zero issues.
- govulncheck v1.9.0 using patched Go 1.27.3: no vulnerabilities found.
- Firemon adapter source contracts under race/vet with the callback migration and
  explicit verified-output/raw fallback applied only in a disposable copy. Parent
  Firemon version: `v0.0.0-20261029122754-9294f4532eb2`. The log records exact source
  hashes. The original adapter and its dependency pins were not changed.
- Paired-source race/vet across connector, vendors, transfer and expect. The fixture
  copies expect into a temporary subpackage and changes only its import prefix;
  no `replace` or `go.work` is used. External modules remain versioned.

The [evidence directory](quality-evidence/2026-10-29-refinement) retains commands,
logs, source hashes and both independent reviews. The reviews found and resolved:
login response conversion before publication, primary storage classification in
the presence of typed cleanup failures, and repeated short-rule rescanning beside
a large retained redaction suffix. Regression logs include failing observations
before the fixes. Metadata-only formatting, exact wire evidence, cancellation,
completion receipts and no-replay behavior remain covered.

Expect's final sources also passed Darwin/Linux Go 1.26.0/1.27.3 race/vet,
its complete CI script including five 100,000-execution fuzz targets, benchmark
smoke, dialogue/source consumer, 23 release-script tests, golangci-lint v2.15.0
and patched-toolchain govulncheck. The allocation correction received a further
independent review. The final paired-source probe was rerun after that correction;
the log and hashes are retained separately from the earlier paired probe.

## Connector performance

Go 1.27.3, Darwin arm64, Apple M5, GOMAXPROCS=4. Ten samples per workload and
version, serial execution, alternating baseline/candidate order. Existing
command/output/logging/config workloads are identical on both sides; the new
full `NewPlan` benchmark is copied unchanged to the baseline benchmark module.
No tests, builds or other agent benchmarks ran during this window.

| Workload | Baseline median | Candidate median | Evidence |
| --- | ---: | ---: | --- |
| Plan, 1,000 devices | 26.81 ms | 0.745 ms | time -97.22%; B/op -97.90% |
| Plan, 10,000 devices | 289.98 ms | 8.291 ms | time -97.14%; B/op -97.10% |
| Configuration, 1 MiB input | 13.68 MiB/op | 12.70 MiB/op | allocation -7.10%; time indistinguishable |
| Configuration, 8 MiB input | 109.3 MiB/op | 101.8 MiB/op | allocation -6.84%; time indistinguishable |
| Command, 64 KiB | 632.4 KiB/op | 568.2 KiB/op | allocation -10.15%; time indistinguishable |
| Logging disabled | 4.929 KiB/op | 4.155 KiB/op | allocation -15.71%; time indistinguishable |

Reported changes above have p<0.05 where a reduction is claimed. No measured
connector time/op regression exceeded the significant 5% rejection threshold.
The generic command and vendor prompt regex paths still dominate large-output
time; this round does not claim a throughput improvement there. Allocation savings
for configuration correspond to approximately one normalized full-output copy.

[Raw samples and benchstat](benchmarks/2026-10-29-refinement) retain all results,
including indistinguishable cases and the exact workload driver.

## Expect performance

The expect worktree contains its own [refinement report](../../../connector-unification/go-expect/docs/2026-10-29-refinement.md)
and complete raw measurements. The same Go 1.27.3 toolchain and GOMAXPROCS=4 were
used for ten serial, alternating samples. Every idle population ran in a fresh
process so that retained Go runtime threads from earlier populations could not
distort the comparison. Idle populations have not performed a payload read;
their scratch storage is still lazy. Warm receive retention is measured separately.
The observation harness's time/op is not throughput.

| Workload | Baseline median | Final candidate median |
| --- | ---: | ---: |
| Real PTY receipt, 1 byte | 11.17 µs | 7.19 µs |
| Real PTY receipt, 1,024 bytes | 13.16 µs | 9.39 µs |
| Dense 64 KiB secret, 256 KiB input | 3.203 s | 549.2 µs |
| 1,024 idle sessions: process threads | 915.5 | 7 |
| 1,024 idle sessions: heap/session | 8.579 KiB | 1.938 KiB |
| 1,024 idle sessions: Poll p95 | 76.01 ms | 66.71 µs |
| 1,024 idle sessions: Close p95 | 10.87 ms | 5.98 µs |
| Generic receive, 1 MiB burst, discarded Result, 8-byte tail | 1,051.21 KiB/session | 18.92 KiB/session |

At 1,024 sessions, idle CPU/session-ms decreases 99.98%. Descriptor count is
unchanged: the harness owns 2,048 endpoints on both versions, with no per-session
wake descriptor added.

The dense redactor scales with input length. Mixed long/short rules under one-byte
appends take about 18 ns/op with zero allocations at retained rule lengths
257/4,096/65,536. The generic receive experiment separates caller-held Results:
retaining a 1 MiB Result still correctly retains about 1 MiB of evidence. It is not
evidence of a 32 MiB-per-connector idle leak; connector already uses bounded
streaming fallback around 64 KiB.

Performance review rejected an intermediate eager-tail-capacity allocation
regression. Exact cold/detached tails plus amortized spare capacity restored short
matches, dense allocation totals and safe-tail capacity to baseline. Ten new
redactor samples show no significant regression above 5% in time/op, B/op or
allocations. Earlier samples remain archived.

The first idle heap series included resource-probe transient allocations in its
baseline, producing an invalid negative single-session value. That series is
retained for audit but excluded from heap claims. The corrected probe performs GC
after baseline resource probes; a separate ten-sample heap series covers all four
populations without repeating the sequential latency measurement. Corrected heap
per session for populations 1/64/256/1,024 is respectively 2.039/7.080/7.148/8.579
KiB before and 1.555/1.940/1.934/1.938 KiB after.

## Delivery boundary

All seven implementation tasks are complete. Both scoped reviews and the final
integration review passed; performance acceptance includes the rejected
intermediate allocation result and the corrected measurements. Source manifests
record the final Go files, module files and verification scripts. The original
primary expect/connector checkouts and the other connector-unification checkout
remain clean, and both working modules retain their original go.mod/go.sum.

No commit, push, release, published dependency adoption, remote CI or real-device
acceptance is claimed by these local results.
