# Connector second-refinement review

Review target: `/tmp/connector-refinement-20261029/connector-review.diff`, against `/tmp/connector-refinement-20261029/connector-baseline`, not Git HEAD. Source checkout: `/Users/crate/Works/go/.worktrees/connector-quality/go-net-connector`.

Scope: refinement-plan tasks 1, 2, and 6, plus connector transport forwarding from task 3. The original findings below describe the submitted review diff. The final assessment includes an independent recheck of the coordinator's subsequent fixes in the source checkout.

## Strengths

- `WithOperation`, `AfterLogin`, and `ConfigStrategy.Finish` now receive the admitted cancellation context, and internal topology/transfer callers use that supplied context.
- Command response conversion occurs before device-rejection checks. Accepted steps, failed attempts, and receipt callbacks share private response data; getters do not retry conversion, and public formatting/JSON retain metadata-only behavior.
- Result slice snapshots preserve the command accessor as the mutable snapshot boundary. Static runtime patterns are built from private declarations, while dynamic command rules do not enter an accumulating cache.
- The finite vendor catalog copies mutable regex declarations and slices, and constructs new configuration-strategy state per collection. NewPlan capabilities are cached only within one plan.
- The dual hook/receipt regression tests check both discoverable causes, primary connector classification, exactly-once receipt delivery, and absence of later commands.
- Deadline/error-normalization forwarding is capability-based and keeps the transport owner separate from session cleanup.

## Issues

### Critical

None found.

### Important findings in the original diff — both resolved on recheck

1. **Login callback receipts lose the existing Rendered contract — RESOLVED.**

   Locations: `connector/types.go:174-179`, `connector/device.go:256-275`, and `connector/device.go:717-718` in the submitted candidate.

   `read` constructs a response whose text errors are all `ErrIncomplete`. Command execution calls `responseData.convert`, but `connectOnce` publishes its login `StepResult` to `AfterLogin` without conversion. Previously `StepResult.Rendered()` rendered the received output on this path; it now returns the cached `ErrIncomplete` even for a complete, valid UTF-8 login.

   A callback that reads the login banner with the existing `Rendered()` accessor can consequently turn a successful login into `after_login_failed`. A public-API net.Pipe probe received `banner\nsw#` and printed:

   ```text
   login raw="banner\nsw#" rendered="" incomplete=true
   connect=connector: after_login_failed (login) incomplete=true
   ```

   Probe: `/tmp/connector-refinement-20261029/review-login-repro.go`.

   Populate and freeze text conversions before publishing the login receipt to `AfterLogin`. Preserve the ability of raw-only callbacks to accept a response whose text conversion fails; eager conversion should record the error rather than unconditionally change the historical login acceptance rule. Add a regression for the existing login `Rendered()` accessor and a raw-only callback with invalid text.

2. **Preserving cleanup errors can replace the primary storage diagnostic with a secondary connector diagnostic — RESOLVED.**

   Locations: `connector/configuration.go:184-187`, `internal/cli/cli.go:409-411`, `internal/cli/tftp.go:140-142`, and `fleet/diagnostic.go:71-127`.

   The new joins correctly preserve both error causes, but `diagnose` searches the entire tree for `*connector.Error` before looking for `*storage.WriteError`. When the primary is a storage write failure and a secondary cleanup/finalization error is a connector error, the secondary sets `ce`; the storage branch then refuses to restore the primary error code/type because `ce != nil`.

   A narrow overlay regression joins primary `storage.WriteError{State:not_committed, Phase:write, Cause:ENOSPC}` with secondary `connector.Error{Code:backup_finalize_failed, Phase:backup}`. It fails with:

   ```text
   ErrorCode: backup_finalize_failed
   ErrorType: Net::Connector::BackupFinalizeError
   Phase: backup
   UnderlyingType: Errno::ENOSPC
   ArtifactState: not_committed
   ArtifactPhase: write
   ```

   The expected primary diagnostic is `file_write_failed` / `Net::Connector::Storage::AtomicFile::WriteError`. The returned error chain still contains both causes; this is a report-classification defect, not loss of `errors.Is` evidence. The same ordering concern applies across connector, transfer, and archive wrappers.

   Probe: `/tmp/connector-refinement-20261029/review-classification-test.go`; overlay: `/tmp/connector-refinement-20261029/review-classification-overlay.json`.

   Select the primary semantic failure before diagnostic classification, retaining the original joined error for callers. Peeling generic unary wrappers and selecting the first child of primary-first joins is a small local approach. Treat `archive.Error` as a generic safe envelope: `archive/channel.go:41-43` wraps operation-plus-cleanup joins in it, so stopping there can still expose a secondary typed error. Peeling `protocolFailure` to its primary archive sentinel preserves the existing final archive diagnostic because that branch overwrites ErrorType and does not populate UnderlyingType. Cover storage→connector, connector→transfer, transfer→connector, and a generic envelope in focused classification tests.

