# CLI conventions and tftp-archive implementation

Goal: retain existing subcommands, make starter configuration minimal, honor explicit overrides, unify human sections/dividers, and rename executables to netman and tftp-archive per latest user steering. Preserve module and configuration compatibility.

1. Regression-first parameter/config fixes: explicit false, legacy source precedence, TFTP required facts, quiet, sparse init and provenance.
2. Defaults and site migration: CLI selected / TFTP verified, explicit site routes/source files, preserve all 976 effective transfer targets, no credentials in YAML.
3. Shared human sections, grouped help, machine-output contracts and initialization/export results.
4. Rename command/build/package/consumer references to netman and tftp-archive; offline help/version, TTY no-args archive help, non-TTY protocol unchanged.
5. Paired Go1.26/1.27 race/vet, format/tidy, packaging tests, cross builds, source fingerprint, install renamed helper before switching client default.

Constraints: preserve dirty WIP; no commits/push/publication; no device configuration changes/full batch; no replace/go.work. Record formal dependency blocker separately from paired validation. Existing archive history remains unchanged.
