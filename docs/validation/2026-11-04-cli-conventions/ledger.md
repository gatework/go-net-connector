# Execution ledger — docs/2026-11-04-cli-conventions-plan.md

Ruling: continue in the existing feat/ruby-parity worktree because the approved CLI is extensive uncommitted WIP; no reset/stash/worktree relocation.
Pre-flight: defaults feed config, CLI preflight and worker path; preserve site semantics before publishing changed defaults. Dividers belong only to human output; protocol/JSON/body export remain pure.
Formal dependency: published go-expect versions end at v0.1.2, which lacks current interfaces. Candidate pairing is validation/development delivery only, never publication proof.

Latest steering: primary executable becomes netman, archive executable becomes tftp-archive; update command/help/completion/build/package references while retaining module, configuration discovery, environment and protocol compatibility.
Regression checks: old archive executable failed new help/version/invalid-argument/TTY contracts; old CLI failed netman help/completion identity. New implementations pass those checks in the paired suite. Configuration and output regressions were watched failing, then passed; generated help reference is pending regeneration.
Site preservation: backup.yml was backed up with mode 0600 and explicit source interface/source file overrides before removing site assumptions from generic defaults; final 976-target comparison remains required.
