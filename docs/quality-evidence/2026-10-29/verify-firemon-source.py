from pathlib import Path
import hashlib, json, os, shutil, subprocess, tempfile
root=Path("/Users/crate/Works/go/.worktrees/connector-quality/go-net-connector")
adapter=Path("/Users/crate/Works/go/firemon/adapter/netconnector")
env=dict(os.environ, GOWORK="off", GOFLAGS="-mod=readonly")
before={str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in adapter.glob("*") if p.is_file()}
with tempfile.TemporaryDirectory(prefix="nc-firemon-source-") as temp:
 package=Path(temp)/"connector"
 shutil.copytree(root,package,ignore=shutil.ignore_patterns(".git",".reference",".superpowers",".work","bin","*.pprof"))
 consumer=package/"consumer"/"firemon"
 consumer.mkdir(parents=True)
 for path in adapter.glob("*.go"):
  text=path.read_text().replace('"github.com/gatework/firemon/adapter/netconnector"','"github.com/gatework/go-net-connector/consumer/firemon"')
  (consumer/path.name).write_text(text)
 source_mod=json.loads(subprocess.check_output(["go","mod","edit","-json"],cwd=adapter,env=env))
 version=next(d["Version"] for d in source_mod["Require"] if d["Path"]=="github.com/gatework/firemon")
 subprocess.run(["go","mod","edit","-require=github.com/gatework/firemon@"+version],cwd=package,env=env,check=True)
 subprocess.run(["go","mod","tidy"],cwd=package,env=env,check=True)
 modules=json.loads(subprocess.check_output(["go","mod","edit","-json"],cwd=package,env=env))
 assert not modules.get("Replace")
 subprocess.run(["go","test","-race","-shuffle=on","-count=1","-timeout=120s","./consumer/firemon"],cwd=package,env=env,check=True)
 subprocess.run(["go","vet","./consumer/firemon"],cwd=package,env=env,check=True)
 print(json.dumps({"source_adapter_contracts":"passed", "firemon_version":version,"replacements":False,"adapter_source_sha256":before},sort_keys=True))
after={str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in adapter.glob("*") if p.is_file()}
assert before==after, "original adapter changed during verification"
