# Foundation quality verification — 2026-10-29

The [approved plan](2026-10-29-quality-plan.md) is implemented in the local worktrees. Baselines are go-expect `3d3b1ef` and go-net-connector `c68c42b`. Existing connector-quality archive, validation and fleet migration work was preserved. There are no commits, pushes, releases, module replacements or go.mod/go.sum changes in this pass.

Active worktrees, relative to `/Users/crate/Works/go`:

- `.worktrees/connector-unification/go-expect`, branch `feat/connector-unification`.
- `.worktrees/connector-quality/go-net-connector`, branch `refactor/connector-quality`.

## Implementation

Device construction and validation are separate from connection I/O. `Profile.Validate`, `ConnectionConfig.Validate` and `ValidateOptions` perform pure checks reused by constructors and bootstrap. `vendors.Open` is removed; callers create a Device, register Close, then Connect. Partial factory returns remain owned until cleanup succeeds, primary and cleanup errors remain discoverable with `errors.Is`, and borrowed AfterLogin maps are never cleared. Interact retains cancellation/output errors together with terminal restore and cleanup failures.

`StepResult.Rendered` exposes immutable derived text shared by result snapshots. Raw and Output retain their contracts; failed Attempts remain separate from accepted Steps. Raw/body storage is shared until a pager marker actually requires distinct output. Ordinary UTF-8 lines use a string rendering path; terminal edits and invalid encodings retain the byte renderer. Disabled echo logging avoids creating redaction/rendering pipelines, while response budgets remain enforced. PAN and Hillstone configuration formats are preserved.

Archive endpoints are validated for nil/typed-nil values, Close ownership and working deadlines, retaining the original error causes. Prepare admission and its initialization gate are cancelable; its network operations no longer hold the state mutex. Already-canceled and closed calls cannot perform setup or use the prepared shortcut. Direct Client.Close still reports active work; contexts and Pool retain cancellation ownership.

Planning, execution, reports and diagnostics now live in `fleet`; Netdisco inventory sources/cache remain in `netdisco`. CLI/report formats are unchanged, and `go list -deps ./fleet` contains no netdisco dependency. Worker Goexit cancels admission and preserves its batch error even when parent cancellation happens first. Completed outcomes survive observer failure/Goexit. Constant rule regexps are compiled once. Embedded TextFSM index/templates use a bounded catalog of immutable compiled definitions with independent per-call mutable values and parser output; custom paths are not cached.

The paired go-expect implementation reuses OS read scratch, incrementally scans pure-literal All patterns, and reduces public Redactor allocation. Arbitrary regexp matching and the 10ms polling architecture are unchanged. Its detailed report is in that worktree at `docs/2026-10-29-quality.md`.

## Regression and review evidence

Failing regressions preceded the relevant repairs: retryable partial transport cleanup, borrowed metadata ownership, archive endpoint validation/canceled setup, Prepare cancellation while another caller waits on network I/O, completed fleet receipts after Goexit, and repeated TextFSM compilation. Rendering has differential fuzz coverage against the existing byte renderer, plus exact 32 MiB line-boundary coverage.

Independent review found two connector issues in the initial implementation. Echo-only output could be accepted when raw prompt whitespace/control bytes no longer matched rendered text; prompt normalization now uses the same rendering and five previously failing prompt cases pass. Parent cancellation could hide abnormal worker exit in the batch error; abnormal exit is now recorded separately and both causes are retained. The fixes passed race checks and were independently rechecked. The expect review also found a quadratic long-secret boundary scan; a bounded fast-path fallback and long-prefix matching/Finish regressions address it.

## Local validation

| Check | Result and scope |
| --- | --- |
| macOS arm64, Go 1.27.3 | Full `bash scripts/ci`: goimports, module verification/tidy, command builds, complete race/shuffle suite, vet, three fixed 100,000-execution fuzz targets, source-consumer and outside-checkout CLI checks passed |
| macOS arm64, Go 1.26.0 | Full race/shuffle suite and vet passed; minimum language compatibility is separate from patched production toolchain selection |
| Linux arm64, Go 1.27.3 and 1.26.0 | Full race suites and vet passed in disposable Docker source copies, networking disabled and module cache mounted read-only |
| Installed archive worker | Built worker exercised through real process pipes in the macOS full CI run |
| PostgreSQL | Temporary isolated PostgreSQL 18.6 integration passed in the macOS full CI run |
| golangci-lint v2.15.0 | 0 issues |
| actionlint v1.7.12 | Passed |
| Release mechanics | All 11 synthetic release tests passed; this did not package or publish the working tree |
| govulncheck v1.9.0, Go 1.27.3 | No vulnerabilities found |
| Paired go-expect | Full CI, five fixed-count fuzz targets, Go 1.26/1.27 macOS and Linux race/vet, source consumer and Go 1.27.3 vulnerability scan passed |
| firemon adapter contracts | Copied current adapter tests passed race/shuffle and vet against candidate connector source; original adapter files were unchanged |

The default host Go 1.27.1 initially reported nine reachable standard-library vulnerabilities. Rechecking with installed Go 1.27.3 cleared the scan and the entire connector CI passed again. Benchmark comparisons retain Go 1.27.1 on both sides to keep the measurement toolchain constant. The Go 1.26.0 directive is a language compatibility floor, not a recommendation to deploy an old unpatched toolchain.

