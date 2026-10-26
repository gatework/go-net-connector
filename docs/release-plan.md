# First release delivery

User authorization (2026-10-26): create private `gatework/go-net-connector`, commit/push implementation, then publish the first version. Default version: v0.1.0. No deployment or visibility change. Original source workspaces and their concurrent modifications remain untouched. The six implementation final-review findings are all accepted after repair.

## Task 1: CI and immutable release artifacts

Add a Linux/macOS Go1.26/1.27 race/shuffle matrix, vet, module verification/tidy check, pinned golangci-lint/govulncheck/actionlint, and reproducible packaging after all gates. Packaging builds five commands for Darwin/Linux amd64/arm64 from GOWORK=off published dependencies; outputs four tarballs, committed source archive, REVISION and SHA256SUMS. Embed exact version/SHA metadata, verify source tree/archives and checksums, preserve required license notices. CI uploads one immutable artifact per SHA/run attempt. Root publishes downloaded CI bytes only after exact-SHA success; no automatic release on every push, no publication from PR jobs. The dependency placeholder must fail release packaging. Add meaningful package/verification regression tests and release documentation. No commits or pushes by implementation subagent.

## Task 2: published expect dependency

Current public expect v0.1.0 lacks required compatibility APIs. The isolated tested candidate includes frozen pre-existing runtime fixes plus additive compatibility changes. Scope confirmation for a separate go-expect release is pending; never publish its current dirty source workspace or fold its later parallel changes into this release. Once a compatible version is published and verified, pin it in go.mod/go.sum and verify direct GOWORK=off consumers without replaces. Original snapshot reconstruction remains useful evidence but is not the public dependency contract.

## Task 3: commit, CI, release and read-back

Commit/push reviewed release setup and final dependency pin. Verify exact head CI, download immutable artifact, validate contents/SHA/source correspondence, create v0.1.0 tag/release using those bytes. Read back tag target, attachments/checksums, visibility and Git state. Record local, CI, installed binary/source-consumer and publication evidence separately. No force tags, clobbering existing release assets, or claims based only on submitted commands.
