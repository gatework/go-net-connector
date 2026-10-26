#!/usr/bin/env python3
"""Verify setup, module consumers and installed CLI from a fresh outside directory."""
from pathlib import Path
import json
import os
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
source = root.parent / 'go-expect'
env = dict(os.environ, GOWORK='off', GOPROXY='off', GOSUMDB='off')
with tempfile.TemporaryDirectory(prefix='nc-consumer-') as temporary:
    temporary = Path(temporary)
    package = temporary / 'connector'
    package.mkdir()
    for name in ['archive','cmd','config','connector','internal','inventory','netdisco','parallel','storage','textfsm','topology','transfer','vendors','testdata','scripts','patches','docs','dependencies']:
        shutil.copytree(root / name, package / name)
    for name in ['go.mod','go.sum']:
        shutil.copy2(root / name, package / name)
    subprocess.run(['python3',str(package/'scripts/prepare-workspace.py')],cwd=package,check=True)
    expect = package / '.work/go-expect'
    subprocess.run(['go','mod','edit','-replace=github.com/gatework/go-expect='+str(expect)],cwd=package,env=env,check=True)
    subprocess.run(['go','test','./...'],cwd=package,env=env,check=True)
    consumer=temporary/'consumer';consumer.mkdir()
    (consumer/'go.mod').write_text('module consumer\n\ngo 1.26.0\nrequire github.com/gatework/go-net-connector v0.0.0\nreplace github.com/gatework/go-net-connector => '+str(package)+'\nreplace github.com/gatework/go-expect => '+str(expect)+'\n')
    (consumer/'main_test.go').write_text('''package consumer
import("testing";"github.com/gatework/go-net-connector/vendors";"github.com/gatework/go-net-connector/textfsm")
func TestIndependentEmbeddedTemplates(t *testing.T){if len(vendors.Names())!=12{t.Fatal("profiles")};rows,e:=textfsm.ParseTemplate("cisco/show_running_config.textfsm","interface Gi0/0\\n description uplink\\n!\\n");if e!=nil||len(rows)!=1{t.Fatal(rows,e)}}
''')
    subprocess.run(['go','mod','tidy'],cwd=consumer,env=env,check=True)
    subprocess.run(['go','test','./...'],cwd=consumer,env=env,check=True)
    binaries=temporary/'bin';binaries.mkdir()
    subprocess.run(['go','build','-o',str(binaries)+'/', './cmd/...'],cwd=package,env=env,check=True)
    outside=temporary/'outside';outside.mkdir()
    for name in ['net-backup','net-backup-batch','net-backup-tftp','net-topology']:
        help_result=subprocess.run([str(binaries/name),'--help'],cwd=outside,env=env,check=True,capture_output=True,text=True)
        assert 'Usage:' in help_result.stdout
    print(json.dumps({'independent_consumer':'passed','embedded_templates':'passed','outside_checkout_cli':'passed','dependency_snapshot':'verified'}))
