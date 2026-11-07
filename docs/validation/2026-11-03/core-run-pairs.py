import subprocess
from pathlib import Path
base='/tmp/go-net-connector-review.test'
after='/tmp/connector-core-candidate.test'
for sample in range(10):
    for label,binary in ([('before',base),('after',after)] if sample%2==0 else [('after',after),('before',base)]):
        with open('/tmp/connector-core-final-'+label+'.txt','a') as f:
            subprocess.run([binary,'-test.run','^$','-test.bench','BenchmarkCommand(Output|Paged1000|Multiline|Logging)$','-test.benchmem','-test.benchtime=100ms','-test.count=1'],stdout=f,stderr=subprocess.STDOUT,check=True)
    print('pair',sample+1,flush=True)
