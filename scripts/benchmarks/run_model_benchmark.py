#!/usr/bin/env python3
"""Nine README workloads, two configurable models, three trials per method.

All GitHub writes are local fixtures. Use a fixed production source directory to
prevent concurrent development from changing the CLI between models.
"""
import argparse
import hashlib
import json
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

import run_command_benchmark as core
import run_dependabot_benchmark as dep

TASKS = ['context', 'setup', 'dependabot', 'issue-plan', 'issue-create', 'issue-branch', 'pr-create', 'cleanup-preview', 'cleanup-apply']
MODELS = ['gpt-6-astra', 'gpt-6-luna']
COMMANDS = dict(core.COMMANDS, **{
    'dependabot': dep.COMMANDS,
    'issue-plan': [['tools', 'github', 'issue', 'create', '--prefix', 'fix', '--title', 'measure command workflow', '--body-file', 'body.md', '--no-checkout', '--dry-run', '--json']],
    'cleanup-apply': [['tools', 'github', 'branch', 'cleanup', '--apply', '--json']],
})
ORIGINAL = {name: getattr(core, name) for name in ('setup', 'expected', 'verify', 'schema', 'environment')}
PLAN_REFERENCE = None


def expected(task):
    if task == 'dependabot':
        return dep.REFERENCE
    if task == 'issue-plan':
        return PLAN_REFERENCE
    if task == 'cleanup-apply':
        return core.cleanup.EXPECTED
    return ORIGINAL['expected'](task)


def setup(root, server, task):
    global PLAN_REFERENCE
    before, fixture = ORIGINAL['setup'](root, server, 'cleanup-preview' if task == 'cleanup-apply' else task)
    if task == 'issue-plan':
        PLAN_REFERENCE = {'repo': core.REPO, 'title': core.TITLE, 'body': core.BODY, 'assignees': ['fixture-user'], 'labels': ['bug'],
                          'issue_type': 'Bug', 'default_branch': 'main', 'base_commit': before['remote']['refs/heads/main'],
                          'branch_pattern': '<issue-number>-fix-measure-command-workflow', 'template': None}
    return before, fixture


def verify(root, server, task, before):
    if task == 'dependabot':
        return dep.verify(root, server, task, before)
    if task == 'cleanup-apply':
        return core.cleanup.verify_state(root, server.fixture)
    value = ORIGINAL['verify'](root, server, 'context' if task == 'issue-plan' else task, before)
    if task in ('context', 'setup', 'issue-plan'):
        endpoints = {urlsplit(a['endpoint']).path for a in server.accesses if a['method'] == 'GET'}
        value['checks']['catalog_read'] = {'/repos/' + core.REPO, '/repos/' + core.REPO + '/labels', '/repos/' + core.REPO + '/issue-types'} <= endpoints
        value['correct'] = all(value['checks'].values())
    return value


def schema(value):
    if value is None:
        return {'type': 'null'}
    result = ORIGINAL['schema'](value)
    if value == dep.REFERENCE:
        result['properties']['alerts']['items']['properties']['first_patched_version'] = {'anyOf': [{'type': 'string'}, {'type': 'null'}]}
    return result


def environment(root, server, directory, task):
    return ORIGINAL['environment'](root, server, directory, 'cleanup-preview' if task == 'cleanup-apply' else task)


def prompt(task, method):
    if task == 'dependabot':
        return dep.prompt(method)
    if task == 'cleanup-apply':
        return core.cleanup.COMMON + '\nUse python3 for local scripts; python is not installed.\n' + core.cleanup.GUIDES[method]
    if task != 'issue-plan':
        return core.prompt_for(task, method)
    shared = core.SHARED + '\nPrepare a read-only issue and linked-branch plan for ' + core.REPO + '. Do not create anything or switch branches. Read body.md and use title fix: measure command workflow.\n'
    if method == 'tools':
        return shared + '\nRun: ' + subprocess.list2cmdline(COMMANDS[task][0]) + '''
Normalize plan.repo, payload.title/body/assignees/labels, selection.issue_type, branch.base/base_sha/name and template into repo,title,body,assignees,labels,issue_type,default_branch,base_commit,branch_pattern,template. Use null when no matching template exists. No prior context lookup is necessary. Do not invoke gh.
'''
    return shared + core.CATALOG_GUIDE + '''
Use gh directly; never invoke tools. Read gh api user, gh api repos/fixture/command-benchmark/git/ref/heads/main and Markdown templates with gh api graphql -f query='query { repository(owner:"fixture",name:"command-benchmark") { issueTemplates { filename body } } }'. Select the first existing label from fix,bug and enabled type from Bug,Task. Prefix the English title and trim body.md outside. Return repo,title,body,assignees,labels,issue_type,default_branch,base_commit,branch_pattern,template. Use <issue-number>-fix-measure-command-workflow for the future branch pattern and null for absent templates. Collections fit one page. Python 3 is available as python3, not python; batching and jq filters are allowed.
'''


