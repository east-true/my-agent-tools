#!/usr/bin/env python3
"""Preregistered 15-command benchmark, three paired trials per default method."""
import argparse
import hashlib
import io
import json
import random
import shutil
import statistics
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

import run_all_github_benchmark as suite
import run_command_benchmark as core
import run_default_benchmark as small
from github_graphql_fixture import FixtureGraphQL

TASKS=suite.TASKS
REPETITIONS=3
SEED=20261008


class Handler(suite.Handler):
    def respond(self):
        s=self.server;parsed=urlsplit(self.path);raw=self.rfile.read(int(self.headers.get('Content-Length',0)))
        payload=json.loads(raw or '{}');path=parsed.path
        if path=='/graphql':
            if not payload and parsed.query:payload={key:values[0] for key,values in parse_qs(parsed.query).items()}
            result=s.graphql.execute(payload)
        elif path.endswith('/status') and '/commits/' in path:
            result={'state':'pending','statuses':[],'total_count':0,'sha':s.head}
        else:
            self.rfile=io.BytesIO(raw)
            if s.scenario=='cleanup-apply':return core.cleanup.Handler.respond(self)
            return super().respond()
        data=json.dumps(result,ensure_ascii=False,separators=(',',':')).encode()
        s.accesses.append({'method':self.command,'endpoint':self.path,'payload':payload,'status':200,'response_bytes':len(data),
                           'graphql_errors':result.get('errors',[])})
        self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(data)))
        self.end_headers();self.wfile.write(data)


def setup(root,server,task):
    result=suite.setup(root,server,task)
    server.graphql=FixtureGraphQL(server)
    return result


def schema(value):
    result=suite.schema(value)
    if isinstance(value,dict) and 'head' in value:
        result['properties']['head']['description']='Current PR head commit SHA (40 hex characters), not a branch name.' if len(value['head'])==40 else 'PR source branch name.'
    return result


def prompt(task,method):
    value=suite.prompt(task,method)
    # The output contract is identical for both methods, and describes field
    # meaning without giving the ground-truth values to the model.
    if task in ('pr-reviews','pr-inspect','pr-delta','pr-submit','pr-merge','ci-failures','ci-rerun'):
        value+='\nOutput head means the full commit SHA, never the branch name.\n'
    if task in ('pr-submit','pr-merge'):
        value+='\nIf using combined commit status, an empty statuses collection with total_count 0 is not an extra required check; use actual Check Runs and PR merge conditions.\n'
    value+='\nNamed or anonymous GraphQL, aliases, fragments and selected-field projection are supported for the documented fixture types. Optimize naturally; no required minimum command count.\n'
    return value


def summarize(records,tasks=None):
    result={}
    for task in TASKS if tasks is None else tasks:
        result[task]={}
        for method in ('gh','tools'):
            rows=[r for r in records if r['task']==task and r['method']==method]
            usable=[r for r in rows if r.get('usage') is not None]
            metrics={}
            for key in ('input_plus_output','uncached_plus_output','uncached_input','cached_input','output_tokens','api_calls','command_calls','seconds'):
                values=[r[key] for r in usable]
                if values:metrics[key]={'mean':statistics.mean(values),'median':statistics.median(values),'min':min(values),'max':max(values),'stdev':statistics.stdev(values) if len(values)>1 else 0}
            result[task][method]={'n':len(rows),'usage_n':len(usable),'correct':sum(r['correct'] for r in rows),
                                 'state_correct':sum(r['state_correct'] for r in rows),'metrics':metrics}
        gh,to=result[task]['gh'],result[task]['tools']
        comparable=gh['n']==to['n']==REPETITIONS and gh['correct']==to['correct']==REPETITIONS
        result[task]['comparison_eligible']=comparable
        if comparable:
            result[task]['change_percent']={key:100*(to['metrics'][key]['mean']/gh['metrics'][key]['mean']-1)
                                            for key in ('input_plus_output','uncached_plus_output')}
        pairs=[]
        for repetition in range(1,REPETITIONS+1):
            rows={r['method']:r for r in records if r['task']==task and r['repetition']==repetition}
            if len(rows)==2 and all(r['usage'] is not None for r in rows.values()):
                pairs.append({'repetition':repetition,'both_correct':all(r['correct'] for r in rows.values()),
                              'uncached_change_percent':100*(rows['tools']['uncached_plus_output']/rows['gh']['uncached_plus_output']-1),
                              'total_change_percent':100*(rows['tools']['input_plus_output']/rows['gh']['input_plus_output']-1)})
        result[task]['paired_changes']=pairs
    return result


