# Go API migration

The generic engine is maintained in go-expect; this module owns the only device
layer. Resolve the pinned GitHub dependency with GOWORK=off. Do not run the old
workspace reconstruction scripts for current builds; retained baselines/patches
are historical audit material.

| Previous API | Current API |
| --- | --- |
| Device.Name / Operation.Name | ProfileName (the profile key, not a hostname) |
| Interaction.Response | Reply (verbatim bytes) |
| hook Executor.Get / Set | Value / SetValue |
| Command.Timeout pointing to zero to poll | Command.Poll = true |
| Interaction.Limit = 0 for repetition | Unlimited = true |
| untyped protocol/trust/state strings | Protocol, HostKeyPolicy, storage.FileState, transfer.Verification |

Command.Timeout nil inherits the configured default; zero disables the relative
command timer. Poll is an explicit immediate readiness operation and rejects a
positive Timeout. Parent context deadlines still apply. Sends remain bounded by
the configured write timeout. Limit zero permits one reply; positive values bound
replies; negative values and Unlimited combined with a positive Limit are invalid.
Built-in vendor rules explicitly preserve their former repetition and reply bytes.

Close is terminal. It cancels the admitted operation and its callback contexts,
interrupts I/O and rejects subsequent valid operations with ErrClosed. It does
not wait for cooperative user callbacks. A failed operation retires its session;
ordinary commands return ErrNotConnected until an explicit Connect succeeds.
No device command is automatically replayed. Constructors are required; do not
copy Device, Session or other owners of synchronization/resource state.

Result.Steps keeps its accepted-main-command meaning and existing JSON format.
Result.Attempts separately records main and hook-issued writes, exact Written
progress, whether a prompt arrived, raw response evidence and Err. Complete is
not device acceptance or persistence. Raw is explicit sensitive access; implicit
formatting and JSON contain metadata only. Snapshot accessors detach mutable
patterns/slices; caller closures remain borrowed and must synchronize shared state.

Profile.ValidateSave(ctx, result) supplies affirmative save evidence. SaveConfig
and topology share it. Missing or negative evidence returns ErrUnconfirmed while
retaining completed steps. Existing confirmed topology save policies moved into
vendor profiles; unsupported firmware evidence is never guessed. Confirmation is
not a claim about physical flash durability after power loss.

ConnectionConfig adds IdentityFile, KeyPassphrase and LineEnding (default LF).
Key passphrases are sent only after an explicit matching prompt, are redacted,
and never enter argv. Command/credential line endings are configurable; declared
interaction Reply values remain verbatim. Copying compiled regexps retains
POSIX/Longest settings; CRLF scripts preserve command bytes and source line numbers.

Archive errors preserve cancellation, primary I/O failures and cleanup causes.
Pool.Close retries retained failed owners. A direct Client.Close should follow
completed/canceled transactions; active work stays owned by its context and Close
returns ErrArchive until that work has returned. Borrowed writers must cooperate
and must not reenter their owning stream/log sink.

## 2026-10-29 quality changes

- `vendors.Open` is removed. Use `vendors.Profile`, `connector.New`, register
  `Close`, then call `Connect`. A connection error never transfers cleanup
  responsibility away from the caller. Repeated `Close` retries failed cleanup;
  successful cleanup is not repeated. Commands are never replayed.
- `Profile.Validate`, `ConnectionConfig.Validate` and `ValidateOptions` perform
  pure checks. The last checks reusable options without requiring a bound Host.
  Constructors validate, normalize defaults and detach mutable configuration.
- `StepResult.Rendered() (string, error)` exposes strict terminal-visible UTF-8.
  Completed steps share immutable derived text. `Raw`, `Output`, failed Attempts,
  accepted Steps and metadata-only formatting keep their existing contracts.
- Planning, batch execution, reports and diagnostics moved from `netdisco` to
  `fleet`; `netdisco.FleetOptions` becomes `fleet.RunOptions`. Inventory sources
  and cache stay in `netdisco`. CLI/YAML/JSON vocabulary is unchanged.
- `parallel.ErrWorkerExit` reports callbacks terminating with `runtime.Goexit`.
  The batch stops admitting work and joins admitted workers. Fleet preserves
  completed outcomes before invoking observers and records abnormal observers.
  Panics still propagate; cancellation and callbacks remain cooperative.

## 2026-10-29 callback and response refinement

Three pre-v1 callback signatures change. Always pass the callback's supplied
context to nested work; it includes cancellation from `Device.Close`:

```go
device.WithOperation(ctx, func(ctx context.Context, op *connector.Operation) error {
    _, err := op.ExecuteCommand(ctx, command, connector.ExecuteOptions{})
    return err
})

profile.AfterLogin = func(ctx context.Context, step connector.StepResult) (connector.LoginState, error) {
    // Parse the explicit response accessors as needed.
    return connector.LoginState{HAState: "active"}, ctx.Err()
}

// ConfigStrategy implementations now use:
// Finish(context.Context, connector.Result) (string, error)
```

