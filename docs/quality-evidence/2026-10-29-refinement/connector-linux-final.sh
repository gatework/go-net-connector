#!/bin/sh
set -eu
export GOWORK=off GOFLAGS=-mod=readonly GOTOOLCHAIN=local
export GOMODCACHE=/go/pkg/mod
cd /source
for version in 1.26.0 1.27.3; do
 if [ "$version" = 1.26.0 ]; then
  go_bin=/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.0.linux-arm64/bin/go
 else
  go_bin=/usr/local/go/bin/go
 fi
 "$go_bin" version
 "$go_bin" build -o /tmp/worker ./cmd/net-connector-archive
 export NC_TEST_ARCHIVE_WORKER=/tmp/worker
 "$go_bin" test -race -shuffle=on -count=1 ./connector ./fleet ./internal/cli
 "$go_bin" vet ./...
done
