from pathlib import Path
import shutil,tempfile,subprocess,os
root=Path('/Users/wenwu.yan/Works/go/go-net-connector')
with tempfile.TemporaryDirectory(prefix='nc-core-control-') as tmp:
 p=Path(tmp)/'connector';shutil.copytree(root,p,ignore=shutil.ignore_patterns('.git','.reference','.superpowers','.work','bin','*.pprof'))
 paired=p/'sourceexpect';paired.mkdir()
 for f in (root.parent/'go-expect').glob('*.go'):shutil.copyfile(f,paired/f.name)
 for f in p.rglob('*.go'):
  s=f.read_text();f.write_text(s.replace('"github.com/gatework/go-expect"','"github.com/gatework/go-net-connector/sourceexpect"'))
 (p/'connector/response_collector.go').write_text('''package connector
import "strings"
type responseCollector struct { b strings.Builder }
func(c *responseCollector) WriteString(s string) { c.b.WriteString(s) }
func(c *responseCollector) WriteRune(r rune) { c.b.WriteRune(r) }
func(c *responseCollector) Len() int { return c.b.Len() }
func(c *responseCollector) String() string { return c.b.String() }
func(c *responseCollector) clone() responseCollector { var v responseCollector; v.b.WriteString(c.b.String()); return v }
''')
 (p/'connector/response_collector_test.go').unlink()
 subprocess.run(['go','test','-c','-o','/tmp/connector-core-control.test','./connector'],cwd=p,env=dict(os.environ,GOWORK='off',GOFLAGS='-mod=readonly'),check=True)
