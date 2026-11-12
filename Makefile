STANDARDS_DIR ?= $(wildcard $(CURDIR)/../go-standards)
include standard.mk

# Repository-specific gates appended to `make check`.
CHECK_EXTRA := modverify modules integration fuzz consumer

.PHONY: modverify modules integration fuzz consumer

modverify:
	$(GO) mod verify

# Published dependency pins and the release tooling's own tests.
modules:
	python3 scripts/release.py check-modules
	python3 scripts/test_release.py -v

# Built binaries, the installed archive worker and PostgreSQL when present.
integration:
	bash scripts/integration

# Fixed execution counts avoid fuzz coordinator deadline races.
fuzz:
	$(GO) test -run '^$$' -fuzz '^FuzzYAMLBoundary$$' -fuzztime=100000x -parallel=2 ./config
	$(GO) test -run '^$$' -fuzz '^FuzzTargetValidation$$' -fuzztime=100000x -parallel=2 ./transfer
	$(GO) test -run '^$$' -fuzz '^FuzzRenderString$$' -fuzztime=100000x -parallel=2 ./internal/terminal

# An independent module builds against this checkout.
consumer:
	python3 scripts/verify-consumer.py
