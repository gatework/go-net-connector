#!/usr/bin/env python3
"""Check production package boundaries from the resolved go list graph."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys


LIBRARIES = (
    "archive", "config", "connector", "facts", "fleet", "inventory", "inventoryfile", "netdisco",
    "parallel", "platform", "storage", "textfsm", "topology", "transfer", "vendors",
)
APPLICATION = ("cmd", "internal/bootstrap", "internal/cli", "internal/workflow")
RULES = (
    (LIBRARIES, APPLICATION, "libraries must not depend on application orchestration"),
    (("connector",), ("vendors",), "generic device execution must not select vendor implementations"),
    (("internal/bootstrap",), ("cmd", "internal/cli"), "bootstrap must not depend on the CLI"),
    (("internal/workflow",), ("cmd", "internal/bootstrap", "internal/cli", "internal/progress"),
     "workflows report events without owning CLI composition or progress reporters"),
)


def in_tree(path, root):
    return path == root or path.startswith(root + "/")


def load_packages(root):
    env = dict(os.environ, GOWORK="off", GOFLAGS="-mod=readonly")
    module = subprocess.run(
        ["go", "list", "-m", "-f", "{{.Path}}"], cwd=root, env=env,
        check=True, capture_output=True, text=True,
    ).stdout.strip()
    output = subprocess.run(
        ["go", "list", "-json", "./..."], cwd=root, env=env,
        check=True, capture_output=True, text=True,
    ).stdout
    decoder = json.JSONDecoder()
    packages = []
    index = 0
    while index < len(output):
        if output[index].isspace():
            index += 1
            continue
        package, index = decoder.raw_decode(output, index)
        packages.append(package)
    if not module or not packages:
        raise ValueError("go list returned no module packages")
    return module, packages


def violations(module, packages):
    prefix = module + "/"
    failures = set()
    for package in packages:
        name = package["ImportPath"]
        if not name.startswith(prefix):
            continue
        source = name.removeprefix(prefix)
        # Deps includes transitive imports, so an internal helper cannot conceal
        # a reverse dependency. Test-only imports intentionally remain separate.
        for dependency in package.get("Deps", []):
            if not dependency.startswith(prefix):
                continue
            target = dependency.removeprefix(prefix)
            for owners, forbidden, reason in RULES:
                if any(in_tree(source, owner) for owner in owners) and any(
                    in_tree(target, boundary) for boundary in forbidden
                ):
                    failures.add(f"{source} -> {target}: {reason}")
    return sorted(failures)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parent.parent)
    args = parser.parse_args()
    try:
        module, packages = load_packages(args.root)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        print("architecture: cannot resolve the Go package graph", file=sys.stderr)
        if isinstance(error, subprocess.CalledProcessError):
            print(error.stderr.strip(), file=sys.stderr)
        return 1
    failures = violations(module, packages)
    if failures:
        for failure in failures:
            print("architecture: " + failure, file=sys.stderr)
        return 1
    print(f"architecture: checked {len(packages)} production packages")
    return 0


if __name__ == "__main__":
    sys.exit(main())