Logs are in [quality-evidence/2026-10-29](quality-evidence/2026-10-29/). Linux runs did not enable the external installed-worker/PostgreSQL fixtures; those have macOS evidence above. No production switch, external SSH server or remote CI was exercised in this pass.

The source-consumer check builds a disposable copy as its own module; all external dependencies remain versioned. The firemon check copies the adapter into a consumer package of the candidate module, changes only its test self-import path, and pins its current parent module `github.com/gatework/firemon v0.0.0-20261029104925-6ec81b00be2a`. Its log records source hashes. This establishes source compatibility, not versioned downstream adoption. Connector still resolves go-expect `v0.1.1-0.20261026150421-505caf867ed3`: the new expect optimizations were measured separately and are not yet incorporated into connector's pinned dependency.

## Measured performance

Apple M5, macOS arm64, Go 1.27.1. Each case has ten samples per side, `-benchmem -benchtime=200ms`. Test/build workloads from this agent team were paused during measurement. The baseline is a clean `git archive c68c42b` export with identical new benchmark fixtures; only the fleet benchmark package/API names differ. No external dependency version differs.

| Workload | Before → after | Measured change |
| --- | --- | --- |
| Continuous short command, no logging | 11.12 → 8.77 us/op; 10.22 → 5.07 KiB/op | Time −21.1%, allocation bytes −50.4% |
| Continuous short command, info/debug | 13.48 → 10.86 / 15.27 → 12.94 us/op | Time −19.4% / −15.2% |
| 8 MiB RunningConfig | 328.3 → 109.3 MiB/op; 2,238,648 → about 1,355 allocs/op | Allocation bytes −66.7%; time statistically unchanged |
| Embedded TextFSM, one record | 258.1 → 1.40 us/op; 7,227 → 20 allocs/op | Time −99.5% |
| Embedded TextFSM, 1,000 records | 1,190 → 871 us/op | Time −26.8%, allocation bytes −43.4% |
| Indexed TextFSM, one record | 304.2 → 1.61 us/op | Time −99.5% |
| Seven inventory rule resolutions | 42.22 → 5.64 us/op | Time −86.6%, allocation bytes −99.0% |
| 1,000 / 10,000 mock fleet tasks | Time statistically unchanged | Added abnormal-exit accounting costs two allocations per batch |
| Single-line output, 64 KiB / 1 MiB / 8 MiB | Interleaved samples: time −2.7% / −2.5% / −2.2% | Allocation bytes −52.2% / −55.0% / −55.1% |
| Single-line output, 32 MiB | Initial broad sample: time +2.0%, allocation bytes −56.9% | Small timing regression retained in the report |

The first broad output timing samples showed +2% to +33%, with candidate spread up to 92%. This prompted a second A/B/B/A sequence using compiled test binaries, five samples per segment, giving ten samples per side for 64 KiB/1 MiB/8 MiB. It measured the small improvements above with 1–3% spread. Both original and repeated samples remain in the artifacts; the 32 MiB case was not statistically resampled. CPU profiles of 20 fixed 1 MiB operations primarily attribute time to runtime scheduling and regexp matching. Allocation reduction does not imply a proportionate end-to-end time gain.

Peak RSS was measured separately with `/usr/bin/time -l` on compiled test binaries, one fixed 32 MiB operation per side: 407,977,984 → 256,360,448 bytes. This is a single high-water observation, not retained-heap data or a statistical comparison. B/op above is cumulative allocation. go-expect separately records post-GC idle heap, retained suffix capacity and its own process RSS; those measures are not interchangeable.

go-expect's final separate samples show literal All time −27% to −49%, small/mid OS reads about −11% to −12%, and safe/no-rule public redaction about −86% to −91% for 1 KiB chunks. Its matched 32-byte redaction remains about 4.3% slower with more allocation bytes; long-secret fallback is 4–8% slower than the old general matcher while avoiding the newly discovered quadratic scan. These tradeoffs remain explicit in its report.

Raw samples, benchstat comparisons, CPU summaries and RSS observations are in [benchmarks/2026-10-29](benchmarks/2026-10-29/). Reproduce the main candidate cases with:

```sh
GOWORK=off GOFLAGS=-mod=readonly go test -run '^$' \
  -bench 'Benchmark(CommandOutput|CommandPaged1000|CommandMultiline|CommandLogging|RunningConfig|EmbeddedTemplate|IndexedTemplate|RulesResolve|MockFleet)$' \
  -benchmem -benchtime=200ms -count=10 ./connector ./vendors ./textfsm ./fleet
```

The baseline command uses `./netdisco` in place of `./fleet`. The complete comparison uses `benchstat -ignore pkg baseline.txt candidate.txt` to compare the renamed fleet cases. Benchstat was built from `golang.org/x/perf v0.0.0-20261009192801-be2c69fb417e`.

## Adoption boundary

See [api-migration.md](api-migration.md) for the deliberate public API changes. Publication and bumping versioned dependencies remain separate steps. This implementation has local source, consumer, cross-platform and benchmark evidence; it does not establish device-fleet performance, remote CI or published-artifact availability.
