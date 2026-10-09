#!/usr/bin/env python3
"""Small current-CLI benchmark; model/effort inherit user defaults without overrides."""
import argparse
import hashlib
import io
import json
import os
import shutil
import subprocess
import threading
import tomllib
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

import run_command_benchmark as core

TASKS = ['pr-create', 'ci', 'reviews']
ORIGINAL = {name: getattr(core, name) for name in ('setup', 'expected', 'verify', 'environment')}
SHA = 'a' * 40
LOG = ('2026-10-07T00:00:00Z setup\n'
       '2026-10-07T00:00:01Z src/build.go:7:2: too many arguments in call to pkg.Build\n'
       '2026-10-07T00:00:01Z have (int, bool)\n'
       '2026-10-07T00:00:01Z want (int)\n'
       '2026-10-07T00:00:01Z src/build.go:9:4: T does not implement I (missing method Flush)\n'
       '2026-10-07T00:00:02Z ##[error]Process completed with exit code 1.\n')


class Handler(core.Handler):
    def respond(self):
        server = self.server
        path = urlsplit(self.path).path
        raw = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        payload = json.loads(raw or '{}')
        base = '/repos/' + core.REPO
        result, content, headers = None, 'application/json', {}
        if path == base + '/pulls/7':
            result = {'number': 7, 'head': {'sha': SHA}}
        elif path.endswith('/check-runs') and '/commits/' in path:
            result = {'check_runs': [{'id': 91, 'name': 'build', 'status': 'completed', 'conclusion': 'failure',
                       'app': {'slug': 'github-actions'}, 'details_url': f'https://github.com/{core.REPO}/actions/runs/42/job/11'}]}
        elif path.endswith('/statuses'):
            result = []
        elif path == base + '/actions/runs/42':
            result = {'id': 42, 'run_attempt': 2, 'head_sha': SHA, 'status': 'completed', 'conclusion': 'failure'}
        elif path == base + '/actions/runs/42/attempts/2/jobs':
            result = {'jobs': [{'id': 11, 'name': 'build', 'status': 'completed', 'conclusion': 'failure',
                       'check_run_url': f'https://api.github.com/{base.lstrip("/")}/check-runs/91',
                       'steps': [{'number': 2, 'name': 'compile', 'conclusion': 'failure',
                                  'started_at': '2026-10-07T00:00:01Z', 'completed_at': '2026-10-07T00:00:02Z'}]}]}
        elif path == base + '/check-runs/91/annotations':
            result = [{'path': 'src/build.go', 'start_line': 7, 'annotation_level': 'failure', 'message': 'compile error'}]
        elif path == base + '/actions/jobs/11/logs':
            server.accesses.append({'method': self.command, 'endpoint': self.path, 'payload': payload})
            self.send_response(302)
            self.send_header('Location', f'http://127.0.0.1:{server.server_port}/signed-log')
            self.end_headers()
            return
        elif path == '/signed-log':
            result, content = LOG, 'text/plain'
        elif path == base + '/pulls/7/reviews':
            result = []
        elif path == '/graphql' and 'MergePullRequest' in payload.get('query', ''):
            result = {'data': {'repository': {'pullRequest': {'url': f'https://github.com/{core.REPO}/pull/7',
                      'state': 'OPEN', 'isDraft': False, 'merged': False, 'headRefOid': SHA, 'baseRefOid': 'b' * 40,
                      'mergeable': 'MERGEABLE', 'mergeStateStatus': 'CLEAN', 'reviewDecision': 'APPROVED'}}}}
        elif path == '/graphql' and 'reviewThreads(' in payload.get('query', ''):
            threads = []
            for index in range(1, 21):
                comments = [{'id': f'C{index}', 'body': f'Please validate input {index}.', 'updated_at': '2026-10-07T00:00:00Z'}]
                if server.changed and index == 1:
                    comments.append({'id': 'C21', 'body': 'Please also reject blank input.', 'updated_at': '2026-10-07T01:00:00Z'})
                threads.append({'id': f'T{index}', 'path': f'file{index}.go', 'line': 10, 'is_resolved': False,
                                'comments': {'nodes': comments, 'pageInfo': {'hasNextPage': False}}})
            result = {'data': {'repository': {'pullRequest': {'url': f'https://github.com/{core.REPO}/pull/7',
                      'head_sha': SHA, 'review_decision': 'APPROVED',
                      'reviewThreads': {'nodes': threads, 'pageInfo': {'hasNextPage': False}}}}}}
        elif path == '/graphql' and 'ReviewCommentBodies' in payload.get('query', ''):
            result = {'data': {'nodes': [{'id': identifier, 'body': 'Please also reject blank input.' if identifier == 'C21' else f'Please validate input {identifier[1:]}.',
                       'updated_at': '2026-10-07T01:00:00Z' if identifier == 'C21' else '2026-10-07T00:00:00Z'}
                       for identifier in payload['variables']['ids']]}}
        elif path == '/fixture/advance':
            server.changed = True
            result = {'changed': True}
        if result is None:
            self.rfile = io.BytesIO(raw)
            return super().respond()
        server.accesses.append({'method': self.command, 'endpoint': self.path, 'payload': payload})
        body = result.encode() if content == 'text/plain' else json.dumps(result, ensure_ascii=False).encode()
        if self.command == 'GET' and content == 'application/json':
            headers['ETag'] = '"' + hashlib.sha256(body).hexdigest() + '"'
            if self.headers.get('If-None-Match') == headers['ETag']:
                self.send_response(304)
                self.send_header('ETag', headers['ETag'])
                self.end_headers()
                return
        self.send_response(200)
        self.send_header('Content-Type', content)
        self.send_header('Content-Length', str(len(body)))
        for name, value in headers.items():
            self.send_header(name, value)
        self.end_headers()
        self.wfile.write(body)


