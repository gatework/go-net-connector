# Foundation module quality plan

Approved in conversation 2026-10-29. Baselines: go-expect 3d3b1ef; go-net-connector c68c42b.

Implementation and independent source review are complete locally. See [verification and measured tradeoffs](2026-10-29-quality-verification.md); publication and dependency adoption remain separate.

## Global constraints
Preserve concurrent WIP. No commits, pushes or releases. Go 1.26 minimum. go-expect owns generic stream matching and I/O; connector owns devices. Retain received evidence and committed receipts; no command replay. Preserve wire bytes, strict internal UTF-8 conversion, vendor configuration formats, safe diagnostics and externally compatible reports. Use GOWORK=off and pinned dependencies; no module replacements.

## Task 1: Generic engine
OS polling pump owns reusable scratch; transfer compact small reads and exclusive large blocks. Incremental per-Expect state for All trees consisting only of literals; preserve generic regexp semantics. Optimize public Redactor allocation with independent returned bytes and bounded retained suffix. Add benchmarks for generic/OS paths, patterns, redaction, idle sessions and retained tails; public Redactor differential fuzz. Do not replace 10ms polling architecture in this iteration.

## Task 2: Device lifecycle and response pipeline
Own partial factory returns until cleanup succeeds. Remove vendors.Open; explicit New/register Close/Connect lifecycle. Retain interaction primary/cleanup/restore errors. Extract HA state without retaining/clearing borrowed AfterLogin map. Pure Profile.Validate, ConnectionConfig.Validate/ValidateOptions reused by constructors. Immutable reusable response rendering with StepResult.Rendered() (string,error); preserve Raw/Output and Attempt meanings. Avoid duplicate raw/body growth when identical. Skip unnecessary diagnostic creation when no CommandErrors; precompile constants. Reuse rendering in vendor and configuration paths, preserving PAN/Hillstone semantics. Only build echo logging pipeline when enabled; counting independent of logging.

## Task 3: Archive lifecycle
Validate nil/typed-nil endpoints, Close, and deadline support before protocol work, retaining causes. Prepare checks cancellation/closed state before side effects and fast paths, and supports cancelable serialized initialization without holding mutex across network work. Preserve channel cleanup retry ownership.

## Task 4: Fleet and parsing
Complete existing netdisco-to-fleet WIP, migrate callers/CLI/bootstrap/consumer script; preserve report/diagnostic formats. Register Outcome before callback, defer callback unlock. Detect worker abnormal exit, cancel batch and preserve completed receipts; panic propagates. Precompile rules and bounded embedded TextFSM templates with per-call independent mutable Values and ParserOutput; no custom-source unbounded cache.

## Task 5: Verification and documentation
Regression tests before fixes. Race/shuffle, vet, existing lint, fixed-count fuzz, Go 1.26/1.27 and Linux/macOS evidence reported separately. Consumer and firemon contracts. At least ten serial benchmark samples for comparisons, separate cumulative allocation/retained heap/peak RSS and actual device evidence. Document migrations, worktree paths, benchmark evidence and unavailable integrations. Publication and adoption of unpublished cross-module changes remain separate.
