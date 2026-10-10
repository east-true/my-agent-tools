#!/usr/bin/env python3
"""Three paired fresh-session trials of each production filesystem command."""
import argparse
import hashlib
import json
import os
import random
import shutil
import signal
import statistics
import subprocess
import sys
import time
import tomllib
from datetime import datetime,timezone
from pathlib import Path

TASKS=['inspect','delta','apply']
REPETITIONS=3
SEED=20261010


def save(path,value):
    path.parent.mkdir(parents=True,exist_ok=True)
    path.write_text(json.dumps(value,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')


def sha(data):return hashlib.sha256(data).hexdigest()


def defaults():
    home=Path(os.environ.get('CODEX_HOME',str(Path.home()/'.codex')))
    data=tomllib.loads((home/'config.toml').read_text(encoding='utf-8')) if (home/'config.toml').exists() else {}
    profile=data.get('profiles',{}).get(data.get('profile'),{})
    return {key:profile.get(key,data.get(key)) for key in ('model','model_reasoning_effort','profile')}


def schedule(tasks=None):
    selected=set(TASKS if tasks is None else tasks)
    rng=random.Random(SEED);result=[]
    for repetition in range(1,REPETITIONS+1):
        tasks=TASKS.copy();rng.shuffle(tasks)
        for task in tasks:
            if task not in selected:continue
            methods=['direct','tools'] if (TASKS.index(task)+repetition)%2 else ['tools','direct']
            result.extend({'task':task,'method':method,'repetition':repetition} for method in methods)
    return result


def inventory(work):
    return {str(p.relative_to(work)):sha(p.read_bytes()) for p in sorted(work.rglob('*')) if p.is_file() and not p.is_symlink()}


def setup(root,task):
    work=root/'workspace';shutil.rmtree(work,ignore_errors=True);(work/'src').mkdir(parents=True);(work/'config').mkdir()
    (work/'user-work.txt').write_text('preserve uncommitted user work\n',encoding='utf-8')
    (work/'dist').mkdir();(work/'dist/decoy.txt').write_text('cache_limit=999\nretry_budget=999\n',encoding='utf-8')
    (work/'src/binary.dat').write_bytes(b'\x00\xff\xfe')
    for index in range(40):
        lines=[f'module {index:02d} neutral line {line:03d}' for line in range(1,121)]
        if index%5==0:lines[39]='cache_limit=16';lines[89]='retry_budget=3'
        lines[60]='limit=08'
        (work/f'src/module_{index:02d}.txt').write_text('\n'.join(lines)+'\n',encoding='utf-8')
    control={'task':task,'advances':0,'before':inventory(work),'before_modes':{str(p.relative_to(work)):p.stat().st_mode&0o777 for p in work.rglob('*') if p.is_file()}};save(root/'fixture-control'/'control.json',control)
    if task=='apply':
        files=[]
        for index in (0,5):
            name=f'src/module_{index:02d}.txt';files.append({'path':name,'sha256':sha((work/name).read_bytes()),'replacements':[{'old':'cache_limit=16','new':'cache_limit=64','count':1},{'old':'retry_budget=3','new':'retry_budget=5','count':1}]})
        files.append({'path':'config/limits.json','sha256':'absent','content':'{"cache_limit":64,"retry_budget":5}\n'})
        save(work/'edits.json',{'version':1,'files':files})
        control['before']=inventory(work);control['before_modes']={str(p.relative_to(work)):p.stat().st_mode&0o777 for p in work.rglob('*') if p.is_file()};save(root/'fixture-control'/'control.json',control)
    return control['before']


def advance(root):
    control=json.loads((root/'fixture-control'/'control.json').read_text(encoding='utf-8'))
    if control['task']!='delta' or control['advances']!=0:raise RuntimeError('advance only once for the delta workload')
    work=root/'workspace';baseline=work/'baseline.json'
    data=baseline.read_text(encoding='utf-8')
    json.loads(data)
    for name,digest in control['before'].items():
        if name.startswith('src/') and name.endswith('.txt') and digest not in data:raise RuntimeError('baseline must contain every selected file content SHA-256 before advance')
    p=work/'src/module_10.txt';info=p.stat();p.write_bytes(p.read_bytes().replace(b'limit=08',b'limit=64'));os.utime(p,ns=(info.st_atime_ns,info.st_mtime_ns))
    (work/'src/module_11.txt').unlink();(work/'src/module_15.txt').rename(work/'src/moved.txt')
    (work/'src/new.txt').write_text('new module\n',encoding='utf-8')
    control['advances']=1;save(root/'fixture-control'/'control.json',control)


def expected(task):
    if task=='inspect':return {'matches':[{'path':f'src/module_{i:02d}.txt','line':line,'text':text} for i in range(0,40,5) for line,text in [(40,'cache_limit=16'),(90,'retry_budget=3')]]}
    if task=='delta':return {'added':['src/moved.txt','src/new.txt'],'modified':['src/module_10.txt'],'deleted':['src/module_11.txt','src/module_15.txt'],'unchanged':37,'changed_line':{'path':'src/module_10.txt','line':61,'text':'limit=64'}}
    return {'applied':['config/limits.json','src/module_00.txt','src/module_05.txt'],'cache_limit':64,'retry_budget':5,'user_files_preserved':True}


def schema(value):
    if isinstance(value,dict):return {'type':'object','additionalProperties':False,'required':list(value),'properties':{k:schema(v) for k,v in value.items()}}
    if isinstance(value,list):return {'type':'array','items':schema(value[0]) if value else {}}
    return {'type':'boolean' if isinstance(value,bool) else 'integer' if isinstance(value,int) else 'string'}


def prompt(task,method,efficient=False):
    shared='''Perform the specified filesystem task inside the current synthetic workspace. Keep all unrelated files and bytes unchanged. Root-relative paths use / separators. UTF-8 text; line numbers start at 1. Ignore dist, .git, .tools, node_modules and binary.dat. Return only the requested JSON schema. No network, model calls, GitHub operations, delegation, installs or source code changes outside the task. Do not read the benchmark runner, frozen source, protocol, results, control file, reference answers or other sessions. Optimize naturally: batching, rg filters and local Python scripts are allowed; no minimum command count.\n'''
    if task=='inspect':shared+='Select all src/**/*.txt files. Find literal cache_limit and retry_budget and collect every matching path, 1-based line number and exact line text, sorted by path then line. Do not modify any files.\n'
    elif task=='delta':shared+='Create baseline.json with the SHA-256 and original text for EVERY selected src/**/*.txt file. Then invoke fs-fixture-advance exactly once to reveal changes. Compare against the saved baseline and return sorted added, modified and deleted paths, unchanged file count, and the changed line in the modified file. Renames are deletion+addition. Content can change with the same byte size and mtime, so use content hashes. Keep user-work.txt, decoys and binary data unchanged.\n'
    else:shared+='Read edits.json. Preview and validate EVERY edit before any file change: root-relative paths, no symlinks or path traversal, expected original-byte SHA-256 or absence, UTF-8, exact replacement counts. Apply the specified changes preserving existing bytes outside replacements and file permissions. Do not overwrite an existing creation destination. Return sorted successfully applied paths, the resulting numeric settings from both edited files/new JSON, and whether unrelated user files were preserved. Do not change edits.json.\n'
    if efficient:
        shared+='The installed Python executable is python3, not python. Use bounded searches/ranges or concise JSON filters for verification; do not dump whole source files.\n'
        if task=='delta':
            shared+='A rejected fs-fixture-advance call before any mutation may be retried; perform exactly one successful advance after the baseline is ready.\n'
            shared=shared.replace('invoke fs-fixture-advance exactly once','invoke fs-fixture-advance for exactly one successful mutation')
    if method=='tools':
        guides={'inspect':"Use tools fs inspect --root . --include 'src/**/*.txt' --pattern cache_limit --pattern retry_budget --json. Files contain matches (line numbers) and ranges ({start,lines}); map each matched line to its exact text.\n",
                'delta':"Use tools fs delta --root . --include 'src/**/*.txt' --state-file baseline.json --include-content --json before and after fs-fixture-advance. First status initialized, then changes with kind added/modified/deleted; each range contains start and lines. Return the requested task answer, not CLI metadata.\n",
                'apply':"Use tools fs apply --root . --plan edits.json --json to preview; then the same with --apply --json. Read the edited files to verify values and unrelated files. Results contain applied count and files with path,status,sha256.\n"}
        if efficient and task=='apply':
            guides[task]="Use tools fs apply --root . --plan edits.json --apply --json once: it internally validates all preimages and counts before any writes. A separate preview CLI invocation is unnecessary for this already authorized batch. Verify resulting scalar settings with tools fs inspect --path src/module_00.txt --path src/module_05.txt --path config/limits.json --pattern cache_limit --pattern retry_budget --json. Do not dump full files.\n"
        if efficient and task=='delta':
            guides[task]+="Pipe/parse the final result with python3 or jq to retain only change path/kind, unchanged count and the modified file's ranges; added/deleted full contents are unnecessary for this answer. The initial result is already compact.\n"
        return shared+'Method: use the production tools fs commands for the requested workflow; normal shell/Python may map/verify results. Never reimplement the workflow in Python.\n'+guides[task]
    return shared+'Method: direct shell/rg/Python standard-library operations; never invoke tools or import its implementation. Implement the same behavior yourself as needed; source-authoring and verification costs are included. rg --files -g, rg -n -F, Python pathlib/hashlib/json/os/stat and local scripts are available.\n'


def verify(root,task,before):
    work=root/'workspace';after=inventory(work);checks={}
    if task=='inspect':checks['all_original_bytes_preserved']=all(after.get(name)==digest for name,digest in before.items());checks['no_workflow_mutations']=not any(name not in before for name in after if not name.endswith('.py'))
    elif task=='delta':
        control=json.loads((root/'fixture-control'/'control.json').read_text(encoding='utf-8'));checks['exactly_one_controlled_advance']=control['advances']==1
        altered={'src/module_10.txt','src/module_11.txt','src/module_15.txt'}
        checks['unrelated_bytes_preserved']=all(after.get(name)==digest for name,digest in before.items() if name not in altered)
        checks['expected_add_delete']=all((work/name).is_file() for name in ('src/moved.txt','src/new.txt')) and not any((work/name).exists() for name in ('src/module_11.txt','src/module_15.txt'))
        checks['modified_exactly']=after.get('src/module_10.txt')==sha(('\n'.join([f'module 10 neutral line {i:03d}' if i not in (40,61,90) else {40:'cache_limit=16',61:'limit=64',90:'retry_budget=3'}[i] for i in range(1,121)])+'\n').encode())
    else:
        edited={'src/module_00.txt','src/module_05.txt'}
        checks['unrelated_bytes_preserved']=all(after.get(name)==digest for name,digest in before.items() if name not in edited)
        plan=json.loads((work/'edits.json').read_text(encoding='utf-8'));valid=True
        for item in plan['files']:
            target=work/item['path']
            if 'content' in item:valid=valid and target.is_file() and target.read_bytes()==item['content'].encode()
            else:
                index=int(Path(item['path']).stem[-2:]);lines=[f'module {index:02d} neutral line {i:03d}' for i in range(1,121)];lines[39]='cache_limit=64';lines[89]='retry_budget=5';lines[60]='limit=08';valid=valid and after.get(item['path'])==sha(('\n'.join(lines)+'\n').encode())
        checks['all_requested_bytes_match']=valid
    control=json.loads((root/'fixture-control'/'control.json').read_text(encoding='utf-8'))
    checks['existing_permissions_preserved']=all(not (work/name).exists() or (work/name).stat().st_mode&0o777==mode for name,mode in control['before_modes'].items())
    checks['no_internal_apply_temporaries']=not any(p.name.startswith('.tools-fs-') for p in work.rglob('*'))
    return {'correct':all(checks.values()),'checks':checks}


def normalize(task,value,work):
    if task=='inspect':
        rows=[]
        for file in value['files']:
            lines={block['start']+i:text for block in file.get('ranges',[]) for i,text in enumerate(block['lines'])}
            rows.extend({'path':file['path'],'line':n,'text':lines[n]} for n in file.get('matches',[]))
        return {'matches':rows}
    if task=='delta':
        changes=value['changes'];modified=[c for c in changes if c['kind']=='modified'][0]
        return {'added':[c['path'] for c in changes if c['kind']=='added'],'modified':[c['path'] for c in changes if c['kind']=='modified'],'deleted':[c['path'] for c in changes if c['kind']=='deleted'],'unchanged':value['unchanged'],'changed_line':{'path':modified['path'],'line':modified['ranges'][0]['start'],'text':modified['ranges'][0]['lines'][0]}}
    paths=sorted(file['path'] for file in value['files'] if file['status']=='applied')
    settings=json.loads((work/'config/limits.json').read_text(encoding='utf-8'))
    return {'applied':paths,'cache_limit':settings['cache_limit'],'retry_budget':settings['retry_budget'],'user_files_preserved':(work/'user-work.txt').read_bytes()==b'preserve uncommitted user work\n'}


def preflight(root,tasks=None):
    results=[]
    for task in TASKS if tasks is None else tasks:
        before=setup(root,task);env=environment(root);directory=root/'preflight'/task;directory.mkdir(parents=True)
        def call(args):
            r=subprocess.run(['codex','sandbox','--permission-profile','filesystem_benchmark',*permissions(root),'--cd',str(root/'workspace'),'--',str(root/'bin/tools'),*args],cwd=root/'workspace',env=env,capture_output=True,text=True,encoding='utf-8',check=True)
            value=json.loads(r.stdout);assert value['complete'];return value
        if task=='inspect':value=call(['fs','inspect','--include','src/**/*.txt','--pattern','cache_limit','--pattern','retry_budget','--json'])
        elif task=='delta':
            args=['fs','delta','--include','src/**/*.txt','--state-file','baseline.json','--include-content','--json'];initial=call(args);assert initial['status']=='initialized' and initial['tracked']==40;advance(root);value=call(args)
        else:
            args=['fs','apply','--plan','edits.json','--json'];preview=call(args);assert preview['applied']==0;value=call(args+['--apply']);assert value['applied']==3
        verification=verify(root,task,before);actual=normalize(task,value,root/'workspace')
        assert actual==expected(task) and verification['correct'],(task,actual,verification)
        save(directory/'native.json',{'response':value,'actual':actual,'verification':verification});results.append({'task':task,'correct':True,'model_calls':0})
        print(json.dumps({'event':'preflight_passed','task':task}),flush=True)
    save(root/'preflight.json',results)


def environment(root):
    env=dict(os.environ);env['PATH']=str(root/'bin')+os.pathsep+env['PATH'];env['FS_BENCHMARK_ROOT']=str(root);return env


def permissions(root):
    return ['-c','permissions.filesystem_benchmark.filesystem='+('{":root"="read",'+json.dumps(str(root/'workspace'))+'="write",'+json.dumps(str(root/'fixture-control'))+'="write"}'),'-c','permissions.filesystem_benchmark.network.enabled=false']


def run_trial(root,step,text):
    before=setup(root,step['task']);directory=root/'runs'/step['task']/f"{step['repetition']}-{step['method']}";directory.mkdir(parents=True)
    (directory/'prompt.txt').write_text(text,encoding='utf-8');save(directory/'schema.json',schema(expected(step['task'])))
    args=['codex','exec','--json','--ephemeral','--skip-git-repo-check','-c','approval_policy="never"','-c','default_permissions="filesystem_benchmark"',*permissions(root),'-c','features.multi_agent=false','--color','never','--cd',str(root/'workspace'),'--output-schema',str(directory/'schema.json'),'--output-last-message',str(directory/'answer.json'),'-']
    started=time.monotonic();timed_out=False
    with (directory/'events.jsonl').open('w',encoding='utf-8') as out,(directory/'stderr.log').open('w',encoding='utf-8') as err:
        process=subprocess.Popen(args,stdin=subprocess.PIPE,stdout=out,stderr=err,text=True,encoding='utf-8',env=environment(root),start_new_session=True)
        try:process.communicate(text,timeout=300)
        except subprocess.TimeoutExpired:timed_out=True;os.killpg(process.pid,signal.SIGKILL);process.wait()
    raw=(directory/'events.jsonl').read_bytes();events=[json.loads(line) for line in raw.splitlines() if line.strip()];usages=[e['usage'] for e in events if e.get('type')=='turn.completed'];usage=usages[0] if len(usages)==1 else None
    try:actual=json.loads((directory/'answer.json').read_text(encoding='utf-8'))
    except (OSError,ValueError):actual=None
    commands=[e['item'] for e in events if e.get('type')=='item.completed' and e.get('item',{}).get('type')=='command_execution']
    verification=verify(root,step['task'],before);save(directory/'state-verification.json',verification);save(directory/'actual-files.json',inventory(root/'workspace'))
    record=dict(step,exit_code=process.returncode,timed_out=timed_out,usage=usage,answer_correct=actual==expected(step['task']),state_correct=verification['correct'],actual=actual,state_checks=verification['checks'],commands=[c['command'] for c in commands],command_calls=len(commands),shell_output_bytes=sum(len(c.get('aggregated_output','').encode()) for c in commands),seconds=round(time.monotonic()-started,3),execution_args=args,prompt_bytes=len(text.encode()),events_sha256=sha(raw))
    record['correct']=record['answer_correct'] and record['state_correct'] and process.returncode==0 and usage is not None
    if usage:record.update(input_plus_output=usage['input_tokens']+usage['output_tokens'],uncached_plus_output=usage['input_tokens']-usage['cached_input_tokens']+usage['output_tokens'])
    return record


def summarize(records,tasks=None):
    result={}
    for task in TASKS if tasks is None else tasks:
        groups={}
        for method in ('direct','tools'):
            rows=[r for r in records if r['task']==task and r['method']==method];measured=[r for r in rows if r['usage'] is not None]
            metrics={}
            for key in ('uncached_plus_output','input_plus_output','command_calls','shell_output_bytes','seconds'):
                values=[r[key] for r in measured]
                if values:metrics[key]={'mean':round(statistics.mean(values),6),'min':min(values),'max':max(values),'stdev':round(statistics.stdev(values),6) if len(values)>1 else 0}
            groups[method]={'n':len(rows),'correct':sum(r['correct'] for r in rows),'metrics':metrics}
        eligible=all(g['n']==g['correct']==REPETITIONS for g in groups.values());groups['comparison_eligible']=eligible
        if eligible:groups['change_percent']={key:round(100*(groups['tools']['metrics'][key]['mean']/groups['direct']['metrics'][key]['mean']-1),6) for key in ('uncached_plus_output','input_plus_output')}
        result[task]=groups
    return result


def manifest(source):return [{'path':str(p.relative_to(source)),'sha256':sha(p.read_bytes())} for p in sorted(source.rglob('*')) if p.is_file()]


def validate_frozen(root,protocol):
    if manifest(root/'source')!=protocol['source_manifest']:raise RuntimeError('frozen production source changed')
    if sha((root/'harness/run_fs_benchmark.py').read_bytes())!=protocol['runner_sha256']:raise RuntimeError('frozen harness changed')
    if sha((root/'bin/tools').read_bytes())!=protocol['cli_sha256']:raise RuntimeError('production CLI changed')
    for name,digest in protocol.get('runner_manifest',{}).items():
        if sha((root/'harness'/name).read_bytes())!=digest:raise RuntimeError('frozen supporting harness changed')


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--root',type=Path,required=True);parser.add_argument('--resume',action='store_true');parser.add_argument('--run-models',action='store_true');parser.add_argument('--advance',action='store_true');parser.add_argument('--tasks',nargs='+',choices=TASKS);parser.add_argument('--efficient-usage',action='store_true');parser.add_argument('--workflows',action='store_true');parser.add_argument('--legacy-cli',type=Path);parser.add_argument('--compact-content',action='store_true');parser.add_argument('--completion-workflows',action='store_true');parser.add_argument('--batch-workflows',action='store_true');parser.add_argument('--recount-workflows',action='store_true')
    args=parser.parse_args();root=args.root.resolve();selected=[t for t in TASKS if args.tasks is None or t in args.tasks]
    recount=args.recount_workflows
    if (args.resume or args.advance) and (root/'protocol.json').exists():recount=recount or json.loads((root/'protocol.json').read_text()).get('recount_workflows',False)
    batch='--batch-workflows' in sys.argv or recount
    if (args.resume or args.advance) and (root/'protocol.json').exists():batch=batch or json.loads((root/'protocol.json').read_text()).get('batch_workflows',False)
    completion=args.completion_workflows or batch
    if (args.resume or args.advance) and (root/'protocol.json').exists():completion=completion or json.loads((root/'protocol.json').read_text()).get('completion_workflows',False)
    workflows='--workflows' in sys.argv or completion
    compact_content=args.compact_content
    if (args.resume or args.advance) and (root/'protocol.json').exists():workflows=workflows or json.loads((root/'protocol.json').read_text()).get('workflows',False)
    if (args.resume or args.advance) and (root/'protocol.json').exists():compact_content=compact_content or json.loads((root/'protocol.json').read_text()).get('compact_content',False)
    if compact_content and not workflows:parser.error('--compact-content requires --workflows')
    if completion:
        import fs_completion_benchmark
        fs_completion_benchmark.install(globals(),batch=batch,recount=recount)
    elif workflows:
        import fs_workflow_benchmark
        fs_workflow_benchmark.install(globals(),compact_content)
    if args.advance:advance(root);return
    if root==Path('/tmp') or not root.is_relative_to('/tmp') or root.exists() and not args.resume:parser.error('fresh dedicated /tmp root, or --resume')
    if not args.resume:
        repo=Path(__file__).resolve().parents[2];root.mkdir();source=root/'source';source.mkdir()
        for name in ('go.mod','go.sum'):shutil.copy2(repo/name,source/name)
        for name in ('cmd','internal'):shutil.copytree(repo/name,source/name)
        (root/'bin').mkdir();subprocess.run(['go','build','-trimpath','-buildvcs=false','-o',str(root/'bin/tools'),'./cmd/tools'],cwd=source,check=True)
        (root/'harness').mkdir();shutil.copy2(Path(__file__),root/'harness/run_fs_benchmark.py')
        runner_manifest={}
        if workflows:
            helper=Path(__file__).with_name('fs_workflow_benchmark.py');shutil.copy2(helper,root/'harness'/helper.name);runner_manifest[helper.name]=sha(helper.read_bytes())
        if completion:
            helper=Path(__file__).with_name('fs_completion_benchmark.py');shutil.copy2(helper,root/'harness'/helper.name);runner_manifest[helper.name]=sha(helper.read_bytes())
        if args.legacy_cli:shutil.copy2(args.legacy_cli,root/'legacy-tools');(root/'legacy-tools').chmod(0o755)
        wrapper=root/'bin/fs-fixture-advance';wrapper.write_text('#!/bin/sh\nexec '+shutil.which('python3')+' '+str(root/'harness/run_fs_benchmark.py')+' --root '+str(root)+' --advance\n',encoding='utf-8');wrapper.chmod(0o755)
        preflight(root,selected)
        protocol={'version':1,'measured_at_utc':datetime.now(timezone.utc).isoformat(),'configured_defaults':defaults(),'tasks':selected,'repetitions_per_method':REPETITIONS,'max_model_calls':len(selected)*2*REPETITIONS,'schedule':schedule(selected),'efficient_usage':args.efficient_usage,'guidance_revision':2,'write_scope':'workspace and fixture-control directory only','seed':SEED,'source_manifest':manifest(source),'runner_sha256':sha((root/'harness/run_fs_benchmark.py').read_bytes()),'cli_sha256':sha((root/'bin/tools').read_bytes()),'prompts':{task:{method:prompt(task,method,args.efficient_usage) for method in ('direct','tools')} for task in selected},'cache_controlled':False,'settings':'inherited defaults; no model/effort override or --ignore-user-config','limitations':['Fixed synthetic workload, three trials per method.','Backend cache cannot be reset; total and uncached token metrics differ.','CLI installation/build excluded; direct script authoring included.','Runtime model/effort not exposed in exec events; configuration snapshot and absent overrides recorded.'],'inclusion_policy':'Retain every scheduled trial including failures, no replacement runs; abort on missing usage/quota or fixture defects.'}
        protocol.update(workflows=workflows,completion_workflows=completion,compact_content=compact_content,runner_manifest=runner_manifest,legacy_cli_sha256=sha((root/'legacy-tools').read_bytes()) if (root/'legacy-tools').exists() else None,scenario_revision='batched-workflows' if workflows else 'initial',guidance_revision=4 if workflows else 2)
        if completion:protocol.update(completion_revision=2,scenario_revision='completion-workflows-v2',guidance_revision=5)
        if batch:protocol.update(batch_workflows=True,completion_revision=3,scenario_revision='completion-workflows-v3-batch',guidance_revision=6)
        if recount:protocol.update(recount_workflows=True,completion_revision=4,scenario_revision='completion-workflows-v4-recount',guidance_revision=7)
        save(root/'protocol.json',protocol);records=[]
    else:
        protocol=json.loads((root/'protocol.json').read_text(encoding='utf-8'));records=json.loads((root/'results.json').read_text(encoding='utf-8'))
        if protocol['configured_defaults']!=defaults():raise RuntimeError('user defaults changed')
        validate_frozen(root,protocol)
    save(root/'results.json',records);save(root/'report.json',{'protocol':protocol,'trials':records,'summary':summarize(records,protocol['tasks'])})
    if args.run_models:
        for step in protocol['schedule']:
            if any(all(r[k]==v for k,v in step.items()) for r in records):continue
            if defaults()!=protocol['configured_defaults']:raise RuntimeError('user defaults changed during study')
            print(json.dumps({'event':'trial_started',**step}),flush=True);record=run_trial(root,step,protocol['prompts'][step['task']][step['method']]);records.append(record)
            save(root/'results.json',records);save(root/'report.json',{'protocol':protocol,'trials':records,'summary':summarize(records,protocol['tasks'])})
            print(json.dumps({'event':'trial_completed',**{k:record.get(k) for k in ('task','method','repetition','correct','state_correct','input_plus_output','uncached_plus_output','seconds')}}),flush=True)
            if record['usage'] is None:raise RuntimeError('missing usage/quota; evidence retained; stopping without replacement')


if __name__=='__main__':main()
