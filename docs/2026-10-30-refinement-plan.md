# Third foundation refinement implementation plan

Approved conversation plan, 2026-10-30. Execute the complete round; public API migrations are authorized where specified below. The user subsequently authorized committing, pushing and releasing both modules, including pinning the newly published expect version in connector. Temporary public visibility for connector is authorized only if private Actions is billing-blocked; restore private after asset verification.

**Goal:** Fix transport ownership/error propagation and reduce repeated computation and retained memory in sessions, inventory and fleet operations.

**Architecture:** expect owns generic stream matching and filtering; connector owns device operations, transport processes, trust persistence and vendor evidence. Keep explicit ownership and cooperative cancellation. Baselines are expect `6e4e2dc` and connector `59c1af1`; source snapshots and execution ledger are in `/tmp/connector-round3-20261030`.

**Constraints:** Go 1.26 minimum; GOWORK=off and pinned modules; no replace/go.work, new production dependencies, unsafe or global pools. Preserve raw evidence, failed attempts, completed receipts and command non-replay. Keep existing worktrees and concurrent work.

## Correctness tasks

- [x] Authentication: optional transport hook becomes `Authenticated(context.Context) error`; reject legacy-only hooks with `invalid_transport` wrapping ErrInvalid, after acquiring cleanup ownership and before login I/O. Publish immutable trust pointer; pass ctx explicitly to trust operations. Close stops admission/cancels active commit, interrupts I/O, joins owned work and removes files only after users have exited. User callbacks remain cooperative and are not joined by Close.
- [x] Process startup: retain a partial owner whenever initialization fails with acquired resources, particularly cancellation after Start. Attach trust on success/error, preserve original and cleanup causes, and permit subsequent Close retry after cleanup failure.
- [x] Writes: preserve classified metadata through a safe wrapper whose cause is the original complete error tree, including WriteError progress and secondary cancellation/cleanup errors.

Storage cancellation remains cooperative: cancellation observed by the final pre-rename check aborts; a later concurrent cancellation may race with a committed rename. Never roll back committed storage. Preserve any storage.WriteError receipt/cause.

## Performance tasks

- [x] Session filtering: incremental long-rule KMP and bounded short-rule scans, merged covered intervals, true ambiguous suffix retention, additive rule initialization from un-emitted bytes, hidden-byte preservation, bounded retained capacity/Finish release, and no filter work without sinks. Retain per-byte `*` semantics separately from public Redactor.
- [x] Captures: keep indices until the winning Case, then detach captures before guards/consumption/callback. All computes extents without capture strings. Preserve ordering, empty-group and POSIX/Longest behavior.
- [x] Fleet: exact outcome allocation and struct{} Map results, preserving the existing scheduler, task snapshot, cancellation/Goexit and callback contracts.
- [x] Inventory: decode known HTTP fields once from RawMessage and map PG columns directly to owned strings; eliminate JSON roundtrips. Preserve exact field names, optional null, required IP, unknown columns, duplicate-key/column behavior, budgets and all-or-nothing results. Preserve old PG invalid-UTF8 replacement behavior; check cancellation during mapping.
- [x] TFTP: finite private precompiled vendor completion catalog; retain exact success/failure precedence, dynamic target rules and receipts.

## Review focus and checks

Regression tests first for auth/Close ordering, startup cancellation with failed cleanup, full write error trees and legacy hook rejection. Existing ownership/cancellation suites remain mandatory. Extend private-filter differential fuzz, capture edge cases, inventory differential/PG tests, fleet Goexit/callback receipts and TFTP spoof/late-success cases.

Serial interleaved benchmark samples (at least 10, same toolchain/hardware): filter chunks and long/mixed rules plus retained heap, competing regex captures, fleet 1k/10k/100k, inventory 500/10k/100k and local HTTP, TFTP 1/1k/10k lines. Require measurable direct benefit and no statistically significant >5% regression in relevant prior workloads; discard losing performance-only changes.

Run complete CI, race/shuffle, vet, lint, fixed-budget fuzz, Darwin/Linux Go1.26 and Go1.27.3, non-skipped temporary PostgreSQL and built worker checks. Run the paired-source verifier separately from pinned dependency consumers. Independent review, API migration documentation and final evidence report complete the round.
