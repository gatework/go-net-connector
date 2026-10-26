#!/usr/bin/env python3
"""Run Linux race/vet in a disposable copy, preserving the host workspace."""
from pathlib import Path
import os
import shutil
import subprocess
import shlex
import tempfile
root=Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix='nc-linux-') as temporary:
    package=Path(temporary)/'connector'
    shutil.copytree(root,package,ignore=shutil.ignore_patterns('.git','.reference','.superpowers','.work','bin','*.pprof'))
    cache=subprocess.check_output(['go','env','GOMODCACHE'],text=True).strip()
    image=os.environ.get('NC_TEST_GO_IMAGE','golang:1.27.2-bookworm')
    go=shlex.quote(os.environ.get('NC_TEST_GO_COMMAND','go'))
    command=f'{go} version && {go} test -race ./... -count=1 && {go} vet ./...'
    subprocess.run(['docker','run','--rm','--network','none','--mount','type=bind,src='+str(package)+',dst=/workspace','--mount','type=bind,src='+cache+',dst=/gomodcache,readonly','-e','GOWORK=off','-e','GOFLAGS=-mod=readonly','-e','GOMODCACHE=/gomodcache','-e','GOTOOLCHAIN=local','-e','GOPROXY=off','-w','/workspace',image,'sh','-c',command],check=True)
