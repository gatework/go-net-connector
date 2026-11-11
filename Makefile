# Shared gates use the published standard unless a checkout is supplied explicitly.
include standard.mk

CHECK_EXTRA := project-checks

.PHONY: project-checks project-quality
project-checks:
	bash scripts/ci

project-quality:
	python3 scripts/release.py check-modules
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
	python3 scripts/test_release.py -v
