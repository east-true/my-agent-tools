#!/usr/bin/env python3
"""출력 복구·원문·설정 검증 변경만 같은 완료 작업으로 비교한다."""
import hashlib
import json
import shutil
from pathlib import Path

import run_usage_benchmark as usage
import run_actionable_benchmark as action
import run_command_benchmark as core

TASKS = ['review-source', 'review-return', 'junit-recovery', 'delta-recovery', 'apply-report', 'setup-proof', 'ci-evidence']
REFERENCES = {}
ORIGINAL_SETUP = usage.setup


def sha(data):
    return hashlib.sha256(data).hexdigest()


class Handler(usage.Handler):
    def respond(self):
        from urllib.parse import urlsplit
        path = urlsplit(self.path).path
        if path in ('/user', f'/repos/{core.REPO}', f'/repos/{core.REPO}/labels', f'/repos/{core.REPO}/issue-types'):
            return core.Handler.respond(self)
        return super().respond()


def native(root, server, directory, task, args):
    return usage.native_call(root, server, directory, task, args)


def setup(root, server, task):
    usage.REPETITION = int(usage.REPETITION)
    ORIGINAL_SETUP(root, server, 'pr-conversation')
    work = root / 'workspace'
    private = work / '.tools'
    private.mkdir(exist_ok=True)
    rep = usage.REPETITION
    source = ('# preserve exact\r\n' * (100+rep) + 'retry_budget=2\r\ntimeout=30\r\n').encode()
    wanted = source.replace(b'retry_budget=2', b'retry_budget=9', 1)
    if task == 'review-source':
        (work / 'settings.py').write_bytes(source)
        (work / 'settings.py').chmod(0o640)
        REFERENCES[task] = {'before_sha256':sha(source),'after_sha256':sha(wanted),'retry_budget':9,'timeout':30,'verified':True,'unrelated_preserved':True}
    elif task == 'review-return':
        server.action_threads = []
        server.history[0]['body'] = f'Only submitted review: preserve timeout=30; consider retry budget {9+rep}.\r\nThis is a historical request, not an inferred current obligation.'
        server.graphql = usage.GraphQL(server)
        directory = root / 'seed' / task / str(rep)
        directory.mkdir(parents=True, exist_ok=True)
        value = native(root, server, directory, task, ['github','pr','inspect','--repo',core.REPO,'--number','7','--sections','reviews','--state-file','.tools/pr-state.json','--json'])
        assert value['complete']
        review = server.history[0]
        REFERENCES[task] = {'body':review['body'],'review_id':review['id'],'state':review['state'],'commit':review['commit_id'],'head':server.head,'review_decision':value['pr']['review_decision']}
    elif task == 'junit-recovery':
        detail = 'unrelated diagnostic\n'*5000 + f'target diagnosis: observed value {rep}; expected 9\n'
        xml = f'<testsuite name="suite" tests="1" failures="1"><testcase name="case"><failure message="failed">{detail}</failure></testcase></testsuite>'.encode()
        (work / 'TEST-case.xml').write_bytes(xml)
        REFERENCES[task] = {'tests':1,'failures':1,'report_sha256':sha(xml),'diagnosis':detail.splitlines()[-1],'diagnosis_line':5001,'execution_verified':False}
    elif task == 'delta-recovery':
        detail = ('old content\r\n'*8000 + f'deleted target {rep}\r\n').encode()
        (work / 'deleted.txt').write_bytes(detail)
        directory = root / 'seed' / task / str(rep)
        directory.mkdir(parents=True, exist_ok=True)
        value = native(root, server, directory, task, ['fs','delta','--state-file','.tools/baseline.json','--include-content','--path','deleted.txt','--json'])
        assert value['complete']
        (work / 'deleted.txt').unlink()
        REFERENCES[task] = {'path':'deleted.txt','kind':'deleted','before_sha256':sha(detail),'diagnosis':f'deleted target {rep}\r\n','diagnosis_line':8001,'baseline_preserved':True}
    elif task == 'apply-report':
        new = 'new content\n'*8000 + f'target final edit {rep}\n'
        original = (work / 'settings.py').read_bytes()
        plan = {'version':1,'files':[{'path':'settings.py','sha256':sha(original),'content':new}]}
        (private / 'edit-plan.json').write_text(json.dumps(plan),encoding='utf-8')
        REFERENCES[task] = {'applied':1,'verified':True,'after_sha256':sha(new.encode()),'diagnosis':new.splitlines()[-1],'diagnosis_line':8001,'mode_preserved':True}
    elif task == 'setup-proof':
        original = {'github':{'body_language':'ko','label_map':core.LABEL_MAP,'issue_type_map':core.TYPE_MAP}}
        # 기존 환경 설정과 권한을 유지하며 내용이 실제 저장되는 조건이다.
        (work / '.tools.json').write_text(json.dumps(original),encoding='utf-8')
        (work / '.tools.json').chmod(0o640)
        ordered = {"github":{"body_language":"ko","label_map":dict(sorted(core.LABEL_MAP.items())),"issue_type_map":dict(sorted(core.TYPE_MAP.items()))}}
        expected = (json.dumps(ordered,indent=2)+'\n').encode()
        REFERENCES[task] = {'saved_sha256':sha(expected),'body_language':'ko','fix_label':'bug','fix_type':'Bug','mode':'0640','changed':True,'verified':True}
    elif task == 'ci-evidence':
        lines = [f'plain log {i+1}' for i in range(100)]
        lines[70] = f'hidden decision parameter=wrong-{rep}'
        evidence = {'repo':core.REPO,'run':{'id':42,'run_attempt':2,'head_sha':server.head},'complete':False,'evidence':[{'kind':'log','truncated':True,'lines':lines,'occurrences':[{'job_id':11,'start_line':11,'end_line':110}]}]}
        (private / 'ci-evidence.json').write_text(json.dumps({'inspection':{'failures':[evidence]}}),encoding='utf-8')
        REFERENCES[task] = {'run':42,'attempt':2,'job':11,'line':81,'text':lines[70],'collection_complete':False,'collection_truncated':True,'fresh_state_verified':False}
    else:
        raise ValueError(task)
    server.accesses = []
    return {"inventory":action.inventory(work),"protected":{str(p.relative_to(work)):sha(p.read_bytes()) for p in private.rglob("*") if p.is_file()}}, {}


