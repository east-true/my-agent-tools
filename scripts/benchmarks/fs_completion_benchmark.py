"""Measure complete edits, actionable error recovery, and real repeated verification."""
import json
import subprocess
from fs_workflow_benchmark import install as install_workflows


def install(ns,batch=False,recount=False):
    install_workflows(ns,compact_content=True)
    setup_old,verify_old,expected_old=(ns[key] for key in ('setup','verify','expected'))
    save,sha,inventory=(ns[key] for key in ('save','sha','inventory'))

    def setup(root,task):
        setup_old(root,task);work=root/'workspace';control_path=root/'fixture-control/control.json'
        control=json.loads(control_path.read_text());control['completion_workflows']=True
        if task=='inspect':
            a=work/'src/module_00.txt';a.write_bytes(a.read_bytes().replace(b'\n',b'\r\n'))
            b=work/'src/module_05.txt';parts=b.read_bytes().splitlines(keepends=True)
            b.write_bytes(b''.join(line.replace(b'\n',b'\r\n') if i%2 else line for i,line in enumerate(parts)).rstrip(b'\n'))
            query={'version':1,'files':[
                {'path':'src/module_00.txt','ranges':[{'start':40,'end':41},{'start':90,'end':91}]},
                {'path':'src/module_05.txt','ranges':[{'start':60,'end':62}]}]}
            save(work/'queries.json',query);after={}
            for request in query['files']:
                name=request['path'];original=(work/name).read_bytes();chunks=original.splitlines(keepends=True);changes=[]
                for span in request['ranges']:
                    a,b=span['start']-1,span['end'];changes.append((b''.join(chunks[a:b]),b''.join(reversed(chunks[a:b]))))
                edited=original
                for old,new in changes:assert edited.count(old)==1;edited=edited.replace(old,new,1)
                after[name]=sha(edited)
            control['expected_after']=after
        elif task=='apply':
            spec=json.loads((work/'spec.json').read_text());spec['files'][0]['replacements'][0]['count']=2
            save(work/'spec.json',spec)
        control['before']=inventory(work)
        control['before_modes']={str(p.relative_to(work)):p.stat().st_mode&0o777 for p in work.rglob('*') if p.is_file()}
        save(control_path,control);return control['before']

    def expected(task):
        if task=='inspect':return {'modified':['src/module_00.txt','src/module_05.txt'],'verified':['src/module_00.txt','src/module_05.txt'],'blocks_reversed':True,'unrelated_preserved':True}
        result=expected_old(task)
        if task=='apply':result['diagnostic']={'path':'src/module_00.txt','replacement_index':1,'expected_count':2,'actual_count':1}
        return result

    def verify(root,task,before):
        work=root/'workspace';control=json.loads((root/'fixture-control/control.json').read_text())
        if task=='inspect':
            after=inventory(work);targets=control['expected_after']
            checks={'exact_edits_and_original_terminators':all(after.get(name)==digest for name,digest in targets.items()),
                    'unrelated_bytes_preserved':all(after.get(name)==digest for name,digest in before.items() if name not in targets),
                    'existing_permissions_preserved':all((work/name).stat().st_mode&0o777==mode for name,mode in control['before_modes'].items()),
                    'no_internal_temporaries':not any(p.name.startswith('.tools-fs-') for p in work.rglob('*'))}
            return {'correct':all(checks.values()),'checks':checks}
        result=verify_old(root,task,before)
        if task=='apply':
            plan=json.loads((work/'spec.json').read_text());plan['files'][0]['replacements'][0]['count']=1
            for item in plan['files']:
                if item.get('sha256')!='absent':item['sha256']=before[item['path']]
            try:result['checks']['saved_plan_has_exact_original_hashes']=json.loads((work/'plan.json').read_text())==plan
            except (OSError,ValueError):result['checks']['saved_plan_has_exact_original_hashes']=False
            result['correct']=all(result['checks'].values())
        return result

    def preflight(root,tasks=None):
        results=[]
        for task in ns['TASKS'] if tasks is None else tasks:
            before=setup(root,task);work=root/'workspace';directory=root/'preflight'/task;directory.mkdir(parents=True)
            def call(args,body=None,success=True):
                run=subprocess.run(['codex','sandbox','--permission-profile','filesystem_benchmark',*ns['permissions'](root),'--cd',str(work),'--',str(root/'bin/tools'),*args],cwd=work,env=ns['environment'](root),input=body,capture_output=True,text=True,encoding='utf-8')
                assert (run.returncode==0)==success,(task,run.stdout,run.stderr)
                value=json.loads(run.stdout)
                if success:assert value['complete']
                return value
            if task=='inspect':
                read=call(['fs','inspect','--request','queries.json','--raw','--hash','--json'])
                assert not read.get('next_cursor')
                plan={'version':1,'files':[]}
                for file in read['files']:
                    replacements=[]
                    for span in file['ranges']:
                        old=span['text'];new=''.join(reversed(old.splitlines(keepends=True)))
                        replacements.append({'old':old,'new':new,'count':1})
                    plan['files'].append({'path':file['path'],'sha256':file['sha256'],'replacements':replacements})
                value=call(['fs','apply','--plan','-','--apply','--report-changes','--json'],json.dumps(plan))
                actual=expected(task);actual['modified']=sorted(f['path'] for f in value['files'] if f['status']=='applied');actual['verified']=sorted(f['path'] for f in value['files'] if f.get('verified'))
            elif task=='delta':
                args=['fs','delta','--include','src/**/*.txt','--state-file','baseline.json','--include-content','--json']
                call(args);ns['advance'](root)
                value=call(args+['--peek','--content-kinds','modified','--comparisons','2'])
                assert value['comparisons']==2 and value['consistent'] and value['baseline_preserved'] and not value.get('other_results')
                actual=expected(task)
                for key,kind in [('added','added'),('modified','modified'),('deleted','deleted')]:actual[key]=[c['path'] for c in value['changes'] if c['kind']==kind]
                actual['unchanged']=value['unchanged'];actual['repeat_consistent']=value['consistent'];actual['baseline_preserved']=value['baseline_preserved']
                actual['changed_lines']=[{'path':c['path'],'line':s['start']+i,'text':line} for c in value['changes'] if c['kind']=='modified' for s in c.get('ranges',[]) for i,line in enumerate(s['lines'])]
            else:
                spec=json.loads((work/'spec.json').read_text())
                if recount:
                    value=call(['fs','apply','--spec','spec.json','--recount','1:1','--save-plan','plan.json','--apply','--report-changes','--json'])
                    assert len(value['corrections'])==1
                    error={'diagnostic':value['corrections'][0]}
                else:
                    error=call(['fs','apply','--spec','spec.json','--apply','--json'],success=False)
                    assert inventory(work)==before
                    spec['files'][0]['replacements'][0]['count']=1
                    value=call(['fs','apply','--spec','-','--save-plan','plan.json','--apply','--report-changes','--json'],json.dumps(spec))
                if batch:
                    proof=value['saved_plan']
                    assert proof['verified'] and proof['version']==1 and proof['files']==len(spec['files']) and proof['sha256']==sha((work/'plan.json').read_bytes())
                actual=ns['normalize'](task,value,work);actual['verified']=sorted(f['path'] for f in value['files'] if f.get('verified'));actual['plan_saved']=(work/'plan.json').exists()
                actual['diagnostic']={key:error['diagnostic'][key] for key in ('path','replacement_index','expected_count','actual_count')}
            verification=verify(root,task,before);assert actual==expected(task) and verification['correct'],(task,actual,verification)
            save(directory/'native.json',{'response':value,'actual':actual,'verification':verification});results.append({'task':task,'correct':True,'model_calls':0})
            print(json.dumps({'event':'preflight_passed','task':task}),flush=True)
        save(root/'preflight.json',results)

    ns.update(setup=setup,verify=verify,expected=expected,preflight=preflight,prompt=lambda task,method,efficient=False:completion_prompt(task,method,efficient,batch=batch,recount=recount))


