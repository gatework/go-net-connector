# First-release verification

Publication is in preparation. The implementation commit is
`4f3774de892f1907531e48117d2eba4a413f02fc`, pushed to the private
`gatework/go-net-connector` repository. Repository visibility and the remote
branch SHA were read back after the push. No version has been published yet.

## Security update

The initial release scan on Go 1.27.1 with golang.org/x/text v0.36.0 reported
reachable standard-library and text-library advisories. Release preparation uses
Go 1.27.3 and x/text v0.43.0. After that update, govulncheck v1.9.0 reported
`No vulnerabilities found`; race tests for archive, storage, Netdisco and connector
passed with the updated dependency. The Go language floor remains 1.26.

The complete shuffled race suite and vet subsequently passed on Go 1.27.3
and Go 1.26.10, with the installed worker and PostgreSQL 18 integration enabled.
Release automation review is accepted after repairing component-wise symlink
containment. Eleven release-package tests and six workspace reconstruction tests
cover corruption, source identity, platform identity and repeatable artifacts.

Primary references: [Go toolchains](https://go.dev/dl/),
[x/text advisory](https://pkg.go.dev/vuln/GO-2026-6629), and
[certificate advisory](https://pkg.go.dev/vuln/GO-2026-6809).

## Remaining release gates

- Publish or select a compatible formal go-expect version, then verify a direct
  GOWORK=off consumer without local replacements. The initial v0.0.0 requirement
  is deliberately not release-ready.
- Run exact-SHA Linux/macOS CI on Go 1.26/1.27,
  lint, vulnerability checks and immutable artifact packaging.
- Verify the downloaded source and four binary archives, version/SHA identities
  and SHA256SUMS. Publish those same bytes, then read back tags, assets,
  checksums, repository visibility and Git state.

Local evidence remains distinct from CI and publication evidence. There is no
production-device or deployment claim.