def expected(task):
    return REFERENCES[task]


def verify(root, server, task, before):
    work = root / 'workspace'
    after = action.inventory(work)
    protected = before['protected']
    before = before['inventory']
    mutable = {'settings.py'} if task in ('review-source','apply-report') else {'.tools.json'} if task == 'setup-proof' else set()
    checks = {'original_files_preserved':all(after.get(p)==v for p,v in before.items() if p not in mutable), 'no_api_mutations':not any(a['method']!='GET' and a['endpoint']!='/graphql' for a in server.accesses)}
    checks['supplied_artifacts_preserved'] = all((work/p).is_file() and sha((work/p).read_bytes())==v for p,v in protected.items())
    if task in ('review-source','apply-report'):
        current = (work/'settings.py').read_bytes()
        checks['final_file_sha_and_mode'] = sha(current)==expected(task)['after_sha256'] and after['settings.py']['mode']==before['settings.py']['mode']
    if task == 'setup-proof':
        checks['actual_saved_bytes_and_mode'] = sha((work/'.tools.json').read_bytes())==expected(task)['saved_sha256'] and (work/'.tools.json').stat().st_mode & 0o777 == 0o640
    if task == 'ci-evidence':
        checks['offline_no_remote_calls'] = len(server.accesses)==0
    if task.startswith('review-'):
        checks['fresh_head_collected'] = any(a['endpoint'].split('?')[0].endswith('/pulls/7') or 'headRefOid' in a.get('payload',{}).get('query','') for a in server.accesses)
    return {'correct':all(checks.values()),'checks':checks}