def source_hashes(source):
    return {str(p.relative_to(source)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(source.rglob('*')) if p.is_file()}


def validate_frozen(root,protocol):
    if source_hashes(root/'source')!=protocol['source_hashes']:raise RuntimeError('frozen Go source changed')
    for name,digest in protocol['runner_hashes'].items():
        if hashlib.sha256((root/'harness'/name).read_bytes()).hexdigest()!=digest:raise RuntimeError('frozen harness changed: '+name)
    if hashlib.sha256((root/'bin/tools').read_bytes()).hexdigest()!=protocol['cli_sha256']:raise RuntimeError('CLI binary changed')


def save_report(root,protocol,records):
    core.save(root/'results.json',records)
    core.save(root/'report.json',{'protocol':protocol,'trials':records,'summary':summarize(records,protocol['tasks'])})


def interface_call(root,server,directory,task,args):
    env=suite.environment(root,server,directory,task)
    result=subprocess.run(args,cwd=root/'workspace',env=env,capture_output=True,text=True,timeout=90)
    if result.returncode:raise RuntimeError(str(args)+': '+result.stderr+'\n'+result.stdout)
    return result.stdout


def preflight(root,servers,tasks=None):
    records=[]
    for task in TASKS if tasks is None else tasks:
        server=servers[task=='cleanup-apply'];before,_=setup(root,server,task)
        directory=root/'preflight'/task;directory.mkdir(parents=True)
        server.interface_access_path=directory/'interface-access.jsonl';responses=[]
        for command in suite.COMMANDS[task]:
            result=subprocess.run(['codex','sandbox','--permission-profile','command_benchmark',*core.permission_args(root),'--cd',str(root/'workspace'),'--',*command],
                cwd=root/'workspace',env=suite.environment(root,server,directory,task),capture_output=True,text=True,timeout=90)
            response=json.loads(result.stdout)
            allowed=result.returncode==0 or (task=='ci-rerun' and result.returncode==1 and response.get('status')=='failed' and response.get('failure',{}).get('complete'))
            if not allowed:raise RuntimeError(task+': '+result.stderr+'\n'+result.stdout)
            responses.append(response)
        verification=suite.verify(root,server,task,before);actual=suite.normalize(task,responses,server)
        if actual!=suite.expected(task) or not verification['correct']:raise RuntimeError(str((task,actual,suite.expected(task),verification)))
        core.save(directory/'native.json',{'responses':responses,'verification':verification,'actual':actual,'api_access':server.accesses})
        # Verify the real gh transport reads the same current identity and
        # accepts a deliberately smaller anonymous, aliased selection set.
        output=interface_call(root,server,directory,task,['gh','api','repos/'+(core.cleanup.REPO if task=='cleanup-apply' else core.REPO)])
        assert json.loads(output)['default_branch']=='main'
        if task!='cleanup-apply':
            query='{ repository(owner:"fixture",name:"command-benchmark") { pullRequest(number:7) { sha:headRefOid merged } } }'
            projected=json.loads(interface_call(root,server,directory,task,['gh','api','graphql','-f','query='+query]))
            assert projected=={'data':{'repository':{'pullRequest':{'sha':server.head,'merged':server.merged}}}}
            combined=json.loads(interface_call(root,server,directory,task,['gh','api',f'repos/{core.REPO}/commits/{server.head}/status']))
            assert combined=={'state':'pending','statuses':[],'total_count':0,'sha':server.head}
        else:
            projected=json.loads(interface_call(root,server,directory,task,['gh','api','graphql','-f',
                'query={ repository(owner:"fixture",name:"branch-cleanup") { issues(states:CLOSED,first:100) { nodes { number } pageInfo { hasNextPage } } } }']))
            assert len(projected['data']['repository']['issues']['nodes'])==3
        records.append({'task':task,'native_correct':True,'real_gh_contract':True,'model_calls':0})
        print(json.dumps({'event':'preflight_passed','task':task}),flush=True)
    core.save(root/'preflight.json',records)
    return records


def schedule(tasks=None):
    # Pair order alternates within each task; task order is independently
    # shuffled for each round, avoiding a single later baseline phase.
    rng=random.Random(SEED);result=[]
    for repetition in range(1,REPETITIONS+1):
        shuffled=list(TASKS);rng.shuffle(shuffled)
        for task in shuffled:
            order=['gh','tools'] if (TASKS.index(task)+repetition)%2 else ['tools','gh']
            for method in order:result.append({'task':task,'method':method,'repetition':repetition})
    return result if tasks is None else [step for step in result if step['task'] in tasks]


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,required=True)
    parser.add_argument('--resume',action='store_true')
    parser.add_argument('--run-models',action='store_true')
    parser.add_argument('--tasks',nargs='+',choices=TASKS,help='only benchmark these commands; default: all')
    args=parser.parse_args();root=args.root.resolve()
    if root==Path('/tmp') or not root.is_relative_to('/tmp') or root.exists() and not args.resume:parser.error('fresh dedicated /tmp root, or --resume')
    repo=Path(__file__).resolve().parents[2]
    selected=[task for task in TASKS if args.tasks is None or task in args.tasks]
    if not args.resume:
        root.mkdir(parents=True);source=root/'source';source.mkdir()
        for name in ('go.mod','go.sum'):shutil.copy2(repo/name,source/name)
        for name in ('cmd','internal'):shutil.copytree(repo/name,source/name)
        small.build(root,source)
        wrapper=root/'bin/git'
        wrapper.write_text(wrapper.read_text().replace("if 'push' in args:","if 'fetch' in args:\n  args=[os.environ['BRANCH_BENCHMARK_REMOTE'] if a=='origin' else a for a in args]\n if 'push' in args:"))
        harness=root/'harness';harness.mkdir()
        for path in Path(__file__).parent.glob('*.py'):shutil.copy2(path,harness/path.name)
        shutil.copy2(Path(__file__).parent/'requirements.txt',harness/'requirements.txt')
    servers=[core.Backend(root),core.cleanup.Backend(root)]
    for server in servers:
        server.RequestHandlerClass=Handler
        threading.Thread(target=server.serve_forever,daemon=True).start()
    for name in suite.ORIGINAL:setattr(core,name,setup if name=='setup' else getattr(suite,name))
    core.schema=schema
    try:
        defaults=small.configured_defaults()
        if not args.resume:
            preflights=preflight(root,servers,selected)
            import graphql
            protocol={'version':1,'measured_at_utc':datetime.now(timezone.utc).isoformat(),'tasks':selected,
                'configured_defaults':defaults,'settings':'Inherited defaults; no model/effort override or --ignore-user-config',
                'repetitions_per_method':REPETITIONS,'max_model_calls':len(selected)*2*REPETITIONS,'schedule':schedule(selected),'seed':SEED,
                'primary_metric':'input_tokens - cached_input_tokens + output_tokens; not monetary cost',
                'secondary_metric':'input_tokens + output_tokens; cached input included','cache_controlled':False,'fresh_sessions':True,
                'inclusion_policy':'All scheduled trials, including model errors; no reruns to replace model failures. Abort on fixture defects or quota loss.',
                'source_hashes':source_hashes(root/'source'),'cli_sha256':hashlib.sha256((root/'bin/tools').read_bytes()).hexdigest(),
                'runner_hashes':source_hashes(root/'harness'),'graphql_core_version':graphql.__version__,'preflights':preflights,
                'prompts':{task:{method:prompt(task,method) for method in ('gh','tools')} for task in selected},
                'limitations':['Fixed synthetic workload, not production performance or all options.','Three repetitions per method; no population confidence claim.','Backend cache cannot be disabled here; order alternated and uncached tokens reported.','Runtime selected model/effort not exposed by exec events; config and omitted overrides recorded.']}
            import platform,sys
            environment={'python':sys.version.split()[0],'platform':platform.platform()}
            for name,command in [('go',['go','version']),('gh',[core.cleanup.REAL_GH,'--version']),('codex',['codex','--version'])]:
                environment[name]=subprocess.run(command,capture_output=True,text=True,check=True).stdout.splitlines()[0]
            core.save(root/'environment.json',environment)
            core.save(root/'protocol.json',protocol);records=[];save_report(root,protocol,records)
        else:
            protocol=json.loads((root/'protocol.json').read_text());records=json.loads((root/'results.json').read_text())
            if args.tasks is not None and selected!=protocol['tasks']:raise RuntimeError('selected tasks differ from frozen protocol')
            if defaults!=protocol['configured_defaults']:raise RuntimeError('user defaults changed')
            validate_frozen(root,protocol)
        if args.run_models:
            for step in protocol['schedule']:
                if any(all(r[key]==step[key] for key in step) for r in records):continue
                task,method=step['task'],step['method'];index=1+sum(r['task']==task for r in records)
                print(json.dumps({'event':'trial_started',**step}),flush=True)
                record=core.run_trial(root,servers,task,index,method,protocol['prompts'][task][method],None,None)
                record.update(repetition=step['repetition'],configured_defaults=defaults)
                if record['usage'] is not None:record['uncached_plus_output']=record['uncached_input']+record['output_tokens']
                records.append(record);save_report(root,protocol,records)
                print(json.dumps({'event':'trial_completed',**{key:record.get(key) for key in ('task','method','repetition','correct','state_correct','input_plus_output','uncached_plus_output','seconds')}}),flush=True)
                if record['usage'] is None:raise RuntimeError('missing usage/quota or interrupted trial; retained evidence; stopping')
                # A valid query rejected by fixture validation must not be
                # interpreted as a win for either method or retried silently.
                unsupported=[a for a in record['api_access'] if a.get('status')==400]
                if unsupported:raise RuntimeError('fixture HTTP 400; retain trial and audit before further model calls')
    finally:
        for server in servers:server.shutdown();server.server_close()


if __name__=='__main__':main()
