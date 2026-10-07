import copy
import json
import tempfile
import unittest
from pathlib import Path

from workflow_reports import acknowledge, apply_replacement, code_context, empty_state, prepare, report_from_events


def sample(output='sample_test.go:8: expected 2, got 1\n', **metadata):
    events = [{'Action': 'start', 'Package': 'example.com/fixture'},
              {'Action': 'output', 'Package': 'example.com/fixture', 'Test': 'TestExample', 'Output': output},
              {'Action': 'fail', 'Package': 'example.com/fixture', 'Test': 'TestExample'},
              {'Action': 'fail', 'Package': 'example.com/fixture'}]
    options = {'repo': 'example/project', 'revision': 'rev1', 'job': 'linux', 'run_id': '1'} | metadata
    return report_from_events('\n'.join(json.dumps(e) for e in events), 1, **options)


class WorkflowReportValidation(unittest.TestCase):
    def test_old_acknowledged_failure_does_not_resurrect_after_pass(self):
        failed = sample()
        state = acknowledge(failed, empty_state(), verification_passed=True)
        passed = report_from_events(json.dumps({'Action': 'pass', 'Package': 'example.com/fixture'}), 0,
                                    repo='example/project', revision='rev2', job='linux', run_id='2')
        state = acknowledge(passed, state)
        result = prepare(failed, state)
        self.assertFalse(result['model_needed'])
        self.assertEqual(result['delta'], {'added': [], 'changed': [], 'resolved': []})

    def test_panic_without_test_terminal_retains_stack_evidence(self):
        events = [{'Action': 'run', 'Package': 'example.com/fixture', 'Test': 'TestPanic'},
                  {'Action': 'output', 'Package': 'example.com/fixture', 'Test': 'TestPanic', 'Output': 'panic: important cause\nstack frame\n'},
                  {'Action': 'output', 'Package': 'example.com/fixture', 'Output': 'FAIL\n'},
                  {'Action': 'fail', 'Package': 'example.com/fixture'}]
        report = report_from_events('\n'.join(json.dumps(e) for e in events), 1,
                                    repo='example/project', revision='rev', job='test', run_id='1')
        self.assertTrue(report['complete'])
        self.assertIn('important cause', report['failures'][0]['output'])

    def test_explicit_expected_revision_rejects_stale_report(self):
        with self.assertRaises(ValueError):
            prepare(sample(), empty_state(), expected_revision='different-revision')

    def test_replacement_verifies_preimage_and_change_scope(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'source.go').write_text('original')
            prepared = code_context(root, ['source.go'])[0]
            with self.assertRaises(ValueError):
                apply_replacement(root, 'source.go', 'changed', prepared['sha256'], ['other.go'])
            (root / 'source.go').write_text('concurrent edit')
            with self.assertRaises(ValueError):
                apply_replacement(root, 'source.go', 'changed', prepared['sha256'], ['source.go'])
            self.assertEqual((root / 'source.go').read_text(), 'concurrent edit')

    def test_replacement_applies_matching_utf8_content(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'source.go').write_text('original')
            prepared = code_context(root, ['source.go'])[0]
            apply_replacement(root, 'source.go', '// 변경\n', prepared['sha256'], ['source.go'])
            self.assertEqual((root / 'source.go').read_text(encoding='utf-8'), '// 변경\n')

    def test_failed_test_has_complete_evidence_without_package_duplicate(self):
        report = sample()
        self.assertTrue(report['complete'])
        self.assertEqual(len(report['failures']), 1)
        self.assertIn('expected 2, got 1', report['failures'][0]['output'])

    def test_observation_does_not_acknowledge_failed_work(self):
        state = empty_state()
        self.assertTrue(prepare(sample(), state)['model_needed'])
        self.assertTrue(prepare(sample(), state)['model_needed'])
        self.assertEqual(state, empty_state())

    def test_only_verified_acknowledgement_suppresses_exact_duplicate(self):
        report = sample()
        with self.assertRaises(ValueError):
            acknowledge(report, empty_state())
        state = acknowledge(report, empty_state(), verification_passed=True)
        self.assertFalse(prepare(report, state)['model_needed'])
        self.assertEqual(prepare(report, state)['reason'], 'acknowledged_event')

    def test_changed_code_and_new_run_do_not_hide_same_failure(self):
        state = acknowledge(sample(), empty_state(), verification_passed=True)
        for metadata in ({'revision': 'rev2'}, {'run_id': '2'}, {'job': 'windows'}, {'repo': 'other/project'}):
            with self.subTest(metadata=metadata):
                self.assertTrue(prepare(sample(**metadata), state)['model_needed'])

    def test_changed_assertion_is_returned_in_delta(self):
        state = acknowledge(sample(), empty_state(), verification_passed=True)
        result = prepare(sample('sample_test.go:8: expected 3, got 1\n', run_id='2'), state)
        self.assertEqual(len(result['delta']['changed']), 1)
        self.assertIn('expected 3', result['delta']['changed'][0]['output'])

    def test_passed_report_resolves_failure_without_model(self):
        state = acknowledge(sample(), empty_state(), verification_passed=True)
        report = report_from_events(json.dumps({'Action': 'pass', 'Package': 'example.com/fixture'}), 0,
                                    repo='example/project', revision='rev2', job='linux', run_id='2')
        result = prepare(report, state)
        self.assertFalse(result['model_needed'])
        self.assertEqual(len(result['delta']['resolved']), 1)

    def test_invalid_or_truncated_events_require_attention(self):
        for text in ('', '{bad JSON', json.dumps({'Action': 'run', 'Package': 'example.com/fixture'})):
            report = report_from_events(text, 1, repo='example/project', revision='rev', job='test', run_id='1')
            self.assertFalse(report['complete'])
            self.assertTrue(prepare(report, empty_state())['model_needed'])
            with self.assertRaises(ValueError):
                acknowledge(report, empty_state(), verification_passed=True)

    def test_build_failure_without_test_is_not_lost(self):
        events = [{'Action': 'build-output', 'ImportPath': 'example.com/fixture', 'Output': 'sample.go:5: bad compile\n'},
                  {'Action': 'build-fail', 'ImportPath': 'example.com/fixture'}]
        report = report_from_events('\n'.join(json.dumps(e) for e in events), 1,
                                    repo='example/project', revision='rev', job='test', run_id='1')
        self.assertTrue(report['complete'])
        self.assertIn('bad compile', report['failures'][0]['output'])

    def test_timing_noise_does_not_change_failure_fingerprint(self):
        a = sample('--- FAIL: TestExample (0.01s)\nassertion\n')
        b = sample('--- FAIL: TestExample (0.98s)\nassertion\n')
        self.assertEqual(a['failures'][0]['fingerprint'], b['failures'][0]['fingerprint'])

    def test_tampered_fingerprint_or_schema_is_rejected(self):
        report = sample()
        report['failures'][0]['output'] = 'different error'
        with self.assertRaises(ValueError):
            prepare(report, empty_state())
        report = sample()
        report['schema_version'] = 99
        with self.assertRaises(ValueError):
            prepare(report, empty_state())

    def test_context_paths_cannot_escape_checkout(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / 'repo'
            root.mkdir()
            (Path(temporary) / 'outside.go').write_text('outside')
            with self.assertRaises(ValueError):
                code_context(root, ['../outside.go'])


if __name__ == '__main__':
    unittest.main()