def prompt(task, method):
    shared = core.SHARED.replace('Read body.md for creation tasks.','')
    shared += f'\nRepo: {core.REPO}. PR: 7. Same final answer and file/permission validation are required for either method. Inherited model defaults; independent use, no assumed previous model context.\n'
    jobs = {
        'review-source':'Read submitted and inline reviews (all fit one page). The reviewed path is not supplied; derive paths from unresolved threads. Apply the actual inline request, preserving every unrelated byte, CRLF and permissions. Verify the final file and a fresh PR head after editing. Return before/after SHA, retry_budget, timeout, verified and unrelated_preserved.',
        'review-return':'Returning after unrelated work: read the currently selected submitted review history and fresh PR head/aggregate decision. There are no inline threads. Existing .tools/pr-state.json is prior observation supplied as task input; do not require previous model context or treat historical reviews as current obligations. Return exact body, review_id, state, commit, head and review_decision. Do not edit files.',
        'junit-recovery':'Read TEST-case.xml counts and locate the diagnostic line containing "target diagnosis". Return exact line without newline, original 1-based diagnostic line number, original XML report SHA, tests, failures and execution_verified=false. Report reading does not prove test process completion. Preserve the XML.',
        'delta-recovery':'Compare supplied .tools/baseline.json against selected deleted.txt, which is now absent. Recover the old diagnostic line containing "deleted target" including exact CRLF and original 1-based line, and before SHA/kind/path. Keep the baseline bytes unchanged and verify this. Return baseline_preserved. The baseline contains original text; do not expect the deleted file on disk.',
        'apply-report':'Apply the authorized exact preimage-bound .tools/edit-plan.json once. Read back final bytes and preserve permissions. Recover the changed line containing "target final edit" and its original after-file line number, without emitting all 8000 unrelated lines. Return applied, verified, after_sha256, diagnosis without newline, diagnosis_line and mode_preserved. Do not repeat a completed apply for its report.',
        'setup-proof':'Read the existing GitHub label/type catalog, preserve all existing .tools.json preferences and mode, save the standard two-space JSON format (github fields body_language, label_map, issue_type_map in that order; sort keys inside each map) with final newline, and verify actual saved bytes/settings and permissions. Return saved_sha256, body_language, fix_label, fix_type, mode, changed, verified. No remote catalog mutations.',
        'ci-evidence':'Read supplied .tools/ci-evidence.json, an existing observation of run42 attempt2 job11, and return the line containing "hidden decision" with original job-log coordinate, run, attempt, job, exact text, collection_complete, collection_truncated, fresh_state_verified=false. Verify artifact SHA before use. Do not authenticate, collect, rerun or claim fresh CI state.'}
    shared += jobs[task]+'\n'
    tools = {
        'review-source':f'tools github pr reviews --repo {core.REPO} --number 7 --review-sources --compact --json. source_files contains exact local source bytes/SHA. Use local scripts to make the requested edit and verify bytes/mode; gh may be used solely for the final fresh head.',
        'review-return':f'tools github pr inspect --repo {core.REPO} --number 7 --sections reviews --state-file .tools/pr-state.json --json. outstanding.reviews contains raw submitted history even when unchanged.',
        'junit-recovery':'tools fs test-results --path TEST-case.xml --diagnostic-pattern "target diagnosis" --json returns all counts/XML SHA and exact selected diagnostic details_ranges with original positions in one response.',
        'delta-recovery':'tools fs delta --state-file .tools/baseline.json --path deleted.txt --include-content --report-pattern "deleted target" --peek --json returns change metadata and before_raw_ranges (exact CRLF and original positions) in the first response. baseline_preserved and baseline_sha256 come from actual baseline byte read-back; use that proof unless it failed.',
        'apply-report':'tools fs apply --plan .tools/edit-plan.json --apply --report-changes --report-pattern "target final edit" --json returns exact selected changed lines/positions and a receipt with verified after SHA, before_mode, mode and mode_preserved from actual read-back in one response. The plan is authorized supplied input and the CLI validates its preimage/content; no separate preview or whole-plan printing is required. Use the receipt proof for permissions and bytes unless verification failed.',
        'setup-proof':f'tools github setup --repo {core.REPO} --json. saved_config describes actual read-back SHA, verified, changed and original mode preservation; plan.config contains saved settings.',
        'ci-evidence':'Compute SHA of supplied artifact locally, then tools github ci failures --read-evidence .tools/ci-evidence.json --evidence-sha256 SHA --evidence-run 42 --evidence-attempt 2 --job 11 --pattern "hidden decision" --json. No additional remote calls needed.'}
    direct = {
        'review-source':f'Use gh GraphQL to collect reviewThreads with comments body/path/diffHunk, submitted reviews state/body/commit/author, headRefOid/reviewDecision, then read mapped local source files and edit/verify naturally. Recheck head via gh api repos/{core.REPO}/pulls/7. Batching and Python are allowed.',
        'review-return':f'Use one naturally batched gh GraphQL query for headRefOid, reviewDecision and reviews(first:100){{nodes{{fullDatabaseId state body commit{{oid}}}}pageInfo{{hasNextPage}}}}. Existing state need not be replayed to get current history.',
        'junit-recovery':'Use Python XML parsing/hash or natural direct file tools. Select the requested diagnostic while keeping aggregate counts, original decoded line positions and XML SHA.',
        'delta-recovery':'Use Python to read the supplied baseline JSON (files is a path-keyed map with sha256 and text), compare selected path existence/SHA and extract the old raw line. Verify baseline bytes remain unchanged in the same script if useful.',
        'apply-report':'Use Python to read the authorized plan, validate exact preimage SHA, write requested content, preserve mode, read back final bytes and extract the requested changed line. One script may do all required validation.',
        'setup-proof':f'Use gh api repos/{core.REPO}/labels and gh api repos/{core.REPO}/issue-types plus repo metadata if needed. Existing mappings already refer to valid catalog values. Python may save and verify actual JSON/settings/SHA/mode in the specified order in one script.',
        'ci-evidence':'Use Python/local direct tools to hash/decode the supplied evidence JSON and select matching run/attempt/job occurrence and raw line. Natural batching/filters allowed.'}
    shared += ('Use the production tools for the collection/operation; do not substitute direct processing. '+tools[task]) if method=='tools' else ('Use direct processing, never tools. '+direct[task])
    return shared+'\n'


