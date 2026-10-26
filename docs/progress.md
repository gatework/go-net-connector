# Implementation progress

Implementation and final scoped review are complete. The user authorized a private GitHub commit/push and first release on 2026-10-26. Release preparation is in progress; deployment remains outside scope.

| Stage | Status |
| --- | --- |
| P0 frozen references | Recorded manifests and working-source snapshots; Ruby baseline suite passes |
| P1 expect compatibility | Implemented; independent review accepted after one repair round |
| P2 connector lifecycle | Implemented; lifecycle/accounting/logging/pager repair independently accepted |
| P3 vendors/parser/storage | 12 profiles, 19 templates and private atomic storage implemented and reviewed |
| P4 topology | Implemented; formatter error propagation repair accepted |
| P5 transfer/archive | TFTP/Junos FTP/SCP, local/SSH worker and pool implemented; five archive fixes independently accepted |
| P6 inventory/Fleet/report | HTTP/PG/cache/planning/report implemented; source, unchanged receipt and diagnostic repairs accepted |
| P7 config/CLI | Config and CLI accepted; initialization, output, policy and report boundary repairs independently reviewed |
| P8 acceptance | Darwin/Linux Go 1.26/1.27 gates, four target builds, consumers, fuzz and benchmarks pass; all six final review repairs independently accepted |

Reference source directories were not edited. The original go-expect directory acquired parallel edits after freezing; those remain separate. `dependencies/go-expect-source.tar.gz` plus the compatibility patch reproduces the isolated candidate without overwriting that workspace.

A real localhost sshd and installed worker validated two foreground master connections and verified upload/archive/cleanup. Synthetic device conversations, HTTP/TLS and temporary PostgreSQL checks remain distinct from real-device evidence. Details and limits are recorded in [verification.md](verification.md).

Review artifacts and source diffs remain under `.superpowers/sdd/implementation-plan/` to preserve the complete review process separately from the source commit.

## 2026-10-26: automatic device output encoding

- Reproduced H3C/H3C wireless GB2312/GBK Chinese configuration failing with `invalid_output_encoding`. Added pinned `github.com/saintfish/chardet` at `v0.0.0-20230101081208-5e3ef4b5456d` and conversion through the existing `golang.org/x/text` dependency.
- Every connector command now automatically checks its complete response and returns UTF-8 text, without new CLI flags, configuration settings or connection options. Valid UTF-8 takes priority; non-UTF-8 detection is bounded to 64 KiB from the first non-ASCII byte and constrained to the GB18030 family. Full-response conversion rejects malformed/truncated sequences rather than silently inserting replacement characters. Detection remains heuristic when bytes have multiple valid interpretations.
- Raw responses remain available. Legacy terminal edits run on original bytes before visible text conversion; decoded output retains the byte budget. Backups use UTF-8 bytes and SHA-256, and failed decoding leaves previous backups intact. Logs convert complete lines and redact UTF-8 and encoded secret variants before and after rendering. The existing Fleet invalid-encoding error and failed-step semantics remain unchanged.
- Regression evidence covers default API and CLI use, ASCII/UTF-8/GBK/GB18030, one-byte transport fragments, paging, Chinese after a large ASCII prefix, native-byte terminal edits, corrupted input, previous-backup preservation, decoded-size limits, secret redaction and backup hashes.
- Final local checks passed with Go 1.27.3: `sh scripts/check` (workspace tests, complete race suite, vet, frozen expect race suite and command builds), `go mod verify`, independent consumer/outside-checkout CLI verification with `GOTOOLCHAIN=local`, and `govulncheck v1.9.0` (`No vulnerabilities found`). Module tidy was checked using a temporary modfile with the existing frozen expect candidate; no replacement was added to the published module file.
- These checks did not contact a production switch or deploy to a device. The original go-expect working tree was not edited.
