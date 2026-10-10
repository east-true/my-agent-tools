import json
import tempfile
import unittest
from importlib.util import find_spec
from pathlib import Path
from types import SimpleNamespace

@unittest.skipUnless(find_spec('graphql'), 'pinned GraphQL dependency unavailable')
class UsageBenchmarkContract(unittest.TestCase):

    def test_nested_thread_comments_do_not_prove_ordinary_conversation_collection(self):
        import run_usage_benchmark as study
        query = 'query {repository(owner:"fixture",name:"command-benchmark"){pullRequest(number:7){reviewThreads(first:100){nodes{comments(first:100){nodes{body}}}}}}}'
        fields = study.query_fields(query)
        self.assertNotIn(('repository', 'pullRequest', 'comments'), fields)
        query = 'query {r:repository(owner:"fixture",name:"command-benchmark"){p:pullRequest(number:7){...F}}} fragment F on PullRequest {c:comments(first:100){nodes{body}} reviews(first:100){nodes{state}}}'
        fields = study.query_fields(query)
        self.assertIn(('repository', 'pullRequest', 'comments'), fields)
        self.assertIn(('repository', 'pullRequest', 'reviews'), fields)

    def test_head_observation_before_the_actual_edit_is_insufficient(self):
        import run_usage_benchmark as study
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            work = root / 'workspace'
            work.mkdir()
            initial = b'retry_budget=2\r\ntimeout=30\r\n'
            final = b'retry_budget=9\r\ntimeout=30\r\n'
            (work / 'settings.py').write_bytes(initial)
            before = study.action.inventory(work)
            server = SimpleNamespace(accesses=[{'method': 'POST', 'endpoint': '/graphql', 'payload': {'query': study.action.REVIEW_QUERY}}, {'method': 'GET', 'endpoint': '/repos/fixture/command-benchmark/pulls/7', 'settings_sha256': study.sha(initial)}])
            (work / 'settings.py').write_bytes(final)
            self.assertFalse(study.verify(root, server, 'pr-reviews', before)['correct'])
            server.accesses.append({'method': 'GET', 'endpoint': '/repos/fixture/command-benchmark/pulls/7', 'settings_sha256': study.sha(final)})
            (work / 'some-authored-input.data').write_text('{}')
            self.assertTrue(study.verify(root, server, 'pr-reviews', before)['correct'])
            (work / 'settings.py').chmod(448)
            self.assertFalse(study.verify(root, server, 'pr-reviews', before)['correct'])

    def test_all_trials_and_repetition_specific_inputs_are_preregistered(self):
        import run_usage_benchmark as study
        steps = study.schedule()
        self.assertEqual(len(steps), 30)
        self.assertEqual(len({tuple(sorted(s.items())) for s in steps}), 30)
        for task in study.TASKS:
            for repetition in range(1, 4):
                self.assertEqual({s['method'] for s in steps if s['task'] == task and s['repetition'] == repetition}, {'direct', 'tools'})
        for repetition in range(1, 4):
            files, expected, occurrence = study.markdown_fixture(repetition)
            self.assertEqual(occurrence, 2 if repetition == 2 else 1)
            self.assertEqual(sorted(files), [f['path'] for f in expected['files']])
            for row in expected['files']:
                self.assertEqual(study.sha(files[row['path']]), row['sha256'])
                self.assertNotIn('fake heading', [h['title'] for h in row['headings']])
        _, third = study.junit_fixture(3)
        self.assertFalse(third['complete'])
        self.assertEqual(third['problems'], ['reports/test/TEST-broken.xml'])
class APICallTradeoff(unittest.TestCase):
    def test_lower_time_and_tokens_with_more_api_calls_are_conditional(self):
        from usage_benchmark_docs import assessment
        def group(total, uncached, seconds, api):
            return {"metrics": {key: {"mean": value} for key, value in zip(("input_plus_output", "uncached_plus_output", "seconds", "command_calls", "api_calls"), (total, uncached, seconds, 1, api))}}
        summary = {"comparison_eligible": True, "direct": group(100, 80, 20, 1), "tools": group(70, 60, 10, 3)}
        self.assertEqual(assessment(summary), "조건부·API 호출 증가")
        summary["tools"]["metrics"]["api_calls"]["mean"] = 1
        self.assertEqual(assessment(summary), "표본에서 AND 충족")


if __name__ == '__main__':
    unittest.main()