def preflight(root, server):
    checks=[]
    for rep in range(1,4):
        usage.REPETITION=rep
        for task in usage.TASKS:
            before,_=setup(root,server,task)
            directory=root/'preflight'/task/str(rep)
            directory.mkdir(parents=True)
            args = {
                'review-source':['github','pr','reviews','--repo',core.REPO,'--number','7','--review-sources','--compact','--json'],
                'review-return':['github','pr','inspect','--repo',core.REPO,'--number','7','--sections','reviews','--state-file','.tools/pr-state.json','--json'],
                'junit-recovery':['fs','test-results','--path','TEST-case.xml','--diagnostic-pattern','target diagnosis','--json'],
                'delta-recovery':['fs','delta','--state-file','.tools/baseline.json','--path','deleted.txt','--include-content','--report-pattern','deleted target','--peek','--json'],
                'apply-report':['fs','apply','--plan','.tools/edit-plan.json','--apply','--report-changes','--report-pattern','target final edit','--json'],
                'setup-proof':['github','setup','--repo',core.REPO,'--json'],
                'ci-evidence':['github','ci','failures','--read-evidence','.tools/ci-evidence.json','--evidence-sha256',sha((root/'workspace/.tools/ci-evidence.json').read_bytes()) if task=='ci-evidence' else '','--evidence-run','42','--evidence-attempt','2','--job','11','--pattern','hidden decision','--json']
            }[task]
            value=native(root,server,directory,task,args)
            if task in ('junit-recovery','delta-recovery','apply-report'):
                assert value['complete'] and 'saved_report' not in value
                if task == 'junit-recovery':
                    d=value['reports'][0]['diagnostics'][0]['details_ranges'][0]
                    assert d['start']==expected(task)['diagnosis_line'] and d['text'].rstrip('\n')==expected(task)['diagnosis']
                    assert value['reports'][0]['sha256']==expected(task)['report_sha256']
                elif task == 'delta-recovery':
                    d=value['changes'][0]['before_raw_ranges'][0]
                    assert d['start']==expected(task)['diagnosis_line'] and d['text']==expected(task)['diagnosis']
                    assert value['baseline_preserved'] and value['baseline_sha256']==sha((root/'workspace/.tools/baseline.json').read_bytes())
                else:
                    f=value['files'][0]
                    d=f['ranges'][0]
                    assert f['verified'] and f['mode_preserved'] and f['mode']==f['before_mode']
                    assert d['start']==expected(task)['diagnosis_line'] and d['lines'][0]==expected(task)['diagnosis']
            elif task=='review-source':
                assert value['source_files']['files'][0]['ranges'][0]['text'].encode()==(root/'workspace/settings.py').read_bytes()
                data=(root/'workspace/settings.py').read_bytes().replace(b'retry_budget=2',b'retry_budget=9',1)
                (root/'workspace/settings.py').write_bytes(data)
            elif task=='review-return':
                assert value['status']=='unchanged' and value['outstanding']['reviews'][0]['body']==expected(task)['body']
            elif task=='setup-proof':
                assert value['saved_config']['verified'] and value['saved_config']['sha256']==expected(task)['saved_sha256']
            elif task=='ci-evidence':
                assert value['segments'][0]['ranges'][0]['start_line']==81
            state=verify(root,server,task,before)
            assert state['correct'],(task,state)
            core.save(directory/'result.json',{'response':value,'state':state})
            checks.append({'task':task,'repetition':rep,'correct':True,'model_calls':0})
            print(json.dumps({'event':'preflight_passed','task':task,'repetition':rep}),flush=True)
    return checks


def main():
    usage.TASKS=TASKS
    usage.setup,usage.expected,usage.verify,usage.prompt,usage.preflight=(setup,expected,verify,prompt,preflight)
    usage.Handler=Handler
    usage.main()


if __name__=='__main__':
    main()
