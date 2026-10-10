"""실제 CLI로 연결 예제의 최종 바이트·권한과 오류 시 무변경을 검증한다."""
import os
import json
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


class IntegerEditExample(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.repository = Path(__file__).resolve().parents[2]
        cls.directory = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.directory.name) / "tools"
        cls.environment = dict(os.environ)
        cls.environment.setdefault("GOCACHE", "/tmp/my-agent-tools-benchmark-go-cache")
        subprocess.run(["go", "build", "-buildvcs=false", "-o", str(cls.binary), "./cmd/tools"], cwd=cls.repository, env=cls.environment, check=True)
        cls.environment["PATH"] = str(cls.binary.parent) + os.pathsep + cls.environment["PATH"]

    @classmethod
    def tearDownClass(cls):
        cls.directory.cleanup()

    def test_final_bytes_modes_and_ambiguous_assignment_failure(self):
        for source, successful in [(b"retry_budget=8\r\ntimeout=30\r\n# keep", True), (b"retry_budget=10\ntimeout=30\n", True), (b"retry_budget=8\nretry_budget=9\n", False), (b"retry_budget=8\ntimeout=31\n", False)]:
            with self.subTest(source=source), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                path = root / "settings.py"
                path.write_bytes(source)
                path.chmod(0o640)
                mode = stat.S_IMODE(path.stat().st_mode)
                process = subprocess.run(["python3", str(self.repository / "examples/fs/edit-integer.py"), "--root", directory, "--path", "settings.py", "--key", "retry_budget", "--amount", "3", "--expect", "timeout=30"], env=self.environment, capture_output=True, text=True)
                if successful:
                    receipt = json.loads(process.stdout)
                    before = b"10" if b"=10" in source else b"8"
                    expected = source.replace(b"retry_budget=" + before, b"retry_budget=" + str(int(before) + 3).encode(), 1)
                    self.assertEqual(process.returncode, 0, process.stderr)
                    self.assertTrue(receipt["verified"])
                    self.assertEqual(receipt["preserved_integers"], {"timeout": 30})
                    self.assertEqual(path.read_bytes(), expected)
                else:
                    self.assertNotEqual(process.returncode, 0)
                    self.assertEqual(path.read_bytes(), source)
                self.assertEqual(stat.S_IMODE(path.stat().st_mode), mode)


if __name__ == "__main__":
    unittest.main()
