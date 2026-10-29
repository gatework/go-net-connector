# Implementation progress

The latest local quality pass is complete; see [2026-10-29 verification](2026-10-29-quality-verification.md). The following delivery notes are historical and do not authorize publication of the new work.

Implementation and final scoped review are complete. The user authorized a private GitHub commit/push and first release on 2026-10-26. Release preparation is in progress; deployment remains outside scope.

| Stage | Status |
| --- | --- |
| P0 frozen references | Recorded manifests and working-source snapshots; Ruby baseline suite passes |
| P1 expect compatibility | Implemented; independent review accepted after one repair round |
| P2 connector lifecycle | Implemented; lifecycle/accounting/logging/pager repair independently accepted |
| P3 vendors/parser/storage | 12 profiles, 19 templates and private atomic storage implemented and reviewed |
| P4 topology | Implemented; formatter error propagation repair accepted |
| P5 transfer/archive | TFTP/Junos FTP/SCP, local/SSH worker and pool implemented; five archive fixes independently accepted |
| P6 inventory/Fleet/report | HTTP/PG/cache/planning/report implemented; source, unchanged receipt and diagnostic repairs accepted |
| P7 config/CLI | Config and CLI accepted; initialization, output, policy and report boundary repairs independently reviewed |
| P8 acceptance | Darwin/Linux Go 1.26/1.27 gates, four target builds, consumers, fuzz and benchmarks pass; all six final review repairs independently accepted |

Reference source directories were not edited. The original go-expect directory acquired parallel edits after freezing; those remain separate. `dependencies/go-expect-source.tar.gz` plus the compatibility patch reproduces the isolated candidate without overwriting that workspace.

A real localhost sshd and installed worker validated two foreground master connections and verified upload/archive/cleanup. Synthetic device conversations, HTTP/TLS and temporary PostgreSQL checks remain distinct from real-device evidence. Details and limits are recorded in [verification.md](verification.md).

Review artifacts and source diffs remain under `.superpowers/sdd/implementation-plan/` to preserve the complete review process separately from the source commit.

## 2026-10-26: automatic device output encoding

- Reproduced H3C/H3C wireless GB2312/GBK Chinese configuration failing with `invalid_output_encoding`. Added pinned `github.com/saintfish/chardet` at `v0.0.0-20230101081208-5e3ef4b5456d` and conversion through the existing `golang.org/x/text` dependency.
- Every connector command now automatically checks its complete response and returns UTF-8 text, without new CLI flags, configuration settings or connection options. Valid UTF-8 takes priority; non-UTF-8 detection is bounded to 64 KiB from the first non-ASCII byte and constrained to the GB18030 family. Full-response conversion rejects malformed/truncated sequences rather than silently inserting replacement characters. Detection remains heuristic when bytes have multiple valid interpretations.
- Raw responses remain available. Legacy terminal edits run on original bytes before visible text conversion; decoded output retains the byte budget. Backups use UTF-8 bytes and SHA-256, and failed decoding leaves previous backups intact. Logs convert complete lines and redact UTF-8 and encoded secret variants before and after rendering. The existing Fleet invalid-encoding error and failed-step semantics remain unchanged.
- Regression evidence covers default API and CLI use, ASCII/UTF-8/GBK/GB18030, one-byte transport fragments, paging, Chinese after a large ASCII prefix, native-byte terminal edits, corrupted input, previous-backup preservation, decoded-size limits, secret redaction and backup hashes.
- Final local checks passed with Go 1.27.3: `sh scripts/check` (workspace tests, complete race suite, vet, frozen expect race suite and command builds), `go mod verify`, independent consumer/outside-checkout CLI verification with `GOTOOLCHAIN=local`, and `govulncheck v1.9.0` (`No vulnerabilities found`). Module tidy was checked using a temporary modfile with the existing frozen expect candidate; no replacement was added to the published module file.
- These checks did not contact a production switch or deploy to a device. The original go-expect working tree was not edited.

## 2026-10-26 unification

Plan: docs/2026-10-26-unification-plan.md. Paired worktrees at ../go-expect and this checkout; bases c66cbd2/a25b345.
Ruling: work in new paired feature worktrees; do not commit or publish. Preserve original compatibility worktree.
Ruling: a published compatible dependency cannot be created without publication authorization; retain the release gate and verify explicit local paired consumers, never label them published-dependency evidence.
Preflight: compatibility port must preserve standalone core fixes; canonical connector consumes the port before its API migration.

User correction: consume github.com/gatework/go-expect directly; no local replace or cross-repository go.work. Temporary setup workspace removed before implementation. Resolve GitHub dependency availability before migrating consumer APIs.