### Minor

The API migration document still describes the round-one API and has no second-refinement section documenting the changed callback signatures or the new error-aware `Response` accessors. Update it as part of delivery; the implementation plan alone is not a consumer migration guide.

## Validation

The following existing tests passed locally under `go1.27.1 darwin/arm64`, `GOWORK=off`, and `GOFLAGS=-mod=readonly`:

```text
go test ./connector ./fleet -run 'Test(HookAndReceiptErrorsBothSurvive|CloseCancelsAdmittedCallbacks|RejectedResponseRetainsVerifiedEvidence|UnavailableResponseConversionIsExplicit|PlanLooksUpCapabilitiesOncePerVendorPerPlan)$' -count=1
```

The two independent probes above confirmed the findings. Temporary probe files and the overlay live outside the checkout. No source, index, branch, or baseline files were edited; no benchmarks or heavy gates were run.

The review also traced private pattern compilation, command snapshots, vendor declaration copying, strategy creation, output sharing, error wrappers, and transport forwarding. The source module retains Go 1.26.0 and a pinned go-expect dependency without a local replacement.

## Remediation recheck

The coordinator changed `connectOnce` to populate response conversion immediately before `AfterLogin`, while leaving any conversion error for the callback to inspect. The original public-API login probe now reports:

```text
login raw="banner\nsw#" rendered="banner\nsw#" incomplete=false
connect=<nil> incomplete=false
```

The coordinator also added `primaryDiagnosticError`, which peels generic wrappers and follows the first branch of joined failures before classification. Connector/storage/transfer semantic error carriers remain intact, and the full returned error tree is unchanged. `archive.Error` is intentionally peeled as a generic safe envelope. Source inspection and the original overlay regression confirm the primary storage diagnostic is restored.

The independent recheck passed these focused tests, including the original failing overlay test:

```text
TestAfterLoginReceivesVerifiedResponse
TestHookAndReceiptErrorsBothSurvive
TestCloseCancelsAdmittedCallbacks
TestRejectedResponseRetainsVerifiedEvidence
TestUnavailableResponseConversionIsExplicit
TestPlanLooksUpCapabilitiesOncePerVendorPerPlan
TestDiagnosticCleanupDoesNotReclassifyStorageFailure
TestDiagnosticPreservesPrimaryAcrossMetadataTypes
TestReviewStoragePrimaryWithSecondaryConnectorFailure
```

The recheck used `GOWORK=off GOFLAGS=-mod=readonly go test -overlay /tmp/connector-refinement-20261029/review-classification-overlay.json ./connector ./fleet -run '<the exact names above>' -count=1`, followed by the original login probe. It did not run heavy tests or benchmarks.

## Declined to judge

- go-expect runtime-polling internals, receive-storage lifecycle, and dense-redaction implementation: outside this connector review bundle and assigned scope.
- Full race/vet/lint/fuzz/worker/PostgreSQL gates, cross-platform/toolchain matrices, benchmark statistics, and resource-scaling acceptance: coordinator evidence was explicitly still in progress; absence of those results is not reported as a code defect.
- Firemon source-consumer changes not present in this diff: their implementation and integration proof require their separate source bundle.
- Published dependency adoption, release artifacts, remote CI, and real-device behavior: not authorized or established by this local source review.
- Broader pre-existing paths that still discard independent errors, such as the topology CLI's conditional close assignment: unchanged from the supplied round-one baseline; they should not be mistaken for a regression introduced by this diff.

## Assessment

**Spec compliance: PASS for the reviewed scope after remediation.** Both original correctness findings are resolved and independently rechecked. The separate full-gate/performance/source-consumer evidence remains the coordinator's delivery work.

**Code quality: PASS after remediation.** No remaining critical or important findings. The nonblocking migration-document note above remains; snapshot ownership, cache scope, cancellation, and the focused fixes have clear boundaries.

**Review blocker summary: None remaining.** The two original blockers are fixed. This verdict is local code review and focused-test evidence, not a replacement for the coordinator's required gates or performance acceptance.
