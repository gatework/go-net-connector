# Second foundation refinement — 2026-10-29

Approved conversation plan. Baseline is the CURRENT round-one WIP, not Git HEAD. Preserve every existing change, no commits/push/releases, no local replace/go.work. Go 1.26 API minimum. Generic streams/PTY/matching belong to expect; device/vendor/auth/save belong to connector. Preserve exact Raw, failed Attempts, completed receipts, no command replay. Metadata-only diagnostics. Cooperative callbacks; Close interrupts owned I/O but never waits for user callbacks. Source integration is distinct from published dependency adoption.

## Task 1: Cancellation and combined errors
WithOperation(ctx, func(context.Context,*Operation) error) passes admitted Close-linked context. AfterLogin becomes func(context.Context,StepResult)(LoginState,error), LoginState{HAState string}. ConfigStrategy.Finish(context.Context,Result)(string,error). Migrate internal callers and source-consumer fixtures. Preserve operation+receipt/log/restore/cleanup errors through safe wrappers and errors.Is/As; retain primary classification, exactly-once callbacks, no later commands/replay.

## Task 2: Immutable response evidence
Private-field Response shared/frozen by StepResult.Response and Attempt.Response. Raw(), RawPrompt(), Encoding(); Output(), Rendered(), Prompt() return (string,error). Preserve validated conversion even if rejected by device or later callbacks. Missing conversion returns ErrIncomplete; invalid conversion preserves cause; raw always retained. No lazy re-detection/retry in getters. Attempt.Complete means prompt only. Keep convenient legacy accessors and safe formatting/JSON. Result.Steps clones only outer slice; Command getter remains snapshot boundary. Source firemon contract uses verified evidence with explicit raw fallback.

## Task 3: OS runtime polling
One owning pump: choose RawConn.Read + unix.Read/EAGAIN(false) when SetReadDeadline works; fallback existing backend only at construction. Forward deadlines through owned PTY/process/wrappers. Explicit Poll has one outstanding request, mutex-protected publication/deadline wake, pump clearing deadline and publishing prior bytes before immediate readiness/read and ack. Canceled request remains pump-owned until ack; Close joins pump, no extra FDs or fd escape. Optional NormalizeReadError(error)error capability handles PTY EIO->EOF through wrappers without changing ordinary EIO/control errors.

## Task 4: Receive storage lifecycle
Nil exhausted pending/staged in exclusive receive path. Track backing allocation size; at operation return detach once if >=64KiB and remainder<=one quarter. No per-continuation copies and no concurrent Close clearing matching state. Preserve timeout/cancel remaining bytes and detached results.

## Task 5: Dense public redaction
Rules >256 bytes compile KMP state per rule scope; <=256 keep bytes.Index. Merge sequential covered intervals and only mark newly covered range. Reuse bounded suffix; large transient storage detaches. Preserve Finish prefix masking, adjacent/overlapping replacement union, SetSecrets, binary data, output ownership. No unsafe/global cache/pool. Differential fuzz and dense repeated-pattern scaling required.

## Task 6: Reusable definitions and output sharing
Finite built-in vendor catalog compiles static regex once, Profile returns independent mutable declaration. New builds immutable private runtime matchers so fixed profile rules avoid per-command FromRegexp conversion. Dynamic Command rules remain snapshotted. NewPlan caches capability lookup once/vendor/plan (not globally). Strategies independent per collection, user closures borrowed. Cisco/NXOS unchanged-header returns existing string. Single-step Result.Output shares existing string; multi-step precomputes size. No general regexp truncation or worker-pool rewrite.

## Task 7: Evidence and delivery
Failing regressions first; independent review. Go1.26/1.27 Darwin/Linux race/vet, existing lint/fuzz/worker/PG and source Firemon contracts. Vulnerability scan with patched toolchain. Serial >=10 samples, same toolchain, interleaved when needed: full NewPlan, real PTY, 1/64/256/1024 idle sessions CPU/threads/FDs/heap/Poll-Close latency; warm tail and held/discarded-result memory. No periodic wake/thread growth on runtime backend; memory scales with remainder; long dense redaction near-linear; one full-copy removed for unchanged config. Statistically significant >5% regressions in other key performance workloads reject performance change until resolved. Keep original measurements and unsupported integration labels. Publication/adoption is not part of this work.
