import json
import tempfile
import copy
import unittest
from importlib.util import find_spec
from pathlib import Path


@unittest.skipUnless(find_spec('graphql'), 'optional pinned GraphQL benchmark dependency unavailable')
class PublishedStudyContract(unittest.TestCase):
    def setUp(self):
        import run_github_study as study
        self.study=study
        self.directory=tempfile.TemporaryDirectory()
        self.root=Path(self.directory.name)
        self.server=study.core.Backend(self.root)
        study.setup(self.root,self.server,'pr-merge')

    def tearDown(self):
        self.server.server_close()
        self.directory.cleanup()

    def test_small_alias_and_fragment_queries_are_valid_and_project_fields(self):
        graph=self.server.graphql
        self.server.merged=True
        self.assertEqual(graph.execute({'query':'{repository(owner:"fixture",name:"command-benchmark"){pullRequest(number:7){done:merged}}}'}),
                         {'data':{'repository':{'pullRequest':{'done':True}}}})
        result=graph.execute({'query':'query Minimal($n:Int!){repository(owner:"fixture",name:"command-benchmark"){pullRequest(number:$n){...Identity}}} fragment Identity on PullRequest{sha:headRefOid}',
                              'variables':{'n':7}})
        self.assertEqual(result,{'data':{'repository':{'pullRequest':{'sha':self.server.head}}}})
        invalid=graph.execute({'query':'{repository(owner:"fixture",name:"command-benchmark"){pullRequest(number:7){notARealField}}}'})
        self.assertTrue(invalid['errors'])

    def test_complete_schedule_crosses_pair_order_and_has_no_duplicate_slots(self):
        steps=self.study.schedule()
        self.assertEqual(len(steps),90)
        self.assertEqual(len({tuple(step.items()) for step in steps}),90)
        for task in self.study.TASKS:
            orders=[]
            for repetition in (1,2,3):
                pair=[step['method'] for step in steps if step['task']==task and step['repetition']==repetition]
                self.assertEqual(set(pair),{'gh','tools'})
                orders.append(pair)
            self.assertNotEqual(orders[0],orders[1])
            self.assertEqual(orders[0],orders[2])

    def test_selected_schedule_keeps_pairs_and_excludes_other_commands(self):
        selected=['pr-reviews','pr-inspect']
        steps=self.study.schedule(selected)
        self.assertEqual(steps,[step for step in self.study.schedule() if step['task'] in selected])
        self.assertEqual(len(steps),12)
        self.assertEqual({step['task'] for step in steps},set(selected))
        for task in selected:
            for method in ('gh','tools'):
                self.assertEqual(sorted(step['repetition'] for step in steps if step['task']==task and step['method']==method),[1,2,3])
        self.assertEqual(set(self.study.summarize([],selected)),set(selected))

    def test_failures_are_included_in_metrics_but_prevent_savings_claim(self):
        rows=[]
        for repetition in (1,2,3):
            for method,value in [('gh',100),('tools',50)]:
                rows.append({'task':'context','method':method,'repetition':repetition,'usage':{},'correct':not(method=='gh' and repetition==2),
                             'state_correct':True,**{key:value for key in ('input_plus_output','uncached_plus_output','uncached_input',
                             'cached_input','output_tokens','api_calls','command_calls','seconds')}})
        summary=self.study.summarize(rows)['context']
        self.assertEqual(summary['gh']['metrics']['input_plus_output']['mean'],100)
        self.assertEqual(summary['gh']['n'],3)
        self.assertFalse(summary['comparison_eligible'])
        self.assertNotIn('change_percent',summary)


class PartialStudyDocuments(unittest.TestCase):
    def test_new_partial_results_replace_only_selected_commands(self):
        from github_benchmark_docs import latest_results,render
        repo=Path(__file__).resolve().parents[2]
        base=json.loads((repo/'docs/benchmarks/github/data/study.json').read_text(encoding='utf-8'))
        update=copy.deepcopy(base)
        update['protocol']['tasks']=['pr-reviews','pr-inspect']
        update['protocol']['measured_at_utc']='2026-10-09T17:30:00+00:00'
        update['protocol']['configured_defaults']['model_reasoning_effort']='low'
        update['trials']=[r for r in update['trials'] if r['task'] in update['protocol']['tasks']]
        update['summary']={task:update['summary'][task] for task in update['protocol']['tasks']}
        reports={'study':base,'study-partial':update}
        chosen=latest_results(reports)
        for task in base['protocol']['tasks']:
            self.assertIs(chosen[task][1],update if task in update['protocol']['tasks'] else base)
        with tempfile.TemporaryDirectory() as directory:
            target=Path(directory)
            render(target,reports)
            reviews=(target/'docs/benchmarks/github/pr/reviews.md').read_text(encoding='utf-8')
            create=(target/'docs/benchmarks/github/pr/create.md').read_text(encoding='utf-8')
            self.assertIn('2026-10-10',reviews)
            self.assertIn('`low`',reviews)
            self.assertIn('data/study-partial.json',reviews)
            self.assertIn('2026-10-08',create)
            self.assertIn('`high`',create)
            self.assertIn('data/study.json',create)
            common=(target/'docs/benchmarks/github/README.md').read_text(encoding='utf-8')
            self.assertIn('12회',common)
            self.assertIn('90회',common)

    def test_earlier_partial_batch_cannot_replace_newer_full_batch(self):
        from github_benchmark_docs import latest_results
        repo=Path(__file__).resolve().parents[2]
        base=json.loads((repo/'docs/benchmarks/github/data/study.json').read_text(encoding='utf-8'))
        old=copy.deepcopy(base)
        old['protocol']['tasks']=['pr-reviews']
        old['protocol']['measured_at_utc']='2026-10-01T00:00:00+00:00'
        selected=latest_results({'study':base,'study-old':old})
        self.assertIs(selected['pr-reviews'][1],base)


if __name__=='__main__':unittest.main()