Generic-core milestone ready in sibling worktree: compatibility regressions RED on absent APIs; close-handler and cleanup-cause regressions RED then GREEN; full script/ci (race/vet/fuzz/bench/example/self-source consumer), lint and vulnerability scan passed. Fresh review found SendAll suffix reconversion; allocation regression failed at 4112 allocations and passed after one-buffer fix, followed by full CI. No other Important/Critical findings.
Independent net-connector changes verified without any local module replacement: archive blocked Send/Expect/Receive cancellation tests RED then GREEN using selected actual archive sources; CRLF parsing and Longest/POSIX snapshot tests RED then GREEN using actual connector/types.go. Full consumer build awaits a GitHub-accessible compatible dependency.

Archive channel ownership extracted into channel.go. Actual-source tests first reproduced lost primary+cleanup causes and unclosed partial launcher return, then passed under race after safe wrapping and cleanup fixes.
Awaiting explicit answer to GitHub dependency delivery question. No local dependency replacements/workspace remain in this worktree, no GitHub mutations performed. Remaining plan work: canonical Device cancellation/terminal Close/recovery and panic guards; attempt evidence; key authentication/line ending migration; shared save confirmation; policy/status types and naming migration; consumer docs/tooling/full integrations and final consumer review. These are NOT complete.

## 2026-10-27: authorized GitHub delivery

The user explicitly authorized committing and pushing both repositories. This supersedes the earlier no-commit/no-push ruling for the paired unification worktrees; the original nested compatibility worktree remains preserved as historical WIP.

The current connector changes implement terminal Close/cancellation and explicit recovery, failed-attempt evidence, key authentication and line endings, shared save validation, API naming and typed policies, archive channel cleanup, and migration documentation. go-expect resolves directly from the immutable GitHub version pinned in go.mod with GOWORK=off and no dependency replacement.

Fresh local verification passed: connector full race/shuffle suite, go vet, goimports, go mod verify, and the independent consumer/outside-checkout CLI check. The paired go-expect script/ci and actionlint checks also passed. These checks do not establish remote CI, full cross-platform integration, final implementation review, real-device behavior, or a formal release.

## 2026-10-29: foundation quality pass

Approved plan: [2026-10-29-quality-plan.md](2026-10-29-quality-plan.md). Worktrees relative to `/Users/crate/Works/go`: `.worktrees/connector-unification/go-expect` (base `3d3b1ef`) and `.worktrees/connector-quality/go-net-connector` (base `c68c42b`). Existing fleet migration and archive/validation WIP was preserved and completed; original checkouts remain separate.

Lifecycle fixes own partial factory transports, retain primary and cleanup causes, stop mutating borrowed AfterLogin metadata, and make archive Prepare cancellation responsive. The public surface now includes pure validation and immutable rendered response access; `vendors.Open` was removed and fleet orchestration moved to `fleet`. Rendering, disabled logging, rule compilation and bounded embedded TextFSM compilation were optimized. go-expect receives OS scratch reuse, incremental literal-All matching and public Redactor improvements without changing arbitrary regexp matching or the polling architecture.

Independent review found and resolved rendered-prompt/configuration evidence mismatch, overlapping parent-cancellation/Goexit cause loss, and a quadratic long-secret redaction boundary path. Regression checks and independent source re-review are complete; no source-review blockers remain. Both repositories passed Go 1.26/1.27 macOS/Linux race/vet checks and fixed-count fuzzing. Connector full CI also covered the installed worker, temporary PostgreSQL, public source consumer and current firemon adapter contracts. Go 1.27.3 vulnerability scans passed after the older host toolchain was identified separately. The [verification report](2026-10-29-quality-verification.md) records raw benchmark artifacts, slower cases, repeated output measurements and delivery limits. No current authorization to commit, push, publish or deploy is inferred from the historical delivery section above.

## 2026-10-29 second refinement

Completed the [seven-task refinement](2026-10-29-refinement-plan.md): admitted
callback contexts and typed login state; immutable failed/accepted response
evidence; preserved simultaneous errors and primary diagnostics; reusable vendor
definitions and plan capabilities; output sharing; and connector forwarding for
expect's runtime read deadlines/PTY normalization. The paired expect worktree
adds the runtime read pump, owned Poll fence, receive-tail release and incremental
long-rule redaction. No dependency replacement or version change was made.

Both independent scoped reviews and final integration review passed. All final
platform gates, race/vet, fuzz, lint, vulnerability and source-consumer checks are
recorded in the [refinement verification](2026-10-29-refinement-verification.md).
Ten-sample serial comparisons passed the final performance gate after correcting
an intermediate redactor allocation regression and a heap measurement baseline.
The full ten-thousand-device plan is about 97% faster; generic command timing is
unchanged, configuration allocation drops about one full normalized output copy.
Expect's idle resource, PTY, redaction and receive-storage results are reported
separately. Original WIP is preserved; changes remain local and uncommitted.
