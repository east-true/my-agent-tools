"""Fixed scenarios for batch ranges/pagination, sparse peek, and spec/apply/read-back."""
import json
import subprocess


def long_lines(changed=False):
    lines=[f'long module neutral line {n:04d}' for n in range(1,1001)]
    lines[9]='limit=64' if changed else 'limit=08'
    lines[989]='budget=05' if changed else 'budget=03'
    return lines


def install(ns,compact_content=False):
    original_setup,original_expected,original_verify,original_advance=(ns[name] for name in ('setup','expected','verify','advance'))
    save,sha,inventory,permissions,environment=(ns[name] for name in ('save','sha','inventory','permissions','environment'))

    def setup(root,task):
        original_setup(root,task)
        work=root/'workspace';control_path=root/'fixture-control/control.json'
        control=json.loads(control_path.read_text())
        control['workflows']=True
        if task=='inspect':
            save(work/'queries.json',{'version':1,'files':[
                {'path':'src/module_00.txt','ranges':[{'start':40,'end':40},{'start':90,'end':90}]},
                {'path':'src/module_05.txt','ranges':[{'start':58,'end':63}]},
                {'path':'src/module_20.txt','patterns':['cache_limit','retry_budget'],'context':1}]})
        elif task=='delta':
            (work/'src/module_10.txt').write_text('\n'.join(long_lines())+'\n',encoding='utf-8')
        else:
            spec=json.loads((work/'edits.json').read_text())
            for item in spec['files']:
                if item['sha256']!='absent':item.pop('sha256')
            save(work/'spec.json',spec);(work/'edits.json').unlink()
        control['before']=inventory(work)
        control['before_modes']={str(p.relative_to(work)):p.stat().st_mode&0o777 for p in work.rglob('*') if p.is_file()}
        save(control_path,control)
        return control['before']

    def advance(root):
        original_advance(root)
        work=root/'workspace';path=work/'src/module_10.txt';info=path.stat()
        path.write_bytes(path.read_bytes().replace(b'budget=03',b'budget=05'))
        ns['os'].utime(path,ns=(info.st_atime_ns,info.st_mtime_ns))
        control_path=root/'fixture-control/control.json';control=json.loads(control_path.read_text())
        control['baseline_sha256']=sha((work/'baseline.json').read_bytes());save(control_path,control)

    def expected(task):
        if task=='inspect':
            rows=[]
            for index,numbers in [(0,[40,90]),(5,list(range(58,64))),(20,[39,40,41,89,90,91])]:
                for n in numbers:
                    text='cache_limit=16' if n==40 else 'retry_budget=3' if n==90 else 'limit=08' if n==61 else f'module {index:02d} neutral line {n:03d}'
                    rows.append({'path':f'src/module_{index:02d}.txt','line':n,'text':text})
            return {'lines':rows}
        result=original_expected(task)
        if task=='delta':
            result.pop('changed_line');result['changed_lines']=[{'path':'src/module_10.txt','line':10,'text':'limit=64'},{'path':'src/module_10.txt','line':990,'text':'budget=05'}]
            result['repeat_consistent']=True;result['baseline_preserved']=True
        else:
            result['verified']=['config/limits.json','src/module_00.txt','src/module_05.txt'];result['plan_saved']=True
        return result

    def verify(root,task,before):
        work=root/'workspace';control=json.loads((root/'fixture-control/control.json').read_text())
        # Original verifier expects edits.json for apply; keep it isolated from the workspace.
        if task=='apply':
            (work/'edits.json').write_bytes((work/'spec.json').read_bytes())
        try:result=original_verify(root,task,before)
        finally:
            if task=='apply':(work/'edits.json').unlink()
        checks=result['checks']
        if task=='delta':
            checks['modified_exactly']=sha((work/'src/module_10.txt').read_bytes())==sha(('\n'.join(long_lines(True))+'\n').encode())
            checks['baseline_not_consumed']=sha((work/'baseline.json').read_bytes())==control['baseline_sha256']
        if task=='apply':
            plan=json.loads(json.dumps(json.loads((work/'spec.json').read_text())))
            for item in plan['files']:
                if item.get('sha256')!='absent':item['sha256']=before[item['path']]
            try:checks['saved_plan_has_exact_original_hashes']=json.loads((work/'plan.json').read_text())==plan
            except (OSError,ValueError):checks['saved_plan_has_exact_original_hashes']=False
        result['correct']=all(checks.values())
        return result

    def preflight(root,tasks=None):
        results=[]
        for task in ns['TASKS'] if tasks is None else tasks:
            before=setup(root,task);directory=root/'preflight'/task;directory.mkdir(parents=True)
            def call(args):
                r=subprocess.run(['codex','sandbox','--permission-profile','filesystem_benchmark',*permissions(root),'--cd',str(root/'workspace'),'--',str(root/'bin/tools'),*args],cwd=root/'workspace',env=environment(root),capture_output=True,text=True,encoding='utf-8',check=True)
                value=json.loads(r.stdout);assert value['complete'] or value['status']=='page';return value
            if task=='inspect':
                args=['fs','inspect','--request','queries.json','--max-output-bytes','512','--json']
                page=call(args);pages=[page]
                while page.get('next_cursor'):page=call(args+['--cursor',page['next_cursor']]);pages.append(page)
                assert len(pages)>1
                actual={'lines':[{'path':f['path'],'line':r['start']+i,'text':line} for p in pages for f in p['files'] for r in f.get('ranges',[]) for i,line in enumerate(r['lines'])]}
                value=pages
            elif task=='delta':
                args=['fs','delta','--include','src/**/*.txt','--state-file','baseline.json','--include-content','--json']
                first=call(args);assert first['status']=='initialized';advance(root)
                baseline=(root/'workspace/baseline.json').read_bytes()
                view=['--content-kinds','modified'] if compact_content else []
                value=call(args+['--peek',*view]);assert value==call(args+['--peek',*view]) and not value['state_updated']
                actual=expected(task)
                actual['added']=[c['path'] for c in value['changes'] if c['kind']=='added']
                actual['modified']=[c['path'] for c in value['changes'] if c['kind']=='modified']
                actual['deleted']=[c['path'] for c in value['changes'] if c['kind']=='deleted']
                actual['unchanged']=value['unchanged']
                actual['changed_lines']=[{'path':c['path'],'line':r['start']+i,'text':line} for c in value['changes'] if c['kind']=='modified' for r in c.get('ranges',[]) for i,line in enumerate(r['lines'])]
                legacy=root/'legacy-tools'
                if legacy.exists():
                    old=subprocess.run([str(legacy),*args,*(['--peek'] if compact_content else []),'--max-output-bytes','1048576'],cwd=root/'workspace',capture_output=True,text=True,encoding='utf-8',check=True)
                    old_value=json.loads(old.stdout)
                    assert [(c['path'],c['kind']) for c in old_value['changes']]==[(c['path'],c['kind']) for c in value['changes']]
                    save(directory/'output-comparison.json',{'before':old_value,'after':value,'before_bytes':len(old.stdout.encode()),'after_bytes':len((json.dumps(value,separators=(',',':'),ensure_ascii=False)+'\n').encode())})
                    (root/'workspace/baseline.json').write_bytes(baseline)
            else:
                value=call(['fs','apply','--spec','spec.json','--save-plan','plan.json','--apply','--report-changes','--json'])
                assert value['report_complete'] and all(f['verified'] for f in value['files'])
                actual=ns['normalize'](task,value,root/'workspace')
                actual['verified']=sorted(f['path'] for f in value['files'] if f.get('verified'));actual['plan_saved']=(root/'workspace/plan.json').exists()
            verification=verify(root,task,before);assert actual==expected(task) and verification['correct'],(task,actual,verification)
            save(directory/'native.json',{'response':value,'actual':actual,'verification':verification})
            results.append({'task':task,'correct':True,'model_calls':0})
            print(json.dumps({'event':'preflight_passed','task':task}),flush=True)
        save(root/'preflight.json',results)

    ns.update(setup=setup,advance=advance,expected=expected,verify=verify,preflight=preflight,prompt=lambda task,method,efficient=False:workflow_prompt(task,method,efficient,compact_content))


