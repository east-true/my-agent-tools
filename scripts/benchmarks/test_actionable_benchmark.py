import copy
import json
import tempfile
import unittest
from importlib.util import find_spec
from pathlib import Path


@unittest.skipUnless(find_spec('graphql'), 'pinned GraphQL dependency unavailable')
class ActionableBenchmarkContract(unittest.TestCase):
    def test_authored_spec_matches_real_integer_schema_and_requested_scope(self):
        import run_actionable_benchmark as study
        paths=['a.txt','b.txt'];value={'version':1,'groups':[{'paths':paths,'replacements':study.replacements()}]}
        self.assertTrue(study.valid_authored_spec(value,paths))
        invalid=copy.deepcopy(value);invalid['version']=True
        self.assertFalse(study.valid_authored_spec(invalid,paths))
        invalid=copy.deepcopy(value);invalid['groups'][0]['replacements'][0]['count']=True
        self.assertFalse(study.valid_authored_spec(invalid,paths))
        invalid=copy.deepcopy(value);invalid['groups'][0]['paths']=['a.txt','a.txt']
        self.assertFalse(study.valid_authored_spec(invalid,paths))

    def test_schedule_has_three_alternating_pairs_for_each_changed_workflow(self):
        import run_actionable_benchmark as study
        steps=study.schedule()
        self.assertEqual(len(steps),24)
        self.assertEqual(len({tuple(sorted(s.items())) for s in steps}),24)
        for task in study.TASKS:
            orders=[[s['method'] for s in steps if s['task']==task and s['repetition']==r] for r in (1,2,3)]
            self.assertEqual(orders[0],orders[2])
            self.assertNotEqual(orders[0],orders[1])
            for order in orders:self.assertEqual(set(order),{'direct','tools'})

    def test_exact_final_state_and_saved_plan_are_both_required(self):
        import run_actionable_benchmark as study
        with tempfile.TemporaryDirectory() as name:
            root=Path(name);server=study.core.Backend(root)
            try:
                before,_=study.setup(root,server,'apply-groups')
                work=root/'workspace'
                plan={'version':1,'files':[]}
                for i,path in enumerate(study.expected('apply-groups')['applied']):
                    eol='\r\n' if i%2 else '\n'
                    (work/path).write_bytes((eol.join(['cache_limit=64','retry_budget=5','keep=unchanged'])+eol).encode())
                    plan['files'].append({'path':path,'sha256':before[path]['sha256'],'replacements':study.replacements()})
                saved=work/'plan.json';saved.write_text(json.dumps(plan),encoding='utf-8')
                self.assertTrue(study.verify(root,server,'apply-groups',before)['correct'])
                authored={'version':1,'groups':[{'paths':study.expected('apply-groups')['applied'],'replacements':study.replacements()}]}
                (work/'batch-spec.json').write_text(json.dumps(authored),encoding='utf-8')
                self.assertTrue(study.verify(root,server,'apply-groups',before)['correct'])
                authored['groups'][0]['paths']=['unrequested.txt']
                (work/'batch-spec.json').write_text(json.dumps(authored),encoding='utf-8')
                self.assertFalse(study.verify(root,server,'apply-groups',before)['correct'])
                (work/'batch-spec.json').unlink()
                duplicate=copy.deepcopy(plan);duplicate['files'][-1]=duplicate['files'][0]
                saved.write_text(json.dumps(duplicate),encoding='utf-8')
                self.assertFalse(study.verify(root,server,'apply-groups',before)['correct'])
                plan['files'][0]['replacements'][0]['count']=99
                saved.write_text(json.dumps(plan),encoding='utf-8')
                self.assertFalse(study.verify(root,server,'apply-groups',before)['correct'])
            finally:server.server_close()

    def test_all_count_errors_and_no_writes_have_a_shared_reference(self):
        import run_actionable_benchmark as study
        with tempfile.TemporaryDirectory() as name:
            root=Path(name);server=study.core.Backend(root)
            try:
                before,_=study.setup(root,server,'apply-errors')
                reference=study.expected('apply-errors')
                self.assertEqual(len(reference['diagnostics']),8)
                self.assertEqual({r['actual_count'] for r in reference['diagnostics']},{0,2})
                self.assertTrue(study.verify(root,server,'apply-errors',before)['correct'])
                (root/'workspace/src/config_00.txt').write_text('unexpected edit',encoding='utf-8')
                self.assertFalse(study.verify(root,server,'apply-errors',before)['correct'])
            finally:server.server_close()

    def test_model_failure_is_retained_and_withholds_comparison(self):
        import run_actionable_benchmark as study
        rows=[]
        for repetition in (1,2,3):
            for method in ('direct','tools'):
                rows.append({'task':'apply-errors','method':method,'repetition':repetition,'usage':{},'correct':not(method=='tools' and repetition==2),
                    'state_correct':True,**{k:100 for k in ('input_plus_output','uncached_plus_output','uncached_input','cached_input','output_tokens','api_calls','command_calls','seconds')}})
        summary=study.summarize(rows)['apply-errors']
        self.assertEqual(summary['tools']['n'],3)
        self.assertEqual(summary['tools']['correct'],2)
        self.assertFalse(summary['comparison_eligible'])
        self.assertNotIn('change_percent',summary)


if __name__=='__main__':unittest.main()
