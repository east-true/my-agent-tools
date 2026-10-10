import copy
import json
import tempfile
import unittest
from importlib.util import find_spec
from pathlib import Path

from actionable_benchmark_docs import assessment, render


class JointAssessment(unittest.TestCase):
    def sample(self):
        metrics=lambda total,uncached,seconds,calls:{'metrics':{key:{'mean':value} for key,value in zip(('input_plus_output','uncached_plus_output','seconds','command_calls'),(total,uncached,seconds,calls))}}
        return {'comparison_eligible':True,'direct':metrics(100,80,20,3),'tools':metrics(70,60,10,2)}

    def test_token_savings_without_speed_improvement_are_partial(self):
        value=self.sample();self.assertEqual(assessment(value),'표본에서 AND 충족')
        value['tools']['metrics']['seconds']['mean']=22
        self.assertEqual(assessment(value),'부분 개선·AND 미충족')
        value['comparison_eligible']=False
        self.assertEqual(assessment(value),'정답/상태 부족·판정 보류')

    def test_more_calls_or_conflicting_cache_metrics_are_not_unconditional_and(self):
        value=self.sample();value['tools']['metrics']['command_calls']['mean']=4
        self.assertEqual(assessment(value),'부분 개선·AND 미충족')
        value=self.sample();value['tools']['metrics']['uncached_plus_output']['mean']=90
        self.assertEqual(assessment(value),'조건부·캐시 지표 상충')

    def test_latest_command_pages_and_readme_rows_are_stable_on_regeneration(self):
        repo=Path(__file__).resolve().parents[2]
        report=repo/'docs/benchmarks/actionable/data/study.json'
        if not report.exists():self.skipTest('published actionable report unavailable')
        with tempfile.TemporaryDirectory() as directory:
            target=Path(directory)
            for relative in ('README.md','docs/README.ko.md','docs/benchmarks/README.md','docs/benchmarks/github/README.md','docs/benchmarks/fs/README.md','docs/benchmarks/actionable/data/study.json','docs/benchmarks/actionable/data/native.json'):
                p=target/relative;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes((repo/relative).read_bytes())
            (target/'docs/benchmarks/github/pr').mkdir(parents=True)
            render(target)
            before={str(p.relative_to(target)):p.read_bytes() for p in target.rglob('*.md')}
            render(target)
            self.assertEqual(before,{str(p.relative_to(target)):p.read_bytes() for p in target.rglob('*.md')})
            readme=(target/'README.md').read_text(encoding='utf-8')
            self.assertEqual(readme.count('[`fs apply: validation`]'),1)
            self.assertEqual(readme.count('[`fs apply: shared edits`]'),1)
            self.assertIn('모델 실행을 교체하지 않았으며',(target/'docs/benchmarks/fs/apply.md').read_text(encoding='utf-8'))


@unittest.skipUnless(find_spec('graphql'),'pinned GraphQL dependency unavailable')
class AuthoredInputReassessment(unittest.TestCase):
    def test_only_exact_authorized_input_with_retained_sha_and_commands_is_reassessed(self):
        from publish_actionable_benchmark import reassess_authored_input
        import run_actionable_benchmark as study
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);run=root/'runs/apply-groups/03-tools';run.mkdir(parents=True)
            spec={'version':1,'groups':[{'paths':[f'src/config_{i:02d}.txt' for i in range(12)],'replacements':study.replacements()}]}
            blob=json.dumps(spec,separators=(',',':')).encode()
            state={'after':{'batch-spec.json':{'sha256':study.digest(blob)}}}
            (run/'state-verification.json').write_text(json.dumps(state),encoding='utf-8')
            row={'correct':False,'task':'apply-groups','method':'tools','repetition':2,'index':3,'answer_correct':True,'state_correct':False,
                 'state_checks':{'no_unexpected_files':False,'exact_files_permissions_and_saved_preimages':True},'commands':['tools fs apply --spec batch-spec.json --apply']}
            corrected,changes=reassess_authored_input(root,[row])
            self.assertTrue(corrected[0]['correct']);self.assertEqual(len(changes),1)
            self.assertFalse(row['correct']);self.assertFalse(row['state_checks']['no_unexpected_files'])
            state['after']['batch-spec.json']['sha256']='bad'
            (run/'state-verification.json').write_text(json.dumps(state),encoding='utf-8')
            corrected,changes=reassess_authored_input(root,[row])
            self.assertFalse(corrected[0]['correct']);self.assertEqual(changes,[])


if __name__=='__main__':unittest.main()
