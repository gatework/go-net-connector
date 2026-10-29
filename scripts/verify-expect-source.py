#!/usr/bin/env python3
"""Test paired sources in a disposable module, without replace/go.work.

Usage: python3 scripts/verify-expect-source.py /path/to/go-expect
Only the copied expect import prefix changes. The real connector dependency
remains pinned; this fixture is not proof of publication or consumer adoption.
"""
from pathlib import Path
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile

root = Path(__file__).resolve().parents[1]
expect = Path(sys.argv[1]).resolve()
env = dict(os.environ, GOWORK="off", GOFLAGS="-mod=readonly")
prefix = "github.com/gatework/go-net-connector/sourceexpect"
with tempfile.TemporaryDirectory(prefix="nc-expect-pair-") as temporary:
    package = Path(temporary) / "connector"
    shutil.copytree(root, package, ignore=shutil.ignore_patterns(".git", ".reference", ".superpowers", ".work", "bin", "*.pprof"))
    paired = package / "sourceexpect"
    paired.mkdir()
    hashes = {}
    for source in expect.glob("*.go"):
        data = source.read_bytes()
        hashes[source.name] = hashlib.sha256(data).hexdigest()
        (paired / source.name).write_bytes(data)
    if (expect / "testdata").exists():
        shutil.copytree(expect / "testdata", paired / "testdata")
    for source in package.rglob("*.go"):
        data = source.read_text()
        changed = data.replace('"github.com/gatework/go-expect"', '"' + prefix + '"')
        if data != changed:
            source.write_text(changed)
    modules = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"], cwd=package, env=env))
    if modules.get("Replace"):
        raise SystemExit("no local replacement permitted")
    subprocess.run(["go", "test", "-race", "-shuffle=on", "-count=1", "-timeout=120s", "./connector", "./vendors", "./transfer", "./sourceexpect"], cwd=package, env=env, check=True)
    subprocess.run(["go", "vet", "./connector", "./vendors", "./transfer", "./sourceexpect"], cwd=package, env=env, check=True)
    print(json.dumps({"paired_source_contracts": "passed", "replacements": False, "published_adoption": False, "expect_sha256": hashes}, sort_keys=True))
