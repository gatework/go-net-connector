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
