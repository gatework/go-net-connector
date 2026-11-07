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
import argparse

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description="在隔离副本中验证两库候选源码，不修改正式依赖")
parser.add_argument("expect", type=Path)
parser.add_argument("--build-only", action="store_true", help="只交叉编译全部命令，不运行测试")
parser.add_argument("--quick", action="store_true", help="只编译，用于迁移阶段定位接口错误")
parser.add_argument("--packages", nargs="+", default=["./..."], help="迁移阶段选择测试包")
args = parser.parse_args()
expect = args.expect.resolve()
env = dict(os.environ, GOWORK="off", GOFLAGS="-mod=readonly")
prefix = "github.com/gatework/go-net-connector/sourceexpect"
with tempfile.TemporaryDirectory(prefix="nc-expect-pair-") as temporary:
    package = Path(temporary) / "connector"
    shutil.copytree(root, package, ignore=shutil.ignore_patterns(".git", ".reference", ".superpowers", ".work", "bin", "*.pprof"))
    connector_hashes = {str(p.relative_to(package)):hashlib.sha256(p.read_bytes()).hexdigest() for p in package.rglob("*.go")}
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
    if args.packages == ["./..."]:
        subprocess.run(["go", "build", "-o", str(Path(temporary)/"bin")+"/", "./cmd/..."], cwd=package, env=env, check=True)
    if args.build_only:
        subprocess.run(["go", "build", "./..."], cwd=package, env=env, check=True)
    elif args.quick:
        subprocess.run(["go", "test", "-run", "^$", *args.packages], cwd=package, env=env, check=True)
    else:
        if args.packages == ["./..."]:
            env["NC_TEST_ARCHIVE_WORKER"] = str(Path(temporary)/"bin/tftp-archive")
        for pg in ("/opt/homebrew/opt/postgresql@18/bin", "/usr/lib/postgresql/18/bin"):
            if Path(pg,"postgres").is_file():
                env.setdefault("NC_TEST_POSTGRES_BIN", pg)
                break
        subprocess.run(["go", "test", "-race", "-shuffle=on", "-count=1", "-timeout=180s", *args.packages], cwd=package, env=env, check=True)
        subprocess.run(["go", "vet", *args.packages], cwd=package, env=env, check=True)
    print(json.dumps({"paired_source_contracts": "built" if args.build_only else ("compiled" if args.quick else "passed"), "replacements": False, "published_adoption": False, "expect_sha256": hashes, "connector_sha256": connector_hashes}, sort_keys=True))
