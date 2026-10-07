import copy
import unittest

from github_candidates import ci, delta, go_ci_facts, parameter_count, reviews


def thread(name, resolved=False, outdated=False):
    return {'id': name, 'path': 'sample.go', 'line': None,
            'isResolved': resolved, 'isOutdated': outdated,
            'comments': {'pageInfo': {'hasNextPage': False},
                         'nodes': [{'body': 'Required change'}]}}


class CandidateValidation(unittest.TestCase):
    def test_parameter_count_handles_nested_types_and_tags(self):
        self.assertEqual(parameter_count('(int, func(a string, b ...any), map[string]struct{Tag string `json:"a,b"`}, []byte)'), 4)
        self.assertEqual(parameter_count('()'), 0)

    def test_parameter_count_rejects_truncation_and_bad_delimiters(self):
        for signature in ('(int, func(a string)', '(map[string)int)', '(int,)', '(,int)', '(int) extra'):
            with self.subTest(signature=signature), self.assertRaises(ValueError):
                parameter_count(signature)

    def test_go_facts_are_computed_for_different_symbols_and_errors(self):
        text = '\n'.join([
            'job\tbuild\t2026-10-07T00:00:00Z ##[error]custom/file.go:7:2: too many arguments in call to other.Build',
            'job\tbuild\t2026-10-07T00:00:00Z want (int, func(a string, b bool))',
            'job\tbuild\t2026-10-07T00:00:00Z ##[error]custom/file.go:9:4: T does not implement I (missing method Flush)',
            'job\tbuild\t2026-10-07T00:00:00Z ##[error]Process completed with exit code 2.',
        ])
        result = go_ci_facts(text)
        self.assertEqual(result['status'], 'recognized')
        self.assertEqual(result['expected_arguments'], {'other.Build': 2})
        self.assertEqual(result['missing_interface_methods'], ['Flush'])
        self.assertEqual(result['exit_codes'], [2])
        self.assertEqual([d['line'] for d in result['diagnostics']], [7, 9])

    def test_unrecognized_failure_retains_full_evidence(self):
        text = '##[error]network disconnected\nProcess completed with exit code 1.'
        result = go_ci_facts(text)
        self.assertEqual(result['status'], 'needs_context')
        self.assertEqual(result['fallback_log'], text)

    def test_malformed_wanted_signature_requires_context(self):
        text = 'sample.go:5:3: too many arguments in call to pkg.Call\nwant (int, func(a int)\nProcess completed with exit code 1.'
        result = go_ci_facts(text)
        self.assertEqual(result['status'], 'needs_context')
        self.assertNotIn('pkg.Call', result['expected_arguments'])
        self.assertEqual(result['fallback_log'], text)

    def test_raw_timestamped_log_with_tabs_is_not_treated_as_job_prefix(self):
        text = '2026-10-07T00:00:00Z sample.go:5:3: cannot use x\n2026-10-07T00:00:00Z FAIL\tpackage\t0.5s\n2026-10-07T00:00:00Z Process completed with exit code 1.'
        result = go_ci_facts(text)
        self.assertEqual(result['status'], 'recognized')
        self.assertEqual(result['diagnostics'][0]['path'], 'sample.go')

    def test_empty_failure_log_requires_more_evidence(self):
        with self.assertRaises(ValueError):
            ci('')

    def test_bom_does_not_hide_timestamp_or_error(self):
        result = ci('job\tstep\t\ufeff2026-10-07T00:00:00Z ##[error]failed')
        self.assertEqual(result['failure_signatures'], ['##[error]failed'])

    def test_matrix_dedup_preserves_platforms_and_different_failures(self):
        text = '\n'.join(f'{job}\ttest\t2026-10-07T00:00:00Z {line}'
                         for job, line in [('linux', 'panic: broken pipe'),
                                           ('windows', 'panic: broken pipe'),
                                           ('macos', 'panic: distinct failure')])
        result = ci(text, context=0)
        self.assertEqual(len(result['blocks']), 2)
        self.assertEqual(len(result['blocks'][0]['occurrences']), 2)
        self.assertEqual(result['failed_jobs'], ['linux', 'macos', 'windows'])
        self.assertEqual(len(result['failure_signatures']), 2)

    def test_unknown_failure_is_not_discarded(self):
        result = ci('job\tstep\tthe runner silently disconnected\njob\tstep\tadditional evidence')
        self.assertFalse(result['complete_diagnosis'])
        self.assertEqual(result['blocks'][0]['lines'],
                         ['the runner silently disconnected', 'additional evidence'])

    def test_context_before_and_after_diagnostic(self):
        text = '\n'.join(['setup', 'expected: 10', 'actual: 20', 'AssertionError: mismatch', 'stack frame', 'tail'])
        self.assertEqual(ci(text, context=2)['blocks'][0]['lines'], text.splitlines()[1:])

    def test_overlapping_windows_keep_separate_diagnostics(self):
        result = ci('setup\npanic: first\ncontext\npanic: second\ntail', context=2)
        self.assertEqual(len(result['blocks']), 1)
        self.assertEqual(len(result['failure_signatures']), 2)

    def review_snapshot(self):
        return {'data': {'repository': {'pullRequest': {'reviewThreads': {
            'pageInfo': {'hasNextPage': False},
            'nodes': [thread('resolved', resolved=True), thread('current'), thread('old', outdated=True)]}}}}}

    def test_outdated_unresolved_review_is_preserved(self):
        result = reviews(self.review_snapshot())
        self.assertEqual([t['id'] for t in result['threads']], ['current', 'old'])
        self.assertTrue(result['threads'][-1]['is_outdated'])

    def test_more_thread_pages_require_full_collection(self):
        data = self.review_snapshot()
        data['data']['repository']['pullRequest']['reviewThreads']['pageInfo']['hasNextPage'] = True
        with self.assertRaises(ValueError):
            reviews(data)

    def test_more_comment_pages_require_full_collection(self):
        data = self.review_snapshot()
        data['data']['repository']['pullRequest']['reviewThreads']['nodes'][1]['comments']['pageInfo']['hasNextPage'] = True
        with self.assertRaises(ValueError):
            reviews(data)

    def delta_snapshot(self):
        return {'status': 'ahead', 'base_commit': {'sha': 'base'},
                'total_commits': 1, 'commits': [{'sha': 'head'}],
                'files': [{'filename': 'new.go', 'previous_filename': 'old.go',
                           'status': 'renamed', 'additions': 1, 'deletions': 0, 'patch': '+new'},
                          {'filename': 'binary.dat', 'status': 'modified', 'additions': 0, 'deletions': 0}]}

    def test_delta_preserves_rename_and_missing_patch(self):
        result = delta(self.delta_snapshot(), 'base', 'head')
        self.assertEqual(result['files'][0]['previous_filename'], 'old.go')
        self.assertIsNone(result['files'][1]['patch'])

    def test_diverged_or_force_pushed_head_requires_refresh(self):
        for status in ('diverged', 'behind'):
            data = self.delta_snapshot()
            data['status'] = status
            with self.assertRaises(ValueError):
                delta(data, 'base', 'head')

    def test_delta_verifies_head_and_baseline(self):
        for since, head in [('wrong', 'head'), ('base', 'wrong')]:
            with self.assertRaises(ValueError):
                delta(self.delta_snapshot(), since, head)

    def test_delta_does_not_silently_accept_missing_commits(self):
        data = self.delta_snapshot()
        data['total_commits'] = 2
        with self.assertRaises(ValueError):
            delta(data, 'base', 'head')

    def test_delta_does_not_silently_accept_file_limit(self):
        data = self.delta_snapshot()
        data['files'] = [copy.deepcopy(data['files'][0]) for _ in range(300)]
        with self.assertRaises(ValueError):
            delta(data, 'base', 'head')

    def test_identical_snapshot_has_no_changes(self):
        result = delta({'status': 'identical', 'base_commit': {'sha': 'base'},
                        'total_commits': 0, 'commits': [], 'files': []}, 'base', 'base')
        self.assertEqual(result['files'], [])


if __name__ == '__main__':
    unittest.main()
