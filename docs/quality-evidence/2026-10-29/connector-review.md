# Connector quality review, tasks 2–4

Reviewed worktree: `/Users/crate/Works/go/.worktrees/connector-quality/go-net-connector`.
Baseline: `c68c42b`; preserved fleet-migration WIP included. Reviewed the supplied diff, current core source, newly added validation/lifecycle/fleet/TextFSM tests, migrated callers, and the pinned gotextfsm v1.2.0 implementation. No existing test suite or benchmark was rerun. Root-reported race/shuffle and 100,000-case terminal differential fuzz results are supporting context, not independently reproduced evidence.

## Findings

### P1 — RESOLVED: Rendered config evidence is compared with an unrendered prompt

Location: `vendors/config.go:74`, `vendors/config.go:85-86`.

Trigger: an echo-only configuration response whose final prompt has trailing space/tab, NUL, or CRLF, all accepted by the strategy prompt regex in `connector/device.go`. For example, `show running-config\r\nrouter# ` renders as `show running-config\nrouter#`, while `StepResult.Prompt()` retains `\nrouter# `. `renderedBody` cannot remove that suffix, removes the command echo, and returns `router#` as purported body.

Impact: `Finish` accepts an empty response for affected dialects (including Cisco and Hillstone). `collectConfig` initializes `hasBody` to true for custom vendor strategies, so it does not reject this false evidence afterward. Backup can replace a valid prior configuration with command/prompt-only content and report a committed successful backup. Before this change, `configBody` removed the raw prompt before rendering, so this is introduced by render reuse.

Minimum fix: normalize/render the prompt into the same representation as the cached visible text before checking/removing the response boundary. Handle the terminal line trimming and trailing newline cases consistently. Keep the PAN/Hillstone returned configuration path on its current raw/output semantics. Add echo-only and prompt-only collection cases with trailing space, tab, NUL and CRLF, including a regression that confirms no configuration/backup artifact is produced.

Evidence: deterministic source trace; existing `TestCollectionDialectOutputsAndEchoOnly` uses CRLF between lines but no whitespace after the prompt, so it misses this case. Reproduction was coordinated with root before any new test run.

### P2 — RESOLVED: Parent cancellation can hide an abnormal worker exit from the returned batch error

Location: `parallel/pool.go:59-63`.

Trigger: a callback cancels the parent context, then invokes `runtime.Goexit`, or it exits abnormally during cancellation cleanup after the parent deadline. The worker defer sets `results[index].Err = ErrWorkerExit` and calls `cancel(ErrWorkerExit)`, but `context.WithCancelCause` retains the first cause. Since the parent cancellation won, `context.Cause(workerCtx)` is `context.Canceled` or `context.DeadlineExceeded`, and Map returns only `ctx.Err()`.

Impact: the advertised abnormal worker failure disappears from the top-level batch error. Fleet deliberately discards Map's result slice, so its caller cannot discover the sentinel through the pool result either. Completed receipts remain preserved and dispatch already stops, but failure diagnosis depends on cancellation timing.

Minimum fix: track actual abnormal exit independently from the cancellation cause (for example, a dedicated atomic boolean set in the existing defer), then return `errors.Join(ctx.Err(), ErrWorkerExit)` when that flag is set. Add a deterministic `cancel(); runtime.Goexit()` regression and verify both sentinels remain discoverable with `errors.Is`.

Evidence: deterministic source trace; current Goexit tests use an uncanceled parent and therefore do not cover first-cause precedence. No reproduction was run by this reviewer.

## Plan compliance and quality

- Device partial transport ownership now transfers before checking factory error, and failed Close ownership remains on the Device for retry. SSH recovery is blocked by cleanup failure or a retained transport, avoiding replay while cleanup is incomplete.
- Explicit construction lifecycle is adopted: `vendors.Open` is removed and source callers use construction plus owned Close paths. Search found no remaining source references to the removed API or moved netdisco fleet APIs.
- `AfterLogin` copies only `ha_state` and no longer retains or clears the borrowed map.
- Validation is pure, shared by constructors/bootstrap, preserves explicit empty legacy arguments, and snapshots caller-owned mutable declarations afterward.
- Interactive primary/cleanup/terminal-restore error joining is materially improved; typed-nil output rejection is covered. Borrowed input ownership remains intact.
- Response rendering is immutable and reused by StepResult snapshots; raw and output storage share strings until excluded pager material requires separation. Attempt snapshots retain their prior wire-response meaning. The vendor boundary bug above must be fixed before claiming complete output compatibility.
- Terminal string fast path remains conservative for terminal control bytes and malformed UTF-8, falling back to byte-oriented rendering. Byte limit checking is before whitespace trimming. PAN/Hillstone clean paths still use original output, preserving their deliberate serialization behavior.
- Echo logging skips pipeline allocation when disabled while byte-budget counting still runs. Secret handling and safe diagnostic output remain intact in the changed paths.
- Archive validates typed-nil endpoints, required Close and actual deadline support before protocol bytes; underlying deadline failures are retained. Prepare uses a cancelable serialization gate, accounts for admitted initialization, checks closed/canceled state before local side effects/fast paths, and no longer holds its state mutex across network operations. Existing failed-close retry ownership is retained.
- Fleet migration preserves report/diagnostic schema and existing compatibility vocabulary. Outcome registration before observer invocation and deferred observer mutex release preserve completed artifacts on observer Goexit. Worker panic is not swallowed. Parent-cancellation overlap needs the P2 fix.
- Embedded TextFSM compilation is bounded by the embedded filesystem. Custom source is compiled without a global cache. In pinned gotextfsm v1.2.0, pristine compiled Values have nil mutable value/filldown state; `maps.Clone` gives each parse its own map of value structs, while shared Options/state/regexp data are read-only during parse. A fresh ParserOutput prevents cross-call record/state reuse. No cache state-leak issue found.
- Some constant regex construction remains in login and vendor helpers, outside the newly precompiled diagnostic/rule sites; this is a possible later optimization, not a correctness blocker.

## Assessment

The ownership, validation and caching design is generally cohesive and stays within tasks 2–4. Request changes for the two findings above. The P1 blocks configuration safety; P2 blocks complete abnormal-exit reporting. Task 5 evidence, benchmark attribution, consumer/Firemon integration, OS/toolchain matrix and documentation completion remain root-owned and are not certified by this review. No commits, edits to reviewed core files, or broad validation runs were performed by this reviewer.

## Fix review follow-up

Both findings are resolved in the subsequently inspected source:

- `configStrategy.Finish` now strictly renders and trims the prompt before visible-body validation; `renderedBody` trims the visible response before removing the normalized suffix. PAN/Hillstone cleanup still receives the original output and raw prompt. The new fixture covers Cisco trailing space/tab/CRLF, Centec/Hillstone space, and H3C NUL. Prompt-only text also becomes an empty body under this normalization. No replacement of raw/output semantics was introduced.
- `parallel.Map` records actual abnormal exits in a separate `atomic.Bool` and joins `ErrWorkerExit` with any parent error after workers finish. The added `cancel(); runtime.Goexit()` fixture asserts both causes and no subsequent dispatch.

Root reports RED reproduction and GREEN targeted race tests; this reviewer independently inspected the fixes and test assertions without rerunning them. The earlier request-changes verdict is cleared for tasks 2–4; no remaining blocking source-review finding identified.

Final disposition: **remaining source-review blockers: none** for tasks 2–4. The findings above are historical and their fixes are cleared in the follow-up. Root reports only benchmark, boundary-test and documentation additions since that source clearance.
