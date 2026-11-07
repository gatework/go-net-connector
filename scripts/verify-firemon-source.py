#!/usr/bin/env python3
"""Exercise a disposable Firemon API migration, never its pinned checkout.

Usage: python3 scripts/verify-firemon-source.py /path/to/firemon/adapter/netconnector
This proves source contracts only, not adoption by a published dependency.
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
adapter = Path(sys.argv[1]).resolve()
expect = Path(sys.argv[2]).resolve() if len(sys.argv) > 2 else None
env = dict(os.environ, GOWORK="off", GOFLAGS="-mod=readonly")
sources = {p.name: p.read_bytes() for p in adapter.iterdir() if p.is_file() and (p.suffix == ".go" or p.name in ("go.mod", "go.sum"))}
manifest = {name: hashlib.sha256(data).hexdigest() for name, data in sources.items()}
with tempfile.TemporaryDirectory(prefix="nc-firemon-refinement-") as temporary:
    package = Path(temporary) / "connector"
    shutil.copytree(root, package, ignore=shutil.ignore_patterns(".git", ".reference", ".superpowers", ".work", "bin", "*.pprof"))
    consumer = package / "consumer" / "firemon"
    consumer.mkdir(parents=True)
    for name, data in sources.items():
        if not name.endswith(".go"):
            continue
        text = data.decode().replace('"github.com/gatework/firemon/adapter/netconnector"', '"github.com/gatework/go-net-connector/consumer/firemon"')
        (consumer / name).write_text(text)
    if expect:
        paired = package / "sourceexpect"
        paired.mkdir()
        for source in expect.glob("*.go"):
            shutil.copy2(source, paired / source.name)
        if (expect / "testdata").exists():
            shutil.copytree(expect / "testdata", paired / "testdata")
        for source in package.rglob("*.go"):
            source.write_text(source.read_text().replace('"github.com/gatework/go-expect"', '"github.com/gatework/go-net-connector/sourceexpect"'))
    # Read the module requirements from the frozen bytes rather than re-reading
    # a potentially concurrent checkout during the test.
    frozen = Path(temporary) / "adapter"
    frozen.mkdir()
    (frozen / "go.mod").write_bytes(sources["go.mod"])
    modules = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"], cwd=frozen, env=env))
    version = next(d["Version"] for d in modules["Require"] if d["Path"] == "github.com/gatework/firemon")
    subprocess.run(["go", "mod", "edit", "-require=github.com/gatework/firemon@" + version], cwd=package, env=env, check=True)
    subprocess.run(["go", "mod", "tidy"], cwd=package, env=env, check=True)
    modules = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"], cwd=package, env=env))
    if modules.get("Replace"):
        raise SystemExit("no local replacement permitted")
    subprocess.run(["go", "fmt", "./consumer/firemon"], cwd=package, env=env, check=True)
    subprocess.run(["go", "test", "-race", "-shuffle=on", "-count=1", "-timeout=120s", "./consumer/firemon"], cwd=package, env=env, check=True)
    subprocess.run(["go", "vet", "./consumer/firemon"], cwd=package, env=env, check=True)
    print(json.dumps({"source_adapter_contracts": "passed", "firemon_version": version, "replacements": False, "published_adoption": False, "adapter_source_sha256": manifest}, sort_keys=True))
