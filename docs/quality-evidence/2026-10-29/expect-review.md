# go-expect independent review

Worktree: `/Users/crate/Works/go/.worktrees/connector-unification/go-expect`.
Baseline: `3d3b1ef`. Scope: task 1 production diff, new regression/fuzz/benchmark sources, OS publication/consumption ownership, and `/tmp/connector-quality-20261029/task1-report.md`. Read-only review; no tests, builds or benchmarks executed during root's connector measurement work.

## Finding

### P2 — RESOLVED: Safe-boundary detection adds quadratic work for repeated long secret prefixes

Location: `streaming_redactor.go:153-155` in the reviewed snapshot.

Trigger: construct a secret `strings.Repeat("a", N) + "b"`, append `a^N`, then append another `a^N`. There is no complete secret in the pending window or incoming chunk. The crossing loop visits N potential starts. At each start, the pending suffix matches a secret prefix, then the incoming suffix comparison fails near the final `b`. Total compared bytes sum to O(N squared), while the Redactor mutex is held. Public secret lengths are unrestricted. The existing general matcher does not need this exhaustive sequence of suffix comparisons.

Impact: a large diagnostic secret such as a long multiline value can turn an otherwise safe append into a substantial CPU stall. This is a new pathological performance regression in an optimization-focused change; existing fuzz caps secrets at 64 bytes and regular benchmarks use short secrets, so neither covers it.

Minimum fix: conservatively route ambiguous/long boundary candidates to the established general matcher rather than comparing every candidate prefix, or implement linear boundary matching. A fixed upper bound on fast-path secret lengths also bounds the new quadratic factor without changing public acceptance of longer secrets. Suggested fixture: the repeated-prefix two-append sequence above, plus a completed crossing match and Finish-prefix masking using the same long secret. Suggested later serial benchmark: several increasing N values to verify scaling. No benchmark run requested during the active root window.

Status: resolved by the long-secret fallback guard; final source re-review is recorded below. The original finding and trigger are retained for traceability.

## Ownership and semantic review

- OS read scratch is local to exactly one pump. Idle Poll allocates none; short reads allocate/copy a compact detached slice; a read at least half the block transfers the full backing allocation and nils scratch. Empty/error reads do not publish borrowed bytes. Existing RawConn.Control and error normalization paths remain unchanged. Session pending/staged slices cap ownership correctly when taking the transferred event, so the next read cannot overwrite consumer-visible data.
- Literal-All state is per Expect invocation. Flattening nested pure-literal trees preserves the minimum first-hit start and maximum first-hit end because the existing nested operation computes the same associative min/max. Duplicate literals may share the same matching bytes as documented. Unmatched leaves rescan the required length-minus-one overlap; matched leaves cache an earliest position that cannot change as bytes append. Every consumption, including streaming fallback and continuation, resets every leaf; a subsequent Expect allocates fresh state. Mixed/regexp/EOF trees remain on the stateless matcher, so regexp terminal/UTF-8/capture semantics do not change.
- The public Redactor no-rule path clones input; safe-path output is separately allocated; retained suffix copies do not alias caller input or returned output. General-path output remains detached. The safe path is used only when no retained byte is already hidden and no complete/crossing match exists. It retains the maximum secret length minus one, preserving future match detection and output timing. Masking-run state is reset only when visible bytes are actually emitted, so adjacent masked runs across empty-output chunks remain coalesced.
- SetSecrets finishes the old scope before installing new copied secrets; Finish masks retained prefixes and resets stream state. General-path suffix compaction releases large transient storage. No secret leakage or changed overlapping/adjacent-mask semantics found by source inspection.
- New differential tests use an independent whole-input marking oracle, include binary data and custom replacement, and exercise midstream rule switching. All matching fuzz compares at every prefix and after reset. Allocation tests and OS ownership fixtures cover the intended common paths. Long-secret boundary scaling is the identified gap.

## Plan and evidence assessment

The implementation stays within the requested generic-engine boundary and leaves device behavior to connector. It preserves polling architecture and does not truncate arbitrary regexp input. The task report separates cumulative allocation, suffix capacity, post-GC heap and peak RSS, reports ten serial benchmark samples, and explicitly retains regressions/tradeoffs. It distinguishes macOS runtime evidence from Linux cross-compilation and avoids claims of publication or downstream adoption. Those measurements and test runs were reported by the implementation agent, not independently rerun by this reviewer.

After resolving the long-secret boundary issue and checking the focused regression, no additional source-review blocker is identified. Broader final validation and any need to refresh impacted benchmark measurements remain with root/implement_expect.


## Final source re-review

Reviewed the frozen follow-up code without running tests or benchmarks:

- **Long-secret guard:** `safeChunk` returns false for any rule longer than 256 bytes before entering candidate-by-candidate comparisons. The full public rule remains accepted and the established matcher handles it. The new quadratic factor is bounded by a constant for eligible rules; long-rule append, completed crossing matches, retained-prefix Finish masking, and detached output ownership retain the general-path semantics. The 4,096-byte regression asserts both the fallback and the final output for a completed crossing match and unfinished prefix. The new serial benchmark spans 32/256/4096/65536-byte retained-prefix workloads. Implementation-agent measurements report the 65,536 case reduced from 49.3 ms unguarded to 286 us guarded, near its 275 us baseline; these numbers were not independently rerun here.
- **Optional All state:** ordinary cases again use their original `[]int` offsets. The separate state slice is allocated only after finding a pure-literal All tree; empty state slots route mixed/regexp cases to the original matcher. Consumption clears ordinary offsets and resets all optional leaf states, including streaming consumption. No changed match priority, overlap, EOF, handler or cancellation semantics found.
- **Source consumer:** `script/consumer` copies source into a temporary disposable module snapshot, installs the existing public-API consumer command there, disables workspace resolution, rejects all module replacements, and runs with readonly module mode. The trap removes the snapshot. It does not mutate repository go.mod/go.sum and explicitly labels the result as source-consumer evidence rather than independent published-version adoption. This satisfies the source-consumer boundary; it does not prove an external consumer can resolve an unpublished artifact.

Root/implementer report the final CI, Go 1.26/macOS race and vet, Linux Go 1.26/1.27 race and vet, and Go 1.27.3 vulnerability check passed. Those are reported validation evidence, not reviewer reruns.

**Remaining blockers: none identified in the final task-1 source review.** The P2 above is resolved. Connector tasks 2–4 also have no remaining source-review blocker after their separately reviewed prompt-normalization and worker-exit fixes. Publication, real-device validation and downstream adoption remain separate checkpoints.