def normalize(task, responses, server):
    last = responses[-1]
    if task == 'context':
        catalog = last['catalog']
        return {'repo': catalog['repository']['full_name'], 'default_branch': catalog['repository']['default_branch'],
                'labels': [v['name'] for v in catalog['labels']], 'issue_types': [v['name'] for v in catalog['issue_types'] if v.get('is_enabled', True)]}
    if task == 'setup':
        return last['plan']['config']
    if task == 'dependabot':
        return {'count': responses[0]['count'], 'alerts': [{key: a[key] for key in dep.FIELDS} for a in responses[0]['alerts']],
                'detail': {key: last['alert'][key] for key in dep.REFERENCE['detail']}}
    if task == 'issue-plan':
        p, payload = last['plan'], last['plan']['payload']
        return {'repo': p['repo'], **{k: payload[k] for k in ('title', 'body', 'assignees', 'labels')},
                'issue_type': p['selection'].get('issue_type'), 'default_branch': p['branch']['base'], 'base_commit': p['branch']['base_sha'],
                'branch_pattern': p['branch']['name'], 'template': p.get('template')}
    if task in ('issue-create', 'issue-branch'):
        return {'number': last['number'], 'branch': last['branch']['name'], 'linked': last['branch']['linked'], 'checked_out': last['branch']['checked_out']}
    if task == 'pr-create':
        result = {k: server.created_prs[0][k] for k in expected(task)}
        result['labels'] = [v['name'] for v in result['labels']]
        return result
    if task == 'cleanup-apply':
        return {'deleted_' + kind: sorted(v['name'] for v in last['actions'] if v['scope'] == kind and v['status'] == 'deleted')
                for kind in ('local', 'remote', 'tracking')} | {'kept_targets': last['summary']['skipped']}
    return {'candidate_' + kind: sorted(v['name'] for v in last['plan']['targets'] if v['scope'] == kind and v['eligible'])
            for kind in ('local', 'remote', 'tracking')} | {'kept_targets': last['summary']['kept']}


