from pathlib import Path
import shutil, tempfile, subprocess, os
root=Path('/Users/wenwu.yan/Works/go/go-net-connector')
expect=root.parent/'go-expect'
with tempfile.TemporaryDirectory(prefix='nc-core-benchmark-') as tmp:
    package=Path(tmp)/'connector'
    shutil.copytree(root,package,ignore=shutil.ignore_patterns('.git','.reference','.superpowers','.work','bin','*.pprof'))
    paired=package/'sourceexpect';paired.mkdir()
    for source in expect.glob('*.go'): shutil.copyfile(source,paired/source.name)
    for source in package.rglob('*.go'):
        data=source.read_text();changed=data.replace('"github.com/gatework/go-expect"','"github.com/gatework/go-net-connector/sourceexpect"')
        if changed!=data:source.write_text(changed)
    subprocess.run(['go','test','-c','-o','/tmp/connector-core-candidate.test','./connector'],cwd=package,env=dict(os.environ,GOWORK='off',GOFLAGS='-mod=readonly'),check=True)