def setup(root, server, task):
    before, fixture = ORIGINAL['setup'](root, server, task if task == 'pr-create' else 'context')
    server.task, server.changed = task, False
    return before, fixture


def expected(task):
    if task == 'ci':
        return {'run': 42, 'attempt': 2, 'head': SHA, 'path': 'src/build.go', 'line': 7,
                'expected_arguments': 1, 'missing_method': 'Flush', 'exit_code': 1}
    if task == 'reviews':
        return {'head': SHA, 'initial_threads': 20, 'new_comments': [{'id': 'C21', 'thread_id': 'T1', 'body': 'Please also reject blank input.'}]}
    return ORIGINAL['expected'](task)


def verify(root, server, task, before):
    if task == 'pr-create':
        return ORIGINAL['verify'](root, server, task, before)
    after = core.snapshot(root, server)
    checks = {'git_and_user_files_preserved': before == after,
              'no_real_resource_mutations': not any(a['method'] != 'GET' and a['endpoint'] != '/graphql' for a in server.accesses),
              'requested_fixture_observed': any('/actions/runs/42' in a['endpoint'] for a in server.accesses) if task == 'ci' else server.changed}
    return {'correct': all(checks.values()), 'checks': checks}


def environment(root, server, directory, task):
    return ORIGINAL['environment'](root, server, directory, task)


def prompt(task, method):
    if task == 'pr-create':
        # PR creation needs labels and repository metadata, not issue types.
        return core.prompt_for(task, method).replace(
            ', and gh api repos/fixture/command-benchmark/issue-types', '').replace(
            '; only enabled types are selectable', '')
    shared = core.SHARED.replace('Read body.md for creation tasks.', '')
    shared += f'\nRepo: {core.REPO}. PR: 7. Return only the specified output schema.\n'
    if task == 'ci':
        shared += 'Collect the completed run 42 attempt failure evidence, revalidate it once, and return the Go compiler fact at src/build.go:7, expected arguments for pkg.Build, missing interface method and exit code. Do not repair code or rerun CI.\n'
        if method == 'tools':
            return shared + 'Use tools only: tools github ci failures --run 42 --json --compact twice. Return the facts from successful evidence. Never invoke gh.\n'
        return shared + f'''Use gh directly, never tools. Available: gh api repos/{core.REPO}/actions/runs/42; gh api repos/{core.REPO}/actions/runs/42/attempts/2/jobs; gh api repos/{core.REPO}/check-runs/91/annotations; gh api repos/{core.REPO}/actions/jobs/11/logs. Logs are plain text. Recheck run identity/attempt before reusing completed facts. Local scripts and jq filters are allowed.\n'''
    shared += 'Read the unresolved review thread snapshot, then call gh api fixture/advance (tools method: tools-fixture-advance) once to reveal a reply. Inspect again for the same head and return only the newly added comment, its thread ID and exact body, plus original thread count. No review submissions or code edits.\n'
    if method == 'tools':
        return shared + 'Use tools github pr inspect --number 7 --sections reviews --state-file review-state.json --json, then tools-fixture-advance, then the same inspect command. Do not invoke gh. Read added comment data in the second result.\n'
    return shared + f'''Use gh directly, never tools. GraphQL query: query {{ repository(owner:"fixture",name:"command-benchmark") {{ pullRequest(number:7) {{ head_sha:headRefOid review_decision:reviewDecision reviewThreads(first:100) {{ nodes {{ id path line is_resolved:isResolved comments(first:100) {{ nodes {{ id body updated_at:updatedAt }} pageInfo {{ hasNextPage endCursor }} }} }} pageInfo {{ hasNextPage endCursor }} }} }} }} }}. Use gh api graphql -f query='QUERY'. Recheck head with gh api repos/{core.REPO}/pulls/7. Compare comment IDs before/after fixture/advance; batching/jq/Python are allowed.\n'''


