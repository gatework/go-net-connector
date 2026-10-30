# Third foundation refinement

This round follows [the approved plan](2026-10-30-refinement-plan.md), starting
from connector `59c1af1f9df9d6d0000d5e51d05b818f38616feb` and expect
`6e4e2dc054b1cd400801381d57b35f08f89a398f`. Baselines are exact Git archives.

The optional transport authentication hook receives the operation context.
Legacy hooks fail before login I/O. Partial process startup returns its resource
owner even on error; Device retains cleanup failures for retry. Trust shutdown
closes admission and cancels active persistence, interrupts process I/O, joins
the internal commit and then removes temporary files. Application callbacks
remain cooperative. Storage cancellation can race with the final rename; a
committed write is retained with its durability receipt. Command and interaction
write errors preserve progress and the complete original error tree beneath
safe metadata. See [API migration](api-migration.md).

Fleet allocates exactly one outcome array and avoids duplicate Map results.
HTTP inventory decodes known RawMessage fields once; PG columns map directly
to owned strings. Differential tests cover exact keys, nulls, malformed UTF-8,
ignored-column budgets and cancellation without partial results. The temporary
PostgreSQL integration includes later-row failure and all-or-nothing output.
TFTP completion uses a finite precompiled catalog with unchanged expressions.

Ten serial interleaved samples use Apple M5 arm64, Go 1.27.3 and
GOMAXPROCS=4. At 100,000 rows, local HTTP inventory drops from 354.1 ms to
181.0 ms, PG mapping from 126.1 ms to 11.2 ms, and mock fleet execution from
40.0 ms to 26.4 ms. Fleet allocation drops from 301.9 MiB to 89.3 MiB.
These are fixture workloads, not network-device or production throughput.
Direct HTTP/PG baseline helpers reproduce the prior mapping code in test-only
adapters; the public local-HTTP benchmark runs the unchanged baseline API.
Production baseline files were not edited.

TFTP completion scans improve across all measured vendors and line counts.
Generic command, paging and logging controls show no statistically significant
timing increase above 5%; the 1 MiB multiline case increases 3.45% in this sample.
The paired expect filter/capture evidence is recorded in its own module.

Independent review found a Session filter retention gap after sink failure;
the new regression failed before the fix and passed afterward. Final read-only
review found no actionable blockers. Benchmark raw samples, commands, test
logs and source digests are retained in
[the evidence directory](quality-evidence/2026-10-30-refinement).

Local checks, paired-source checks, pinned dependency consumers, remote CI and
published assets are separate evidence surfaces. No real-device acceptance is
claimed by these fixture tests.

Local validation passed: complete scripts/ci on Darwin Go 1.27.3, temporary
PostgreSQL 18.6 enabled, built archive worker, three 100000-execution fuzz
targets, source consumer and outside-checkout CLI. Full race/shuffle and vet
passed on Darwin and Linux arm64 with Go 1.26.0 and 1.27.3. Linux uses offline
containers and a read-only module cache; PG integration is proven on Darwin.
Lint reports zero issues; govulncheck finds no vulnerabilities. Release fixture
tests, paired expect sources and the disposable Firemon adapter pass.
