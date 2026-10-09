import unittest

from run_command_benchmark import BRANCH, resume_evidence


class ResumeEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.api = [{'method': 'POST', 'endpoint': '/graphql', 'payload': {'query': 'query { linkedBranches { nodes { ref { name } } } }'}}]
        self.git = [
            {'kind': 'git', 'args': ['fetch', '--no-tags', 'origin', f'refs/heads/{BRANCH}:refs/remotes/origin/{BRANCH}'], 'exit_code': 0},
            {'kind': 'git', 'args': ['switch', '--create', BRANCH, '--track', 'origin/' + BRANCH], 'exit_code': 0},
            {'kind': 'git', 'args': ['fetch', '--no-tags', 'origin', f'refs/heads/{BRANCH}:refs/remotes/origin/{BRANCH}'], 'exit_code': 0},
            {'kind': 'git', 'args': ['switch', BRANCH], 'exit_code': 0},
        ]

    def test_graphql_ref_and_two_git_operations_need_no_second_rest_lookup(self):
        self.assertTrue(all(resume_evidence(self.api, self.git).values()))

    def test_single_checkout_is_not_a_repeated_operation(self):
        self.assertFalse(resume_evidence(self.api, self.git[:-1])['resume_checkout_performed'])

    def test_failed_second_fetch_is_not_successful_resume(self):
        self.git[2]['exit_code'] = 1
        self.assertFalse(resume_evidence(self.api, self.git)['resume_fetch_verified'])

    def test_existing_fetched_tracking_ref_can_be_reused_for_second_checkout(self):
        del self.git[2]
        self.assertTrue(all(resume_evidence(self.api, self.git).values()))

    def test_existing_development_association_must_be_checked(self):
        self.assertFalse(resume_evidence([], self.git)['resume_link_checked'])


if __name__ == '__main__':
    unittest.main()