def report(protocol, records):
    return {'protocol': protocol, 'trials': records,
            'summary': {task: core.cleanup.summarize(
                [r for r in records if r['task'] == task and r.get('included', True)]) for task in TASKS}}


def configured_defaults():
    path = Path(os.environ.get('CODEX_HOME', str(Path.home() / '.codex'))) / 'config.toml'
    value = tomllib.loads(path.read_text()) if path.exists() else {}
    selected = value.get('profiles', {}).get(value.get('profile'), {})
    return {key: selected.get(key, value.get(key)) for key in ('model', 'model_reasoning_effort', 'profile')}


def build(root, source):
    core.cleanup.build_interface(root, source, build_vcs=False)
    # Retain NewAPI's production cache/retry transport and state provider; only
    # the API base URL is cloned to localhost, matching the gh wrapper boundary.
    (root / 'main-overlay.go').write_text('''package main
import("context";"os";"fmt";"github.com/east-true/my-agent-tools/internal/cli";"github.com/east-true/my-agent-tools/internal/github";"github.com/east-true/my-agent-tools/internal/command";sdk "github.com/google/go-github/v92/github")
func main(){ctx:=context.Background(); api,err:=github.NewAPI(ctx,command.Exec{});if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}; concrete:=api.(github.SDK);base:=os.Getenv("BRANCH_BENCHMARK_URL")+"/";concrete.Client,err=concrete.Client.Clone(sdk.WithURLs(&base,nil));if err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)};os.Exit(cli.RunBranchBenchmark(ctx,os.Args[1:],os.Stdin,os.Stdout,os.Stderr,concrete))}
''')
    subprocess.run(['go','build','-buildvcs=false','-overlay',str(root/'overlay.json'),'-o',str(root/'bin/tools'),'./cmd/tools'],cwd=source,check=True)
    (root/'bin/tools-fixture-advance').write_text('#!/bin/sh\nexec "'+str(root/'bin/gh')+'" api fixture/advance\n')
    (root/'bin/tools-fixture-advance').chmod(0o755)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,required=True)
    parser.add_argument('--run-models',action='store_true')
    parser.add_argument('--repetitions',type=int,default=1)
    parser.add_argument('--resume',action='store_true')
    parser.add_argument('--source',type=Path,help='freeze this Go source tree instead of the current checkout')
    parser.add_argument('--repair-baselines',action='store_true',
                        help='resume only: retain/exclude original PR/reviews gh trials and replace each once')
    args=parser.parse_args()
    root=args.root.resolve()
    if root==Path('/tmp') or not root.is_relative_to(Path('/tmp')) or (root.exists() and not args.resume) or args.repetitions<1:
        parser.error('use fresh dedicated /tmp root, positive repetitions, or --resume')
    if args.repair_baselines and not (args.resume and args.run_models):
        parser.error('--repair-baselines requires --resume --run-models')
    if not args.resume:
        root.mkdir(parents=True)
        source=root/'source'; source.mkdir()
        repo=args.source.resolve() if args.source else Path(__file__).resolve().parents[2]
        for name in ('go.mod','go.sum'):
            shutil.copy2(repo/name,source/name)
        for name in ('cmd','internal'):
            shutil.copytree(repo/name,source/name)
        build(root,source)
    server=core.Backend(root); server.RequestHandlerClass=Handler
    threading.Thread(target=server.serve_forever,daemon=True).start()
    for name in ORIGINAL:
        setattr(core,name,globals()[name])
    try:
        commands={'pr-create':core.COMMANDS['pr-create'],'ci':[['tools','github','ci','failures','--run','42','--compact','--json']]*2,
                  'reviews':[['tools','github','pr','inspect','--number','7','--sections','reviews','--state-file','review-state.json','--json'],['tools-fixture-advance'],['tools','github','pr','inspect','--number','7','--sections','reviews','--state-file','review-state.json','--json']]}
        defaults=configured_defaults()
        if not args.resume:
            for task in TASKS:
                before,_=setup(root,server,task)
                directory=root/'preflight'/task;directory.mkdir(parents=True)
                server.interface_access_path=directory/'interface-access.jsonl'
                results=[]
                for command in commands[task]:
                    result=subprocess.run(['codex','sandbox','--permission-profile','command_benchmark',*core.permission_args(root),'--cd',str(root/'workspace'),'--',*command],cwd=root/'workspace',env=environment(root,server,directory,task),capture_output=True,text=True)
                    if result.returncode: raise RuntimeError(task+': '+result.stderr+'\n'+result.stdout)
                    results.append(json.loads(result.stdout))
                verification=verify(root,server,task,before)
                if not verification['correct']: raise RuntimeError(f'{task}: native state verification failed: {verification}')
                if task=='ci':
                    fact=results[-1]['evidence'][0]
                    assert fact['kind']=='go_compiler' and fact['expected_arguments']['pkg.Build']==1 and fact['missing_interface_methods']==['Flush']
                elif task=='reviews':
                    assert len(results[0]['reviews']['threads'])==20 and results[-1]['added']['comment:C21']['comment']['body']==expected(task)['new_comments'][0]['body']
                else: assert server.created_prs[0]['title']==core.TITLE
                core.save(directory/'result.json',{'results':results,'verification':verification,'api_access':server.accesses})
                print(json.dumps({'event':'preflight_passed','task':task}),flush=True)
            hashes={str(p.relative_to(root/'source')):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((root/'source').rglob('*')) if p.is_file()}
            protocol={'measured_at_utc':datetime.now(timezone.utc).isoformat(),'settings':'inherited defaults; no --model, reasoning override or --ignore-user-config',
                      'configured_defaults':defaults,'tasks':TASKS,'repetitions_per_method':args.repetitions,'max_model_calls':len(TASKS)*2*args.repetitions,
                      'fresh_sessions':True,'cache_controlled':False,'source_hashes':hashes,'prompts':{task:{method:prompt(task,method) for method in ('gh','tools')} for task in TASKS},
                      'usage_source':'codex exec --json turn.completed.usage input + output; cached input included',
                      'limitations':['Small synthetic fixtures, not live GitHub writes or monetary savings.','One repetition is exploratory, not a reliable average.','No older CLI comparison; compares direct gh to the current CLI.','MCP/system-prompt and backend cache effects are not controlled.']}
            core.save(root/'protocol.json',protocol)
            records=[]
        else:
            protocol=json.loads((root/'protocol.json').read_text());records=json.loads((root/'results.json').read_text()) if (root/'results.json').exists() else []
            if defaults!=protocol['configured_defaults']:raise RuntimeError('default model settings changed; do not mix conditions')
        if args.repair_baselines:
            if not protocol.get('baseline_repair'):
                protocol['original_prompts'] = protocol['prompts']
                protocol['prompts'] = {task: {method: prompt(task,method) for method in ('gh','tools')} for task in TASKS}
                protocol['baseline_repair'] = {
                    'pr-create': 'Original gh guide unnecessarily required issue types; corrected to repository metadata and labels.',
                    'reviews': 'Original fixture only recognized named review queries and rejected valid anonymous GraphQL queries; fixed dispatch.'}
                protocol['max_model_calls'] += 2
                for record in records:
                    if record['method']=='gh' and record['task'] in protocol['baseline_repair']:
                        record.update(included=False,exclusion_reason=protocol['baseline_repair'][record['task']])
                core.save(root/'protocol.json',protocol)
                core.save(root/'results.json',records)
            for task in ('pr-create','reviews'):
                if any(r['task']==task and r['method']=='gh' and r.get('included',True) for r in records):continue
                index=max(r['index'] for r in records if r['task']==task)+1
                print(json.dumps({'event':'trial_started','task':task,'method':'gh','index':index}),flush=True)
                record=core.run_trial(root,[server,server],task,index,'gh',protocol['prompts'][task]['gh'],None,None)
                record.update(configured_defaults=defaults,included=True)
                records.append(record);core.save(root/'results.json',records)
                core.save(root/'report.json',report(protocol,records))
                print(json.dumps({'event':'trial_completed',**{k:record.get(k) for k in ('task','method','correct','usage','seconds','api_calls')}}),flush=True)
                if record['usage'] is None and record['command_calls']==0:raise RuntimeError('model unavailable or execution rejected; no further calls')
            return
        if args.run_models:
            existing={(r['task'],r['index'],r['method']) for r in records}
            for task in TASKS:
                order=['gh','tools'] if TASKS.index(task)%2==0 else ['tools','gh']
                for repetition in range(protocol['repetitions_per_method']):
                    for index,method in enumerate(order,start=1+2*repetition):
                        if (task,index,method) in existing:continue
                        print(json.dumps({'event':'trial_started','task':task,'method':method,'index':index}),flush=True)
                        record=core.run_trial(root,[server,server],task,index,method,protocol['prompts'][task][method],None,None)
                        record['configured_defaults']=defaults
                        records.append(record);core.save(root/'results.json',records)
                        core.save(root/'report.json',report(protocol,records))
                        print(json.dumps({'event':'trial_completed',**{k:record.get(k) for k in ('task','method','correct','usage','seconds','api_calls')}}),flush=True)
                        if record['usage'] is None and record['command_calls']==0:raise RuntimeError('model unavailable or execution rejected; retained events; no further calls')
        else:
            core.save(root/'report.json',{'protocol':protocol,'trials':[]})
    finally:
        server.shutdown();server.server_close()


if __name__=='__main__':
    main()
