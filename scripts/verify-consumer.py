#!/usr/bin/env python3
"""Verify the source under review; all external dependencies resolve from GitHub."""
from pathlib import Path
import json
import os
import shutil
import subprocess
import tempfile
import sys

root = Path(__file__).resolve().parents[1]
expect = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else None
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
import("testing";"errors";"github.com/gatework/go-net-connector/vendors";"github.com/gatework/go-net-connector/vendors/dialect";"github.com/gatework/go-net-connector/vendors/h3c";"github.com/gatework/go-net-connector/topology";"github.com/gatework/go-net-connector/textfsm";"github.com/gatework/go-net-connector/connector";"github.com/gatework/go-net-connector/fleet")
func TestSourceConsumer(t *testing.T){
 registry:=vendors.NewBuiltinRegistry()
 declaration,e:=registry.Lookup("cisco_ios");if e!=nil{t.Fatal(e)}
 var platform dialect.Platform=declaration
 p:=platform.Profile()
 if e=p.Validate();e!=nil{t.Fatal(e)}
 c:=connector.ConnectionConfig{Host:"192.0.2.1"};if e=c.Validate();e!=nil{t.Fatal(e)}
 d,e:=connector.New(p,c);if e!=nil{t.Fatal(e)};defer d.Close()
 if _,e=fleet.NewRules(fleet.RuleOptions{},registry);e!=nil{t.Fatal(e)}
 var _ fleet.PlatformResolver=registry
 if got,ok:=registry.ResolvePlatform(dialect.InventoryIdentity{Vendor:"h3c"});!ok||got!="h3c"{t.Fatal(got,ok)}
 var _ topology.DescriptionReader=h3c.New()
 var _ topology.DescriptionWriter=h3c.New()
 var topologyClient *topology.Client
 _ = topologyClient
 if got:=topology.ConfigName("Te1/0/2");got!="TenGigabitEthernet1/0/2"{t.Fatal(got)}
 var step connector.StepResult;if _,e=step.Response().Rendered();!errors.Is(e,connector.ErrIncomplete){t.Fatal(e)}
 rows,e:=textfsm.ParseTemplate("cisco/show_running_config.textfsm","interface Gi0/0\\n description uplink\\n!\\n");if e!=nil||len(rows)!=1{t.Fatal("embedded template",e)}
}
''')
    if expect:
        paired = package / "sourceexpect"
        paired.mkdir()
        for source in expect.glob("*.go"):
            if not source.name.endswith("_test.go"):
                shutil.copy2(source, paired / source.name)
        for source in package.rglob("*.go"):
            source.write_text(source.read_text().replace('"github.com/gatework/go-expect"', '"github.com/gatework/go-net-connector/sourceexpect"'))
    subprocess.run(["go", "test", "./consumercheck"], cwd=package, env=env, check=True)
    dependency = json.loads(subprocess.check_output(["go", "list", "-m", "-json", "github.com/gatework/go-expect"], cwd=package, env=env))
    if dependency.get("Replace") or not dependency.get("Version"):
        raise SystemExit("go-expect must be a versioned GitHub module")
    binaries = temporary / "bin"
    binaries.mkdir()
    subprocess.run(["go", "build", "-o", str(binaries) + "/", "./cmd/..."], cwd=package, env=env, check=True)
    outside = temporary / "outside"
    outside.mkdir()
    expected = {"netman", "tftp-archive"}
    if {path.name for path in binaries.iterdir()} != expected:
        raise SystemExit("发行命令清单不匹配")
    commands = [[], ["backup"], ["backup", "run"], ["backup", "plan"], ["backup", "tftp"], ["backup", "export"], ["inventory", "list"], ["topology", "discover"], ["topology", "plan"], ["topology", "apply"], ["config", "init"], ["config", "show"], ["config", "check"], ["completion"], ["version"]]
    for command in commands:
        result = subprocess.run([str(binaries / "netman"), *command, "--help"], cwd=outside, env=env, check=True, capture_output=True, text=True)
        if "用法" not in result.stdout or any(term in result.stdout for term in ("Usage:", "Available Commands:", "Global Flags:", "help for ", "(default ")):
            raise SystemExit("中文帮助不完整：" + " ".join(command))
    for shell in ("bash", "zsh", "fish"):
        result = subprocess.run([str(binaries / "netman"), "completion", shell], cwd=outside, env=env, check=True, capture_output=True, text=True)
        if "netman" not in result.stdout:
            raise SystemExit("未生成补全脚本：" + shell)
    for command in (["not-a-command"], ["backup", "not-a-command"], ["backup", "run", "--not-a-flag"], ["backup", "export"]):
        result = subprocess.run([str(binaries / "netman"), *command], cwd=outside, env=env, capture_output=True, text=True)
        if result.returncode != 2 or not result.stderr or any(term in result.stderr for term in ("unknown command", "unknown flag", "arg(s)")):
            raise SystemExit("中文参数错误处理不完整：" + " ".join(command))
    result = subprocess.run([str(binaries / "tftp-archive"), "--help"], cwd=outside, env=env, check=True, capture_output=True, text=True)
    if "用法" not in result.stdout:
        raise SystemExit("归档 worker 未输出中文帮助")
    subprocess.run([str(binaries / "tftp-archive"), "--version"], cwd=outside, env=env, check=True)
    print(json.dumps({"source_consumer": "passed", "embedded_templates": "passed", "outside_checkout_cli": "passed", "expect_version": dependency["Version"], "external_replacements": False, "paired_expect_source": bool(expect), "published_adoption": False}))