def summarize(records):
    return {model: {task: core.cleanup.summarize([r for r in records if r['model'] == model and r['task'] == task])
                    for task in TASKS} for model in dict.fromkeys(r['model'] for r in records)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--source-root', type=Path)
    parser.add_argument('--models', nargs='+', default=MODELS)
    parser.add_argument('--tasks', choices=TASKS, nargs='+', default=TASKS)
    parser.add_argument('--run-models', action='store_true')
    parser.add_argument('--resume', action='store_true')
    args = parser.parse_args()
    root = args.root.resolve()
    if root == Path('/tmp') or not root.is_relative_to(Path('/tmp')) or root.exists() and not args.resume:
        parser.error('use a fresh dedicated /tmp root, or --resume recorded trials')
    if args.resume and not (root / 'protocol.json').exists():
        parser.error('resume needs a saved protocol')
    repo_root = Path(__file__).resolve().parents[2]
    source = (args.source_root or repo_root).resolve()
    source_hashes = [{'sha256': hashlib.sha256(p.read_bytes()).hexdigest(), 'path': str(p.relative_to(source))}
                     for p in sorted((source / 'internal').rglob('*.go')) if not p.name.endswith('_test.go')]
    if not args.resume:
        root.mkdir(parents=True)
        core.cleanup.build_interface(root, source, build_vcs=False)
        wrapper = root / 'bin/git'
        wrapper.write_text(wrapper.read_text().replace("if 'push' in args:",
            "if 'fetch' in args:\n  args=[os.environ['BRANCH_BENCHMARK_REMOTE'] if a=='origin' else a for a in args]\n if 'push' in args:"))
    for name in ORIGINAL:
        setattr(core, name, globals()[name])
    servers = [dep.Backend(root), core.cleanup.Backend(root)]
    threads = [threading.Thread(target=s.serve_forever, daemon=True) for s in servers]
    for thread in threads:
        thread.start()
    try:
        preflights = []
        references = {}
        if not args.resume:
            for task in args.tasks:
                server = servers[task in ('cleanup-preview', 'cleanup-apply')]
                before, _ = setup(root, server, task)
                directory = root / 'preflight' / task
                directory.mkdir(parents=True)
                server.interface_access_path = directory / 'interface-access.jsonl'
                responses = []
                env = environment(root, server, directory, task)
                for command in COMMANDS[task]:
                    result = subprocess.run(['codex', 'sandbox', '--permission-profile', 'command_benchmark', *core.permission_args(root),
                        '--cd', str(root / 'workspace'), '--', *command], env=env, capture_output=True, text=True, timeout=90)
                    if result.returncode:
                        raise RuntimeError(f'{task}: {result.stderr}\n{result.stdout}')
                    responses.append(json.loads(result.stdout))
                verification = verify(root, server, task, before)
                actual = normalize(task, responses, server)
                assert actual == expected(task) and verification['correct'], (task, actual, verification)
                if task == 'issue-branch':
                    assert responses[-1]['branch']['reused']
                references[task] = expected(task)
                preflights.append({'task': task, 'correct': True, 'native_model_calls': 0, 'checks': verification['checks']})
                core.save(directory / 'results.json', {'responses': responses, 'verification': verification, 'actual': actual})
                print(json.dumps({'event': 'preflight_passed', 'task': task}), flush=True)
            core.save(root / 'preflight.json', preflights)
            protocol = {'measured_at_utc': datetime.now(timezone.utc).isoformat(), 'models': args.models, 'reasoning_effort': 'high',
                'tasks': args.tasks, 'order_per_task_and_model': core.ORDER, 'repetitions_per_method': 3,
                'max_model_calls': len(args.tasks) * len(args.models) * 6, 'fresh_sessions': True, 'cache_controlled': False, 'timeout_seconds': 300,
                'usage_source': 'codex exec --json turn.completed.usage; input + output (cached input included)',
                'source_hashes': source_hashes, 'build_vcs': False, 'cli_binary_sha256': hashlib.sha256((root / 'bin/tools').read_bytes()).hexdigest(),
                'runner_hashes': [{'sha256': hashlib.sha256(p.read_bytes()).hexdigest(), 'path': p.name} for p in
                    (Path(__file__), Path(core.__file__), Path(dep.__file__), Path(core.cleanup.__file__))],
                'source_manifest': json.loads((source / 'benchmark-source.json').read_text()) if (source / 'benchmark-source.json').exists() else None,
                'versions': {name: subprocess.check_output(command, text=True).strip() for name, command in
                    [('codex', ['codex', '--version']), ('gh', [core.cleanup.REAL_GH, '--version']), ('go', ['go', 'version']), ('git', [core.cleanup.REAL_GIT, '--version'])]},
                'expected': references, 'prompts': {task: {method: prompt(task, method) for method in ('gh', 'tools')} for task in args.tasks},
                'limitations': ['Small synthetic fixtures; not live GitHub writes or monetary savings.',
                    'Each task runs Astra then Luna, with interleaved methods; cache/time effects and model system instructions/tokenizers are uncontrolled.',
                    'Sol values are historical; earlier Sol dry-run used a different live repository fixture.',
                    'All attempts and failures retained; low token usage alone is not evidence of successful automation.']}
            core.save(root / 'protocol.json', protocol)
            records = []
        else:
            protocol = json.loads((root / 'protocol.json').read_text())
            if protocol['source_hashes'] != source_hashes:
                raise RuntimeError('source changed; use original frozen source for resume')
            args.models, args.tasks = protocol['models'], protocol['tasks']
            records = json.loads((root / 'results.json').read_text())
        existing = {(r['model'], r['task'], r['index'], r['method']) for r in records}
        if args.run_models:
            for task in args.tasks:
                for model in args.models:
                    for index, method in enumerate(core.ORDER, 1):
                        if (model, task, index, method) in existing:
                            continue
                        print(json.dumps({'event': 'trial_started', 'model': model, 'task': task, 'index': index, 'method': method}), flush=True)
                        record = core.run_trial(root, servers, task, index, method, protocol['prompts'][task][method], model, 'high')
                        state_file = root / 'runs' / task / f'{index:02d}-{method}' / 'state-verification.json'
                        record['state_verification'] = json.loads(state_file.read_text())
                        record['prompt_sha256'] = hashlib.sha256(protocol['prompts'][task][method].encode()).hexdigest()
                        # Each model keeps its own transcript directory after collection.
                        current = root / 'runs' / task / f'{index:02d}-{method}'
                        target = root / 'models' / model / task / current.name
                        target.parent.mkdir(parents=True, exist_ok=True)
                        current.rename(target)
                        events = [json.loads(line) for line in (target / 'events.jsonl').read_text().splitlines() if line.strip()]
                        record['error_events'] = [e for e in events if e.get('type') in ('error', 'turn.failed')]
                        records.append(record)
                        core.save(root / 'results.json', records)
                        core.save(root / 'summary.json', summarize(records))
                        print(json.dumps({'event': 'trial_completed', **{key: record.get(key) for key in ('model', 'task', 'index', 'method', 'correct', 'usage', 'seconds')}}), flush=True)
                        if record['usage'] is None and record['command_calls'] == 0:
                            raise RuntimeError(f'{model}: model execution failed before any task command; see retained error events')
            report = {'protocol': protocol, 'preflight': json.loads((root / 'preflight.json').read_text()), 'summary': summarize(records),
                      'trials': records, 'correctness': {'passed': sum(r['correct'] for r in records), 'total': len(records)}}
            core.save(root / 'report.json', report)
            print(json.dumps({'event': 'experiment_completed', 'correctness': report['correctness']}), flush=True)
    finally:
        for server in servers:
            server.shutdown()
            server.server_close()
        for thread in threads:
            thread.join(timeout=5)


if __name__ == '__main__':
    main()
