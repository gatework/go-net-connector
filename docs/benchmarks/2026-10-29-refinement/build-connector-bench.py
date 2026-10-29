from pathlib import Path
import shutil,subprocess,os
work=Path('/tmp/connector-refinement-20261029'); source=Path('/Users/crate/Works/go/.worktrees/connector-quality/go-net-connector')
base=work/'connector-bench-baseline'
shutil.copytree(work/'connector-baseline',base,dirs_exist_ok=True)
shutil.copy2(source/'fleet/refinement_bench_test.go',base/'fleet/refinement_bench_test.go')
env=dict(os.environ,GOWORK='off',GOFLAGS='-mod=readonly',GOTOOLCHAIN='go1.27.3')
for label,root in [('before',base),('after',source)]:
 for pkg in ['connector','fleet','vendors']:
  subprocess.run(['go','test','-c','-o',str(work/f'{pkg}-{label}.test'),'./'+pkg],cwd=root,env=env,check=True)
print('connector benchmark binaries built')