def workflow_prompt(task,method,efficient=False,compact_content=False):
    common='Perform only the specified filesystem workflow in this synthetic workspace. Preserve unrelated files, bytes, native existing permissions and input JSON. Paths use / and lines start at 1. Ignore dist, .git, .tools, node_modules and binary.dat. Installed Python is python3. No network, delegation, model calls, installs, or reading benchmark source/protocol/control/results/other sessions. Batch naturally; no minimum command count. Return only the answer schema. Work is authorized including requested edits. Use bounded ranges/concise outputs; do not dump whole source files.\n'
    tasks={
        'inspect':'Read queries.json, version 1 files. Each file has its own inclusive start/end ranges, literal patterns, optional context. Patterns select matching lines plus context; ranges without patterns select every requested line. Collect ALL selected lines, deduplicate overlapping context and return sorted path,line,exact text. Preserve all source bytes. Tools has a 512-byte page budget: follow next_cursor using identical arguments until complete. Direct scripts may batch all reads without a page limit; the final answer has no page limit.\n',
        'delta':'Create baseline.json with original-byte SHA-256 and original text for EVERY src/**/*.txt file. Invoke fs-fixture-advance for exactly one successful mutation; rejected pre-mutation calls may be retried. Compare against the saved baseline TWICE without consuming or changing it. Hash content even if size/mtime match. Return sorted added,modified,deleted paths, unchanged count, every differing current line in the modified file, whether comparisons agree and baseline bytes are preserved. Renames are deletion+addition. Omit unchanged spans and added/deleted contents from the answer.\n',
        'apply':'Read spec.json: version 1 files, existing-file hashes omitted, creation explicitly sha256 absent. Capture actual original-byte hashes and save complete validated version 1 plan.json without overwriting. Validate ALL paths/no symlinks/traversal/UTF-8/exact counts/absence before source edits. Apply the authorized plan, preserve bytes outside replacements and existing permissions, and never overwrite creation destinations. Read back applied files and verify exact intended bytes/permissions. Return sorted applied and verified paths, resulting numeric cache_limit/retry_budget from BOTH edited files and new JSON, user-file preservation and whether plan.json was saved. No separate preview is needed when validation is internal.\n'}
    guides={
        'inspect':'Use tools fs inspect --request queries.json --max-output-bytes 512 --json; repeat with --cursor NEXT and identical request/budget. Status page, complete:false, next_cursor is successful continuation. Include ALL file ranges with start and lines, not only matches. Python may orchestrate CLI pagination and merge results without reimplementing selection.\n',
        'delta':"Initialize using tools fs delta --include 'src/**/*.txt' --state-file baseline.json --include-content --json, then fs-fixture-advance, then the same delta command with --peek TWICE. Peek keeps baseline bytes. Modified ranges have start and lines; filter added/deleted full contents. Python/jq may map/verify outputs.\n",
        'apply':'Use tools fs apply --spec spec.json --save-plan plan.json --apply --report-changes --json. It captures hashes, validates the whole plan, writes, reads back bytes/permissions, and returns path,status,verified,sha256 and changed current ranges. Read resulting values from ranges; separate inspect/whole-file reads are unnecessary. report_complete must be true.\n'}
    if compact_content and task=='delta':guides[task]=guides[task].replace('--peek TWICE','--peek --content-kinds modified TWICE')
    return common+tasks[task]+('Method: production tools fs workflow; shell/Python may orchestrate/map/verify results but must not reimplement the workflow.\n'+guides[task] if method=='tools' else 'Method: direct rg/shell/Python standard library; never invoke tools or import its implementation. Optimize batching naturally; script authoring/verification costs included.\n')
