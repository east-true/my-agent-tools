"""완료 비교의 상충 판정과 독립 근거 정정을 검증한다."""
import copy
import json
import tempfile
import unittest
from pathlib import Path

import publish_completion_benchmark as publication


class CompletionAssessmentTest(unittest.TestCase):
    def summary(self):
        return {'comparison_eligible':True,'direct':{'metrics':{key:{'mean':value} for key,value in {'input_plus_output':100,'uncached_plus_output':50,'seconds':20,'command_calls':2,'api_calls':2}.items()}},'tools':{'metrics':{key:{'mean':value} for key,value in {'input_plus_output':90,'uncached_plus_output':40,'seconds':10,'command_calls':1,'api_calls':2}.items()}}}

    def test_failure_cache_conflict_and_api_increase_are_not_and(self):
        value=self.summary()
        self.assertIn('AND 충족',publication.assessment(value))
        value['tools']['metrics']['api_calls']['mean']=3
        self.assertIn('호출 부담 증가',publication.assessment(value))
        value['tools']['metrics']['uncached_plus_output']['mean']=60
        self.assertIn('상충',publication.assessment(value))
        value['comparison_eligible']=False
        self.assertIn('판정 보류',publication.assessment(value))

    def test_native_adjudication_keeps_original_failure_and_rejects_wrong_body(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            (root/'harness').mkdir()
            (root/'harness/run_completion_benchmark.py').write_text("expected={'review_decision':'CHANGES_REQUESTED'}",encoding='utf-8')
            path=root/'preflight/review-return/1/result.json'
            path.parent.mkdir(parents=True)
            review={'id':1,'state':'CHANGES_REQUESTED','body':'exact\r\nrequest','commit_id':'old'}
            response={'complete':True,'outstanding':{'pr':{'head_sha':'current','review_decision':'APPROVED'},'reviews':[review]}}
            path.write_text(json.dumps({'response':response}),encoding='utf-8')
            row={'task':'review-return','index':1,'method':'tools','repetition':1,'correct':False,'exit_code':0,'usage':{'input_tokens':1},'state_correct':True,'actual':{'body':review['body'],'review_id':1,'state':'CHANGES_REQUESTED','commit':'old','head':'current','review_decision':'APPROVED'}}
            report={'trials':[row]}
            original=copy.deepcopy(report)
            corrections=publication.adjudicate_review_return(root,report)
            self.assertEqual(original,report)
            self.assertFalse(corrections[0]['original_correct'])
            self.assertTrue(corrections[0]['corrected_correct'])
            row['actual']['body']='wrong'
            self.assertFalse(publication.adjudicate_review_return(root,report)[0]['corrected_correct'])


if __name__=='__main__':
    unittest.main()
