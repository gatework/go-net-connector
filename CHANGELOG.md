# Changelog

## v0.7.1 - 2026-11-11

- Adopt go-standards v1.0.2 through shared lint, architecture and make gates while retaining installed-worker, PostgreSQL, fuzz and consumer checks.
- Split configuration, execution, transfer and reporting phases without changing public APIs or delivery evidence.
- Upgrade the published go-expect dependency to v0.6.0 and use MatchOption internally.

## v0.7.0

- Add platform drivers, per-device stage pipelines, unified report schema v3 and the deploy, exec, remote backup and diff commands. See docs/releases/v0.7.0.md.