`AfterLogin` returns a value instead of a borrowed metadata map. Its errors stop
login. `Finish` receives the admitted operation context. Callbacks remain
cooperative: Close interrupts owned I/O without waiting for application callbacks.

`StepResult.Response()` and `Attempt.Response()` expose an immutable `Response`.
`Raw`, `RawPrompt`, and `Encoding` return explicit evidence. `Output`, `Rendered`,
and `Prompt` return `(string, error)`; the latter two provide terminal-visible
UTF-8. Missing conversion returns `ErrIncomplete`, and invalid conversion retains
its original cause. Accessors never retry detection or decoding. A device-rejected
completed response can therefore retain verified text even though it has no
accepted Step. `Attempt.Complete` still means a prompt arrived, not acceptance.

```go
view := attempt.Response()
text, err := view.Output()
if err != nil {
    text = view.Raw() // explicitly incomplete/invalid text evidence
}
```

Existing Step convenience accessors remain; `Step.Prompt` preserves its historical
UTF-8 wire form. Prefer `Response.Prompt` for verified visible text. Login callbacks
also receive verified response snapshots. Formatting and JSON remain metadata-only.
`Result.Steps` returns an independent outer slice, while immutable responses are
shared; `Command()` still detaches mutable fields. Simultaneous operation, receipt,
logging and cleanup failures remain discoverable with `errors.Is/As`, with the
primary failure controlling fleet diagnostic classification. No command is replayed.

`vendors.Profile` returns independent mutable declarations backed by a finite
compiled catalog. Device snapshots and fixed matchers are private. `NewPlan`
resolves capabilities once per vendor per plan; custom lookup state is not cached
across plans. Each configuration collection still gets its own strategy instance.

Optional source probes are `scripts/verify-firemon-source.py ADAPTER_PATH` and
`scripts/verify-expect-source.py EXPECT_PATH`. They migrate or rebind imports only
inside temporary source copies, without `replace` or `go.work`. The real modules
remain pinned; passing these probes does not establish published adoption.

Example lifecycle (the deferred Close also runs on a failed Connect):

```go
profile, err := vendors.Profile("cisco_ios")
if err != nil { return err }
device, err := connector.New(profile, cfg)
if err != nil { return err }
defer device.Close()
if err := device.Connect(ctx); err != nil { return err }
```

`archive.Client.Close` still returns `ErrArchive` while transactions or Prepare
are active; their caller contexts own cancellation. Pool performs cancellation
and joining. Prepare waiters can cancel without waiting for another network
preflight. Canceled/closed calls do not take the prepared fast path or begin
local setup. Channel endpoints must be non-nil, own a Close function and support
working deadlines; deadline failures preserve the original cause.

## 2026-10-30 transport authentication and cleanup

Custom transports may implement the optional authentication hook with this
signature:

```go
func (t *Transport) Authenticated(ctx context.Context) error {
    return t.persistTrust(ctx)
}
```

`Connect` invokes it after login using the admitted operation context. Propagate
that context to subprocesses, locks and persistence. A transport implementing
the old `Authenticated() error` hook is rejected before login I/O with
`invalid_transport` wrapping `ErrInvalid`; its acquired resources still receive
cleanup. Transports without either hook continue to work.

A `TransportFactory` must return a non-nil owner together with an initialization
error whenever resources still require cleanup. This includes cancellation just
after starting a process. `Device` retains that owner until `Close` succeeds;
failed cleanup remains retryable. Preserve primary and cleanup errors with
`errors.Join` instead of dropping ownership when returning an error.

The built-in process transport closes trust-commit admission, cancels an active
commit and interrupts I/O before joining its internal commit. Temporary trust
files are removed only after that commit exits. `Device.Close` still does not
join application callbacks. Cancellation is cooperative: a cancellation racing
with storage's final check and rename can accompany a committed file. Committed
storage is never rolled back, and a later `storage.WriteError` keeps its receipt.

Command and interaction failures preserve their complete original error tree
under safe connector metadata. Use `errors.As` for `expect.WriteError` and its
actual write progress, and `errors.Is` for secondary causes. An interaction's
reply progress remains distinct from the main command's `Attempt.Written`.

Inventory field mapping, fleet scheduling and TFTP success vocabulary have no
API changes. HTTP keys and PG column names remain case-sensitive; optional nulls
become empty strings and IP is required. PG byte budgets include ignored columns.
Cancellation or validation failure returns no partial inventory. PG output owns
its strings before the next driver row can reuse buffers.
