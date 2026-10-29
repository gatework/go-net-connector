#!/usr/bin/env python3
"""Verify the source under review; all external dependencies resolve from GitHub."""
from pathlib import Path
import json
import os
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
env = dict(os.environ, GOWORK="off", GOFLAGS="-mod=readonly")
with tempfile.TemporaryDirectory(prefix="nc-consumer-") as temporary:
    temporary = Path(temporary)
    package = temporary / "connector"
    package.mkdir()
    for name in ("archive", "cmd", "config", "connector", "internal", "inventory", "fleet", "netdisco", "parallel", "storage", "textfsm", "topology", "transfer", "vendors", "testdata"):
        shutil.copytree(root / name, package / name)
    for name in ("go.mod", "go.sum"):
        shutil.copy2(root / name, package / name)
    modules = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"], cwd=package, env=env))
    if modules.get("Replace"):
        raise SystemExit("source module must not replace external dependencies")
    # Test the extracted source as its own module; external dependencies remain
    # versioned. This is a source-consumer check, not proof of a published version.
    consumer = package / "consumercheck"
    consumer.mkdir()
    (consumer / "main_test.go").write_text('''package consumercheck
import("testing";"github.com/gatework/go-net-connector/vendors";"github.com/gatework/go-net-connector/textfsm";"github.com/gatework/go-net-connector/connector";"github.com/gatework/go-net-connector/fleet")
func TestSourceConsumer(t *testing.T){
 p,e:=vendors.Profile("cisco_ios");if e!=nil{t.Fatal(e)}
 if e=p.Validate();e!=nil{t.Fatal(e)}
 c:=connector.ConnectionConfig{Host:"192.0.2.1"};if e=c.Validate();e!=nil{t.Fatal(e)}
 d,e:=connector.New(p,c);if e!=nil{t.Fatal(e)};defer d.Close()
 if _,e=fleet.NewRules(fleet.RuleOptions{},vendors.Profile);e!=nil{t.Fatal(e)}
 var step connector.StepResult;if _,e=step.Rendered();e!=nil{t.Fatal(e)}
 rows,e:=textfsm.ParseTemplate("cisco/show_running_config.textfsm","interface Gi0/0\\n description uplink\\n!\\n");if e!=nil||len(rows)!=1{t.Fatal("embedded template",e)}
}
''')
    subprocess.run(["go", "test", "./consumercheck"], cwd=package, env=env, check=True)
    dependency = json.loads(subprocess.check_output(["go", "list", "-m", "-json", "github.com/gatework/go-expect"], cwd=package, env=env))
    if dependency.get("Replace") or not dependency.get("Version"):
        raise SystemExit("go-expect must be a versioned GitHub module")
    binaries = temporary / "bin"
    binaries.mkdir()
    subprocess.run(["go", "build", "-o", str(binaries) + "/", "./cmd/..."], cwd=package, env=env, check=True)
    outside = temporary / "outside"
    outside.mkdir()
    for name in ("net-backup", "net-backup-batch", "net-backup-tftp", "net-topology"):
        result = subprocess.run([str(binaries / name), "--help"], cwd=outside, env=env, check=True, capture_output=True, text=True)
        if "Usage:" not in result.stdout:
            raise SystemExit("missing help output: " + name)
    subprocess.run([str(binaries / "net-connector-archive"), "--version"], cwd=outside, env=env, check=True)
    print(json.dumps({"source_consumer": "passed", "embedded_templates": "passed", "outside_checkout_cli": "passed", "expect_version": dependency["Version"], "external_replacements": False}))
