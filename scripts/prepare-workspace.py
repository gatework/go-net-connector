#!/usr/bin/env python3
"""Create an isolated expect candidate from the recorded user source snapshot."""
from pathlib import Path
import hashlib
import json
import shutil
import os
import stat
import subprocess
import sys
import tempfile
import tarfile

root = Path(__file__).resolve().parents[1]
source = Path(sys.argv[1]).absolute() if len(sys.argv) > 1 else None
target = root / ".work/go-expect"

def safe_directory(directory):
    # Do not resolve links first: that would erase evidence of indirection.
    for path in [directory, *directory.parents]:
        if path.is_symlink():
            raise SystemExit("Unsafe source path: " + str(path))
    if not directory.is_dir():
        raise SystemExit("Unsafe source directory: " + str(directory))

def verify(directory, manifest, exact=True):
    safe_directory(directory)
    entries = json.loads(manifest.read_text())["files"]
    expected = {item["path"] for item in entries}
    for item in entries:
        relative = Path(item["path"])
        if relative.is_absolute() or ".." in relative.parts:
            raise SystemExit("Unsafe manifest path")
        path = directory / relative
        safe_directory(path.parent)
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != item["sha256"]:
            raise SystemExit("Source snapshot differs: " + item["path"])
    if exact:
        for parent, dirs, files in os.walk(directory, followlinks=False):
            for name in list(dirs) + files:
                path = Path(parent) / name
                relative = path.relative_to(directory).as_posix()
                mode = path.lstat().st_mode
                if stat.S_ISLNK(mode) or not (stat.S_ISDIR(mode) or stat.S_ISREG(mode)):
                    raise SystemExit("Unsafe candidate path: " + relative)
                if relative == ".git":
                    if name in dirs:
                        dirs.remove(name)
                    continue  # Git directory or worktree gitfile is administrative metadata.
                if stat.S_ISREG(mode) and relative not in expected:
                    raise SystemExit("Unrecorded candidate input: " + relative)

candidate = root / "docs/baseline/go-expect-candidate.json"
if target.exists() or target.is_symlink():
    verify(target, candidate)
else:
    baseline = root / "docs/baseline/go-expect.json"
    if source is not None:
        verify(source, baseline, exact=False)
    target.parent.mkdir(exist_ok=True)
    safe_directory(target.parent)
    temporary = Path(tempfile.mkdtemp(prefix="expect-", dir=target.parent))
    try:
        for item in json.loads(baseline.read_text())["files"]:
            destination = temporary / item["path"]
            destination.parent.mkdir(parents=True, exist_ok=True)
            if source is not None:
                shutil.copy2(source / item["path"], destination)
            else:
                with tarfile.open(root / "dependencies/go-expect-source.tar.gz", "r:gz") as archive:
                    entry = archive.getmember(item["path"])
                    if not entry.isfile():
                        raise SystemExit("Invalid dependency snapshot entry")
                    with archive.extractfile(entry) as stream:
                        destination.write_bytes(stream.read())
                    destination.chmod(entry.mode & 0o777)
        verify(temporary, baseline)
        subprocess.run(["git", "apply", "--check", str(root / "patches/go-expect-compat.patch")], cwd=temporary, check=True)
        subprocess.run(["git", "apply", str(root / "patches/go-expect-compat.patch")], cwd=temporary, check=True)
        verify(temporary, candidate)
        temporary.rename(target)
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)
work = root / "go.work"
content = "go 1.26.0\n\nuse (\n\t.\n\t./.work/go-expect\n)\n\nreplace github.com/gatework/go-expect v0.0.0 => ./.work/go-expect\n"
if work.exists():
    print("Verified expect candidate; existing go.work preserved")
else:
    work.write_text(content)
    print("Prepared local expect candidate and go.work")
