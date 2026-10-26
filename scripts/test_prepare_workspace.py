import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import tarfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
class CandidateTest(unittest.TestCase):
    def fixture(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name).resolve()
        (root/'scripts').mkdir()
        shutil.copy2(Path(os.environ.get('NC_TEST_PREPARE_SCRIPT', str(ROOT/'scripts/prepare-workspace.py'))), root/'scripts/prepare-workspace.py')
        (root/'docs/baseline').mkdir(parents=True)
        for name in ['go-expect.json','go-expect-candidate.json']:
            shutil.copy2(ROOT/'docs/baseline'/name, root/'docs/baseline'/name)
        shutil.copytree(ROOT/'dependencies', root/'dependencies')
        shutil.copytree(ROOT/'patches', root/'patches')
        shutil.copytree(ROOT/'.work/go-expect', root/'.work/go-expect')
        return root
    def run_script(self,root):
        return subprocess.run(['python3', str(root/'scripts/prepare-workspace.py')], capture_output=True, text=True)
    def test_extra_build_inputs_preserved_and_refused(self):
        for name in ['unrecorded.go','go.work','vendor/modules.txt']:
            with self.subTest(name=name):
                root=self.fixture(); extra=root/'.work/go-expect'/name
                extra.parent.mkdir(exist_ok=True); extra.write_text('unrecorded input')
                result=self.run_script(root)
                self.assertNotEqual(result.returncode,0,result.stdout)
                self.assertEqual(extra.read_text(),'unrecorded input')
    def test_symlink_inputs_refused(self):
        for mode in ['file','parent','target']:
            with self.subTest(mode=mode):
                root=self.fixture(); target=root/'.work/go-expect'
                if mode=='file':
                    path=target/'go.mod'; saved=root/'saved'; path.rename(saved); path.symlink_to(saved)
                elif mode=='target':
                    saved=root/'saved'; target.rename(saved); target.symlink_to(saved,target_is_directory=True)
                else:
                    work=root/'.work'; saved=root/'saved'; work.rename(saved); work.symlink_to(saved,target_is_directory=True)
                self.assertNotEqual(self.run_script(root).returncode,0)
                self.assertTrue(saved.exists())
    def test_changed_recorded_input_preserved(self):
        root=self.fixture(); path=root/'.work/go-expect/go.mod'; path.write_text('different source')
        self.assertNotEqual(self.run_script(root).returncode,0)
        self.assertEqual(path.read_text(),'different source')
    def test_fresh_reconstruction_and_source_work_preserved(self):
        root=self.fixture(); shutil.rmtree(root/'.work/go-expect')
        self.assertEqual(self.run_script(root).returncode,0)
        # Independently hash all recorded output files, not just script status.
        import hashlib
        for item in json.loads((root/'docs/baseline/go-expect-candidate.json').read_text())['files']:
            self.assertEqual(hashlib.sha256((root/'.work/go-expect'/item['path']).read_bytes()).hexdigest(),item['sha256'])
    def test_source_copy_imports_only_recorded_files(self):
        root=self.fixture(); shutil.rmtree(root/'.work/go-expect'); source=root/'original'; source.mkdir()
        with tarfile.open(root/'dependencies/go-expect-source.tar.gz') as archive:
            for item in json.loads((root/'docs/baseline/go-expect.json').read_text())['files']:
                path=source/item['path']; path.parent.mkdir(parents=True,exist_ok=True)
                path.write_bytes(archive.extractfile(item['path']).read())
        extra=source/'concurrent_work.go'; extra.write_text('preserve original work')
        result=subprocess.run(['python3',str(root/'scripts/prepare-workspace.py'),str(source)],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(extra.read_text(),'preserve original work')
        self.assertFalse((root/'.work/go-expect/concurrent_work.go').exists())
    def test_git_metadata_allowed(self):
        root=self.fixture(); git=root/'.work/go-expect/.git'; git.unlink(missing_ok=True); git.mkdir(); (git/'config').write_text('[core]\n')
        self.assertEqual(self.run_script(root).returncode,0)
if __name__=='__main__': unittest.main()
