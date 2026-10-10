import unittest
from copy import deepcopy
import run_fs_benchmark as benchmark
from fs_workflow_benchmark import workflow_prompt
from fs_completion_benchmark import completion_prompt
from publish_fs_benchmark import and_assessment


class FilesystemBenchmarkContract(unittest.TestCase):
    def test_fixed_schedule_has_exactly_eighteen_balanced_runs(self):
        steps=benchmark.schedule()
        self.assertEqual(len(steps),18)
        self.assertEqual(len({(s['task'],s['method'],s['repetition']) for s in steps}),18)
        for task in benchmark.TASKS:
            pairs=[[s['method'] for s in steps if s['task']==task and s['repetition']==n] for n in (1,2,3)]
            self.assertNotEqual(pairs[0],pairs[1])
            self.assertEqual(pairs[0],pairs[2])

    def test_followup_schedule_has_only_selected_pairs(self):
        steps=benchmark.schedule(['delta','apply'])
        self.assertEqual(len(steps),12)
        self.assertEqual({s['task'] for s in steps},{'delta','apply'})
        self.assertEqual(set(benchmark.summarize([],['delta','apply'])),{'delta','apply'})

    def test_failed_trial_is_not_excluded_and_withholds_comparison(self):
        rows=[]
        for task in benchmark.TASKS:
            for method in ('direct','tools'):
                for repetition in (1,2,3):
                    rows.append({'task':task,'method':method,'repetition':repetition,'usage':{},'correct':not(task=='inspect' and method=='direct' and repetition==2),'uncached_plus_output':100,'input_plus_output':200,'command_calls':1,'shell_output_bytes':20,'seconds':1})
        result=benchmark.summarize(rows)
        self.assertEqual(result['inspect']['direct']['n'],3)
        self.assertFalse(result['inspect']['comparison_eligible'])
        self.assertNotIn('change_percent',result['inspect'])
        self.assertTrue(result['delta']['comparison_eligible'])

    def test_delta_fixture_guidance_does_not_leak_to_apply(self):
        for method in ('direct','tools'):
            self.assertNotIn('fs-fixture-advance',benchmark.prompt('apply',method,True))
            self.assertIn('fs-fixture-advance',benchmark.prompt('delta',method,True))
            self.assertIn('python3',benchmark.prompt('apply',method,True))

    def test_workflow_scenarios_keep_fixture_instruction_task_specific(self):
        for method in ('direct','tools'):
            self.assertNotIn('fs-fixture-advance',workflow_prompt('apply',method))
            self.assertNotIn('fs-fixture-advance',workflow_prompt('inspect',method))
            self.assertIn('fs-fixture-advance',workflow_prompt('delta',method))
        self.assertIn('next_cursor',workflow_prompt('inspect','tools'))
        self.assertIn('--peek TWICE',workflow_prompt('delta','tools'))
        self.assertIn('--save-plan plan.json --apply --report-changes',workflow_prompt('apply','tools'))
        self.assertEqual(workflow_prompt('delta','direct'),workflow_prompt('delta','direct',compact_content=True))
        self.assertIn('--peek --content-kinds modified TWICE',workflow_prompt('delta','tools',compact_content=True))

    def test_completion_workflows_measure_actionable_results_without_forced_pages(self):
        for task in benchmark.TASKS:
            for method in ('direct','tools'):
                text=completion_prompt(task,method)
                self.assertNotIn('512',text)
                self.assertIn('no minimum command count',text)
                self.assertEqual('fs-fixture-advance' in text,task=='delta')
        self.assertIn('--raw --hash',completion_prompt('inspect','tools'))
        self.assertIn('splitlines(keepends=True)',completion_prompt('inspect','tools'))
        self.assertIn('--comparisons 2 ONCE',completion_prompt('delta','tools'))
        self.assertIn('real fresh file scans',completion_prompt('delta','direct'))
        self.assertIn('correct ONLY that count',completion_prompt('apply','direct'))
        self.assertIn('expected_count,actual_count',completion_prompt('apply','tools'))
        self.assertIn('Preserve spec.json',completion_prompt('apply','direct'))
        for method in ('direct','tools'):
            self.assertIn('EXACTLY the supplied version 1 schema',completion_prompt('apply',method))
            self.assertIn('replacement_index is 1-based',completion_prompt('apply',method))

    def test_batched_guidance_keeps_direct_batching_and_safety_checks(self):
        for task in ('delta','apply'):
            for method in ('direct','tools'):
                text=completion_prompt(task,method,batch=True)
                self.assertIn('For EITHER method, batch prerequisite reads',text)
                self.assertIn('No command count is required',text)
                self.assertIn('stop on any unexpected failure',text)
        delta=completion_prompt('delta','tools',batch=True)
        self.assertIn('still perform both real scans',delta)
        apply=completion_prompt('apply','tools',batch=True)
        self.assertIn('Reject all other errors or mismatches',apply)
        self.assertIn('saved_plan.verified:true',apply)

    def test_joint_assessment_does_not_promote_partial_or_cache_conflicting_results(self):
        summary={'comparison_eligible':True}
        for method,values in [('direct',(100,100,10,2)),('tools',(90,90,9,2))]:
            summary[method]={'metrics':{key:{'mean':value} for key,value in zip(('input_plus_output','uncached_plus_output','seconds','command_calls'),values)}}
        self.assertEqual(and_assessment(summary),'표본에서 AND 충족')
        for key in ('input_plus_output','seconds','command_calls'):
            changed=deepcopy(summary);changed['tools']['metrics'][key]['mean']=summary['direct']['metrics'][key]['mean']+1
            self.assertEqual(and_assessment(changed),'부분 개선·AND 미충족')
        changed=deepcopy(summary);changed['tools']['metrics']['uncached_plus_output']['mean']=101
        self.assertEqual(and_assessment(changed),'조건부·캐시 지표 상충')
        changed['comparison_eligible']=False
        self.assertEqual(and_assessment(changed),'검증 부족·판정 보류')

    def test_recount_workflow_authorization_and_direct_batching_remain_explicit(self):
        tools=completion_prompt('apply','tools',batch=True,recount=True)
        direct=completion_prompt('apply','direct',batch=True,recount=True)
        self.assertIn('--recount 1:1',tools)
        self.assertIn('only the FIRST file',tools)
        self.assertIn('Zero matches, unexpected mismatches and source changes are blocked',tools)
        self.assertIn('correct ONLY that count',direct)
        self.assertIn('For EITHER method, batch prerequisite reads',direct)
        self.assertNotIn('--recount',direct)


if __name__=='__main__':unittest.main()
