# Two-library unification

Approved conversation plan, 2026-10-26. No commits, pushes, or publication.

1. Port generic compatibility APIs and regression tests into current go-expect; preserve defaults, match/read ownership, cancellation, byte conservation and safe diagnostics. Add FromRegexp; remove duplicate device package after migration.
2. Canonical connector: cancellation-aware terminal Close, explicit recovery after uncertain operations, panic/Goexit retirement, complete error chains and attempted-command evidence.
3. Archive cancellation/cleanup ownership, CRLF parsing, regex semantics, save confirmation shared with topology.
4. API migration: TTY, ProfileName, Value/SetValue, Reply, explicit Poll and Unlimited; typed policy/status strings preserving external values. Retain device-layer key authentication and configurable line endings.
5. Documentation, format/static checks, full local race/vet/fuzz/integrations and independent consumers; fresh final review.

## Constraints
Preserve normal CLI/vendor bytes, report schema and fixtures. No device-command replay. Never expose payloads through implicit diagnostics. Original checkouts and nested compatibility WIP remain untouched. Formal dependency release remains a separate authorized delivery step.
