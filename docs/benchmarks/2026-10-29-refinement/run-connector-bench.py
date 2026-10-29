from pathlib import Path
import os,subprocess,time
work=Path('/tmp/connector-refinement-20261029')
env=dict(os.environ,GOMAXPROCS='4')
for label in ['before','after']:
 (work/f'connector-bench-{label}.txt').write_text('')
for sample in range(10):
 for label in (['before','after'] if sample%2==0 else ['after','before']):
  for pkg,match in [('connector','^BenchmarkCommand'),('vendors','^BenchmarkRunningConfig$'),('fleet','^BenchmarkNewPlan$')]:
   print(f'sample={sample+1} candidate={label} package={pkg}',flush=True)
   with (work/f'connector-bench-{label}.txt').open('a') as log:
    subprocess.run([str(work/f'{pkg}-{label}.test'),'-test.run=^$','-test.bench='+match,'-test.benchmem','-test.benchtime=200ms','-test.count=1'],cwd=work,env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
print('serial comparisons complete',flush=True)
