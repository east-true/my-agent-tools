import json
import re
import tempfile
import threading
import unittest
from importlib.util import find_spec
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import Request, urlopen

import run_all_github_benchmark as benchmark


class CompleteCoverage(unittest.TestCase):
    @unittest.skipUnless(find_spec('jsonschema'), 'optional independent JSON Schema validator unavailable')
    def test_output_schema_accepts_a_rename_and_a_plain_modified_file(self):
        import jsonschema
        value={'files':benchmark.FILES}
        generated=benchmark.schema(value)
        jsonschema.validate(value,generated)
        invalid={'files':[benchmark.FILES[0],dict(benchmark.FILES[1],previous_filename=None)]}
        with self.assertRaises(jsonschema.ValidationError):jsonschema.validate(invalid,generated)

    def test_every_advertised_command_has_an_independent_task(self):
        source = Path(__file__).resolve().parents[2] / 'internal/cli/cli.go'
        help_text = source.read_text(encoding='utf-8').split('const help = `', 1)[1].split('`', 1)[0]
        advertised = {tuple(part for part in match if part) for match in
                      re.findall(r'^  tools github ([a-z]+)(?: ([a-z]+))?', help_text, re.M)}
        measured = {('branch', 'cleanup') if task == 'cleanup-apply' else tuple(task.split('-'))
                    for task in benchmark.TASKS}
        self.assertEqual(advertised, measured)
        self.assertEqual(len(measured), len(benchmark.TASKS))
        self.assertEqual(set(benchmark.TASKS), set(benchmark.COMMANDS) - {'cleanup-preview'})

    def test_rerun_fixture_requires_new_attempt_and_rejects_duplicate_write(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            server = benchmark.core.Backend(root)
            server.RequestHandlerClass = benchmark.Handler
            benchmark.setup(root, server, 'ci-rerun')
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            base = f'http://127.0.0.1:{server.server_port}/repos/{benchmark.core.REPO}/actions/runs/42'

            def request(path='', method='GET'):
                req = Request(base + path, method=method, data=b'{}' if method == 'POST' else None)
                with urlopen(req) as response:
                    return json.load(response)

            try:
                self.assertEqual(request()['run_attempt'], 2)
                request('/rerun-failed-jobs', 'POST')
                active = request()
                self.assertEqual((active['run_attempt'], active['status']), (3, 'in_progress'))
                final = request()
                self.assertEqual((final['run_attempt'], final['status'], final['conclusion']), (3, 'completed', 'failure'))
                with self.assertRaises(HTTPError) as error:
                    request('/rerun-failed-jobs', 'POST')
                self.assertEqual(error.exception.code, 409)
                self.assertEqual(server.reruns, 1)
            finally:
                server.shutdown()
                server.server_close()
                thread.join()


if __name__ == '__main__':
    unittest.main()