def completion_prompt(task,method,efficient=False,batch=False,recount=False):
    shared='Complete the specified filesystem work, including exact final edits and verification, in this synthetic workspace. Preserve unrelated bytes, existing native permissions, provided JSON, each original line terminator and final newline state. Use / paths and 1-based lines. Installed Python is python3. No network, delegation, model calls, installs, or reading benchmark source/protocol/control/results/other sessions. Batch/filter naturally; no minimum command count or artificial output/page limit. Return only the requested answer schema. Work including edits is authorized. Do not dump whole source files.\n'
    tasks={
        'inspect':'Read queries.json. Each specified range selects a complete block of original lines. Reverse the order of lines in EVERY selected block in each file, moving each line with its original terminator. Preserve all other bytes, line endings and final newline state. Capture original hashes, validate exact unique original fragments before edits, apply and verify final bytes/permissions. Return sorted modified/verified paths and block-reversal/unrelated-preservation facts. This is an actual edit, not merely a line-list answer. Use the normal output budget; pagination is tested separately.\n',
        'delta':'Create baseline.json containing original SHA-256 and original text for EVERY src/**/*.txt file. Invoke fs-fixture-advance for exactly one successful mutation; rejected pre-mutation calls may be retried. Compare against the baseline TWICE with real fresh file scans, without consuming or changing it. Hash contents even if size/mtime match. Return sorted added/modified/deleted, unchanged count, every differing current line in the modified file, repeat consistency and baseline-byte preservation. Renames are deletion+addition. Only modified text is required. Return one complete result with real verification evidence; do not print two identical full reports.\n',
        'apply':'Read spec.json. The requested change is cache_limit 16 to 64 and retry_budget 3 to 5 in both selected existing files, plus the explicit new JSON. The supplied first replacement intentionally has a wrong exact count. Validate the supplied spec without mutations, capture path/replacement_index/expected_count/actual_count, and correct ONLY that count to the observed count on a copy. Preserve spec.json. Generate/save a validated SHA plan as plan.json without overwrite, apply, and read back intended bytes/permissions. Return diagnostic, sorted applied/verified paths, resulting numeric settings from both edited files/new JSON, plan_saved and unrelated-file preservation. All other path/no-symlink/UTF-8/count/absence checks remain required.\n'}
    guides={
        'inspect':'Use tools fs inspect --request queries.json --raw --hash --json once. ranges contains start and exact text (including CRLF/mixed terminators), not lines; line_ending/final_newline describe the file. Build {version:1,files:[{path,sha256,replacements:[{old:original_text,new:reversed_text,count:1},...]}]} from each original sha256 and raw text with reversed splitlines(keepends=True). Apply using tools fs apply --plan FILE_OR_STDIN --apply --report-changes --json; verified true reflects read-back. Python may construct/pass the plan from inspection data; do not reread source files just to recover line endings or reimplement reads/writes.\n',
        'delta':"Initialize using tools fs delta --include 'src/**/*.txt' --state-file baseline.json --include-content --json; then fs-fixture-advance. Invoke the same delta with --peek --content-kinds modified --comparisons 2 ONCE: it performs two fresh scans, verifies baseline bytes and returns comparisons,consistent,baseline_preserved plus one complete changes report. Different observations cause unstable/complete:false with other_results, so require complete:true here. Normal mapping of fields is allowed; no external comparison loop is needed.\n",
        'apply':'Use tools fs apply --spec spec.json --apply --json to capture the error without source writes. diagnostic includes path,replacement_index,expected_count,actual_count. Correct the indicated count in a copied spec using that result, then tools fs apply --spec FILE_OR_STDIN --save-plan plan.json --apply --report-changes --json. Use verified and changed ranges for final values; report_complete must be true. No extra source query or separate preview is required.\n'}
    if task=='apply':
        tasks[task]+='replacement_index is 1-based. The saved plan must have EXACTLY the supplied version 1 schema: {version:1,files:[...]}. Keep the corrected replacements/content and all other fields, adding sha256 of the original bytes to each existing-file entry; keep sha256:"absent" for creation. Do not substitute a different saved-plan format.\n'
    if batch:
        shared+='For EITHER method, batch prerequisite reads, processing, dependent actions and final verification in a local script where practical. Intermediate output need not be sent to the model when the next authorized action is fully determined by the task and verified data. Emit a complete usable final result, keep error evidence, and stop on any unexpected failure. No command count is required.\n'
        if task=='delta':
            guides[task]+='A Python subprocess script can initialize the baseline, check complete:true, invoke fs-fixture-advance, perform the repeated peek and map the final JSON without returning to the model between steps. Keep only the final usable result on stdout; still perform both real scans and preserve baseline bytes.\n'
        if task=='apply':
            guides[task]+='A Python subprocess script can read the provided spec, capture the first validation diagnostic and correct the authorized first replacement in memory. Require diagnostic path/index/expected_count to match that first input replacement; actual_count must be positive. Reject all other errors or mismatches. Then invoke the corrected spec with --save-plan --apply --report-changes. The response saved_plan has version,files,sha256,verified from actual saved bytes, checked before writes and after apply. Require saved_plan.verified:true and all files verified plus report_complete:true; no separate plan.json read/help call is needed to establish this proof. Keep the input spec unchanged and retain the diagnostic in the final answer.\n'
    if recount and task=='apply':
        guides[task]='Use tools fs apply --spec spec.json --recount 1:1 --save-plan plan.json --apply --report-changes --json. The user explicitly authorized correcting only the FIRST file\'s FIRST replacement count to the observed positive count: this flag selects exactly that entry. The CLI reads the spec, captures original hashes, diagnoses the supplied count before writes, adjusts only that count on a copy, validates every other condition unchanged, saves the plan and verifies final bytes/permissions plus saved plan bytes. Zero matches, unexpected mismatches and source changes are blocked. Corrections contains the original path/replacement_index/expected_count/actual_count diagnostic. Require complete,report_complete,saved_plan.verified and each file verified. Return the diagnostic from corrections and settings from verified ranges. This CLI already reads spec.json; no separate input/plan read or Python repair is needed.\n'
    return shared+tasks[task]+('Method: production tools fs workflow; Python may orchestrate/map/construct input JSON but must not reimplement filesystem selection, validation or edits.\n'+guides[task] if method=='tools' else 'Method: direct shell/rg/Python standard library; never invoke tools or import its implementation. Scripts may batch all operations and validation. Authoring, processing, failures and verification costs are included.\n')
