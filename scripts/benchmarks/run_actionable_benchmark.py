#!/usr/bin/env python3
"""Frozen completion study for the four actionable-output improvements.

No models are called unless --run-models is given. Direct processing permits
natural batching and filtering. Model/effort defaults are never overridden.
"""
import argparse
import copy
import hashlib
import json
import os
import random
import shutil
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

import run_command_benchmark as core
import run_default_benchmark as small
import run_github_study as gh
from github_graphql_fixture import FixtureGraphQL

TASKS = ['apply-errors', 'apply-groups', 'pr-reviews', 'pr-inspect']
REPETITIONS = 3
SEED = 20261010
ACTIVE_REPETITION = 1
REFERENCES = {}
BASE_ENVIRONMENT = core.environment


def digest(data):
    return hashlib.sha256(data).hexdigest()


def inventory(work):
    return {str(p.relative_to(work)): {'sha256': digest(p.read_bytes()), 'mode': p.stat().st_mode & 0o777}
            for p in sorted(work.rglob('*')) if p.is_file() and '.git' not in p.relative_to(work).parts
            and '.tools' not in p.relative_to(work).parts}


class ActionGraphQL(FixtureGraphQL):
    def threads(self):
        return copy.deepcopy(self.server.action_threads)

    def node(self, identifier):
        for thread in self.threads():
            if thread['id'] == identifier:
                return thread
            for comment in thread['comment_nodes']:
                if comment['id'] == identifier:
                    return comment
        return super().node(identifier)


class Handler(gh.Handler):
    def respond(self):
        path = urlsplit(self.path).path
        if '/commits/' in path and path.endswith('/check-runs'):
            value = {'check_runs': [{'id': 91, 'name': 'unit', 'status': 'completed', 'conclusion': 'success',
                                     'app': {'slug': 'external-ci'}, 'details_url': ''}]}
            data = json.dumps(value).encode()
            self.server.accesses.append({'method': self.command, 'endpoint': self.path, 'payload': {}, 'status': 200})
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        return super().respond()


def environment(root, server, directory, task):
    return BASE_ENVIRONMENT(root, server, directory, 'pr-reviews')


def call(root, server, directory, command, body=None, before_cli=None):
    args = list(command)
    if before_cli:
        args[0] = str(before_cli)
    run = subprocess.run(['codex', 'sandbox', '--permission-profile', 'command_benchmark', *core.permission_args(root),
                          '--cd', str(root/'workspace'), '--', *args], cwd=root/'workspace',
                         env=environment(root, server, directory, ''), input=body, capture_output=True,
                         text=True, encoding='utf-8', timeout=90)
    value = json.loads(run.stdout)
    if run.returncode and value.get('status') not in ('error', 'blocked'):
        raise RuntimeError((run.returncode, run.stderr, value))
    return value


def setup(root, server, task):
    gh.setup(root, server, 'pr-reviews')
    work = root/'workspace'
    server.action_threads = []
    thread = ActionGraphQL(server).comment(1)
    request = 'Please review this carefully. ' * (60 if ACTIVE_REPETITION != 3 else 2)
    request += 'Set retry_budget=9 in settings.py; preserve timeout=30 and original line endings.'
    thread['body'] = request
    thread['diffHunk'] = '@@ -1,2 +1,2 @@\n retry_budget=2\n timeout=30\n' + '+ context\n' * 180
    server.action_threads = [{'__typename': 'PullRequestReviewThread', 'id': 'T1', 'path': 'settings.py', 'line': 1,
        'startLine': None, 'originalLine': 1, 'originalStartLine': None, 'diffSide': 'RIGHT', 'startDiffSide': None,
        'isResolved': False, 'isOutdated': False, 'comment_nodes': [thread]}]
    server.graphql = ActionGraphQL(server)
    (work/'settings.py').write_bytes(b'retry_budget=2\r\ntimeout=30\r\n')
    (work/'other-work.txt').write_text('unrelated completed work\n', encoding='utf-8')
    paths = [f'src/config_{i:02d}.txt' for i in range(12)]
    if task.startswith('apply'):
        (work/'src').mkdir()
        for i, name in enumerate(paths):
            eol = '\r\n' if i % 2 else '\n'
            content = eol.join(['cache_limit=8', 'retry_budget=2', 'keep=unchanged']) + eol
            if task == 'apply-errors':
                content = ('marker\n' * (i % 3)) + 'keep=unchanged\n'
            (work/name).write_bytes(content.encode())
            (work/name).chmod(0o600 if i % 2 else 0o640)
        if task == 'apply-errors':
            spec = {'version': 1, 'files': [{'path': p, 'replacements': [{'old': 'marker', 'new': 'updated', 'count': 1}]} for p in paths]}
            (work/'invalid-spec.json').write_text(json.dumps(spec, ensure_ascii=False), encoding='utf-8')
            REFERENCES[task] = {'diagnostics': [{'path': p, 'replacement_index': 1, 'expected_count': 1, 'actual_count': i % 3}
                                               for i, p in enumerate(paths) if i % 3 != 1], 'writes': 0}
        else:
            REFERENCES[task] = {'applied': paths, 'verified': paths, 'cache_limit': 64, 'retry_budget': 5,
                                'saved_plan_verified': True, 'unrelated_preserved': True}
    else:
        budget = 9
        if task == 'pr-inspect':
            directory = root/'seed'/str(ACTIVE_REPETITION)
            directory.mkdir(parents=True, exist_ok=True)
            seed = call(root, server, directory, ['tools', 'github', 'pr', 'inspect', '--number', '7', '--repo', core.REPO,
                        '--state-file', '.tools/pr-state.json', '--compact=false', '--json'])
            assert seed['complete'] and seed['status'] == 'blocked'
            # A supplied baseline represents earlier work. Its original creation
            # is intentionally outside the return-workflow measurement.
            if ACTIVE_REPETITION == 2:
                reply = copy.deepcopy(thread)
                reply.update(id='C2', body='Correction: set retry_budget=11 instead; preserve timeout=30.',
                             updatedAt='2026-10-08T00:00:00Z')
                server.action_threads[0]['comment_nodes'].append(reply)
                budget = 11
            if ACTIVE_REPETITION == 3:
                server.action_threads[0]['isResolved'] = True
                budget = 2
        REFERENCES[task] = {'head': server.head, 'unresolved_threads': 0 if budget == 2 else 1,
                            'retry_budget': budget, 'timeout': 30, 'verified': True, 'unrelated_preserved': True}
    server.accesses = []
    return inventory(work), None


def expected(task):
    return REFERENCES[task]


def valid_authored_spec(value, paths):
    """Input authoring is authorized; its filename is not prescribed."""
    try:
        if type(value.get('version')) is not int or value['version'] != 1 or set(value) - {'version', 'files', 'groups'}:
            return False
        edits = list(value.get('files', []))
        for group in value.get('groups', []):
            if set(group) != {'paths', 'replacements'}:
                return False
            edits.extend({'path': name, 'replacements': group['replacements']} for name in group['paths'])
        return sorted(e['path'] for e in edits) == paths and all(set(e) == {'path', 'replacements'} and e['replacements'] == replacements()
            and all(type(r['count']) is int for r in e['replacements']) for e in edits)
    except (AttributeError, KeyError, TypeError, ValueError):
        return False


def verify(root, server, task, before):
    work = root/'workspace'
    after = inventory(work)
    changed = {'settings.py'} if task.startswith('pr-') else set(expected(task).get('applied', []))
    allowed_new = {'plan.json', 'spec.json'} if task == 'apply-groups' else set()
    if task == 'apply-groups':
        for name in set(after) - set(before) - allowed_new:
            try:
                value = json.loads((work/name).read_text(encoding='utf-8'))
                if valid_authored_spec(value, expected(task)['applied']):
                    allowed_new.add(name)
            except (OSError, UnicodeError, ValueError):
                pass
    checks = {'unrelated_bytes_and_modes': all(after.get(p) == v for p, v in before.items() if p not in changed),
              'no_unexpected_files': set(after) - set(before) <= allowed_new,
              'no_api_mutations': not server.reruns and not server.merge_requests and not server.created_prs and not server.created_issues,
              'no_apply_temporaries': not any(p.name.startswith('.tools-fs-') for p in work.rglob('*'))}
    if task == 'apply-errors':
        checks['no_source_writes'] = after == before
    elif task == 'apply-groups':
        valid = True
        for i, name in enumerate(expected(task)['applied']):
            eol = '\r\n' if i % 2 else '\n'
            wanted = eol.join(['cache_limit=64', 'retry_budget=5', 'keep=unchanged']) + eol
            valid &= (work/name).read_bytes() == wanted.encode() and after[name]['mode'] == before[name]['mode']
        try:
            plan = json.loads((work/'plan.json').read_text(encoding='utf-8'))
            valid &= set(plan) == {'version', 'files'} and plan['version'] == 1 and len(plan['files']) == 12
            valid &= sorted(row['path'] for row in plan['files']) == expected(task)['applied']
            for row in plan['files']:
                valid &= set(row) == {'path', 'sha256', 'replacements'} and row['sha256'] == before[row['path']]['sha256'] and row['replacements'] == replacements()
        except (OSError, ValueError, KeyError):
            valid = False
        checks['exact_files_permissions_and_saved_preimages'] = bool(valid)
    else:
        wanted = f"retry_budget={expected(task)['retry_budget']}\r\ntimeout=30\r\n".encode()
        checks['exact_requested_edit_and_readback'] = (work/'settings.py').read_bytes() == wanted and after['settings.py']['mode'] == before['settings.py']['mode']
        checks['current_review_metadata_observed'] = any('reviewThreads(' in a.get('payload', {}).get('query', '') for a in server.accesses)
        if task == 'pr-reviews':
            checks['review_body_and_diff_observed'] = any('body' in a.get('payload', {}).get('query', '') and 'diffHunk' in a.get('payload', {}).get('query', '') for a in server.accesses)
    return {'correct': all(checks.values()), 'checks': checks, 'after': after}


def replacements():
    return [{'old': 'cache_limit=8', 'new': 'cache_limit=64', 'count': 1},
            {'old': 'retry_budget=2', 'new': 'retry_budget=5', 'count': 1}]


REVIEW_QUERY = '''query {repository(owner:"fixture",name:"command-benchmark"){pullRequest(number:7){headRefOid reviewThreads(first:100){nodes{id path line isResolved isOutdated comments(first:100){nodes{id body diffHunk updatedAt} pageInfo{hasNextPage endCursor}}} pageInfo{hasNextPage endCursor}}}}}'''


def prompt(task, method):
    common = ('Complete only this synthetic task. Preserve unrelated bytes, permissions, line endings and user inputs. '
              'No other agents, model calls, installs, external network, real GitHub mutations or reading benchmark '
              'source/protocol/control/results/other sessions. Python is python3. Batch/filter naturally; no minimum '
              'command count. Authoring, mapping, retries and final verification are included. Return only the output schema.\n')
    tasks = {
        'apply-errors': 'Validate every file in invalid-spec.json and report ALL independently observable replacement-count errors, sorted by path. Do not correct counts or write source, input or plan files. Counts within a file are sequential: stop diagnosing that file after its first invalid replacement. Replacement indexes start at 1. Validation failure is expected.\n',
        'apply-groups': 'For src/config_00.txt through src/config_11.txt, replace cache_limit=8 with cache_limit=64 and retry_budget=2 with retry_budget=5, each exactly once. Author the input/script yourself. Before writes validate ALL relative paths/no symlinks/UTF-8/exact counts, capture original SHA-256 and save exclusive plan.json with EXACT schema {version:1,files:[{path,sha256,replacements:[{old,new,count},...]}]}. Validate saved bytes before source writes and again afterward. Preserve existing permissions and bytes outside replacements; read back actual files and verify intended bytes. Return resulting values and paths from verified data. This complete batch is authorized; no separate preview required.\n',
        'pr-reviews': 'Read PR 7 current unresolved review requests including exact body, coordinates and diff. Follow the latest request in each unresolved thread to fix settings.py; preserve timeout and CRLF. Verify actual final bytes and current PR head. Do not rely on a summary or truncated body. Do not mutate GitHub.\n',
        'pr-inspect': 'Return after unrelated work with an existing .tools/pr-state.json baseline but NO prior review text in this session. Inspect PR 7 current unresolved work and follow the latest request to fix settings.py, preserving timeout and CRLF; if no unresolved thread remains leave it unchanged. Verify actual final bytes and current PR head. Do not interpret unchanged as no outstanding work. Do not mutate GitHub.\n'}
    if method == 'tools':
        guides = {
            'apply-errors': 'Use tools fs apply --spec invalid-spec.json --json (exit 1 expected). diagnostics contains every independent file error; diagnostic is the compatible first error. No source or saved plan is written on failure.\n',
            'apply-groups': 'Use tools fs apply --spec FILE_OR_STDIN --save-plan plan.json --apply --report-changes --json. Shared input: {version:1,groups:[{paths:[...],replacements:[{old,new,count},...]}]}. The CLI expands and verifies every file/hash/count before writes, stores canonical plan files, and reads back files and plan. Require complete, report_complete, each file.verified and saved_plan.verified. Derive values from exact returned ranges; no separate plan/file dump needed.\n',
            'pr-reviews': f'Use tools github pr reviews --repo {core.REPO} --number 7 --compact --json. Body and diff_hunk are exact; threads have current coordinates and comments. A local script may read this result, make the authorized edit and verify it in one batch.\n',
            'pr-inspect': f'Use tools github pr inspect --repo {core.REPO} --number 7 --state-file .tools/pr-state.json --json. Even unchanged/changed includes outstanding current threads with exact comments/body/diff and PR identity when work remains. pr_status and attention_required describe current status. A local script may process/edit/verify in one batch.\n'}
        return common + tasks[task] + 'Method: production tools CLI; local scripts may author inputs and process results without reimplementing validation or GitHub collection.\n' + guides[task]
    guide = ('Method: direct shell/rg/Python standard library; never invoke tools or import its implementation. '
             'Implement the same requested validations, edits and verification; batching/filtering allowed.\n')
    if task.startswith('pr-'):
        guide += f'Use gh api graphql -f query={json.dumps(REVIEW_QUERY)} and gh api repos/{core.REPO}/pulls/7 to verify current head after collection. GraphQL selected fields, aliases/fragments and --jq work; all collections fit one page. Sort unresolved threads, follow their latest comments; bodies are exact originals. Existing state does not substitute for current evidence.\n'
    return common + tasks[task] + guide


def preflight(root, server, before_cli):
    records = []
    for repetition in range(1, 4):
        global ACTIVE_REPETITION
        ACTIVE_REPETITION = repetition
        for task in TASKS:
            before, _ = setup(root, server, task)
            directory = root/'preflight'/task/str(repetition)
            directory.mkdir(parents=True)
            if task == 'apply-errors':
                value = call(root, server, directory, ['tools', 'fs', 'apply', '--spec', 'invalid-spec.json', '--json'])
                actual = {'diagnostics': [{k: row[k] for k in ('path', 'replacement_index', 'expected_count', 'actual_count')}
                                          for row in value['diagnostics']], 'writes': 0}
                if before_cli:
                    spec = json.loads((root/'workspace/invalid-spec.json').read_text())
                    old_errors = []
                    old_calls = 0
                    remaining = list(spec['files'])
                    while remaining:
                        old = call(root, server, directory, ['tools', 'fs', 'apply', '--spec', '-', '--json'],
                                   json.dumps({'version': 1, 'files': remaining}), before_cli)
                        old_calls += 1
                        if 'diagnostic' not in old:
                            assert old['complete']
                            break
                        old_errors.append({k: old['diagnostic'][k] for k in ('path', 'replacement_index', 'expected_count', 'actual_count')})
                        remaining = [edit for edit in remaining if edit['path'] != old['diagnostic']['path']]
                    assert old_errors == actual['diagnostics']
                    value['before_cli_calls'] = old_calls
                    value['after_cli_calls'] = 1
            elif task == 'apply-groups':
                paths = expected(task)['applied']
                spec = {'version': 1, 'groups': [{'paths': paths, 'replacements': replacements()}]}
                expanded = {'version': 1, 'files': [{'path': p, 'replacements': replacements()} for p in paths]}
                value = call(root, server, directory, ['tools', 'fs', 'apply', '--spec', '-', '--save-plan', 'plan.json', '--apply', '--report-changes', '--json'], json.dumps(spec))
                assert value['complete'] and value['report_complete'] and value['saved_plan']['verified']
                assert len(value['files']) == 12 and all(r['verified'] for r in value['files'])
                value['grouped_input_bytes'] = len(json.dumps(spec, separators=(',', ':')).encode())
                value['expanded_input_bytes'] = len(json.dumps(expanded, separators=(',', ':')).encode())
                actual = expected(task)
            else:
                command = ['tools', 'github', 'pr', 'reviews', '--repo', core.REPO, '--number', '7', '--compact', '--json'] if task == 'pr-reviews' else ['tools', 'github', 'pr', 'inspect', '--repo', core.REPO, '--number', '7', '--state-file', '.tools/pr-state.json', '--json']
                value = call(root, server, directory, command)
                assert value['complete']
                threads = value['threads'] if task == 'pr-reviews' else value.get('outstanding', {}).get('threads', [])
                wanted_threads = [t for t in server.action_threads if not t['isResolved']]
                assert len(threads) == len(wanted_threads)
                for thread, original in zip(threads, wanted_threads):
                    assert thread['line'] == original['line']
                    for comment, source in zip(thread['comments'], original['comment_nodes']):
                        assert comment['body'] == source['body'] and comment['diff_hunk'] == source['diffHunk']
                if before_cli:
                    old = call(root, server, directory, command, before_cli=before_cli)
                    if task == 'pr-reviews':
                        old_thread = old.get('threads', [])[0]
                        value['before_needed_artifact'] = any(c.get('body_truncated') or 'diff_hunk' not in c for c in old_thread['comments'])
                    elif expected(task)['unresolved_threads']:
                        value['before_missing_current_thread'] = 'outstanding' not in old
                (root/'workspace/settings.py').write_bytes(f"retry_budget={expected(task)['retry_budget']}\r\ntimeout=30\r\n".encode())
                actual = expected(task)
            verification = verify(root, server, task, before)
            assert actual == expected(task) and verification['correct'], (task, actual, verification)
            core.save(directory/'native.json', {'response': value, 'actual': actual, 'verification': verification})
            records.append({'task': task, 'repetition': repetition, 'correct': True, 'model_calls': 0})
            print(json.dumps({'event': 'preflight_passed', **records[-1]}), flush=True)
    return records


def schedule():
    rng = random.Random(SEED)
    steps = []
    for repetition in range(1, 4):
        tasks = list(TASKS)
        rng.shuffle(tasks)
        for task in tasks:
            methods = ['direct', 'tools'] if (repetition + TASKS.index(task)) % 2 else ['tools', 'direct']
            steps.extend({'task': task, 'method': m, 'repetition': repetition} for m in methods)
    return steps


def summarize(records):
    result = gh.summarize([dict(r, method='gh' if r['method'] == 'direct' else 'tools') for r in records], TASKS)
    for summary in result.values():
        summary['direct'] = summary.pop('gh')
    return result


def save_report(root, protocol, records):
    core.save(root/'results.json', records)
    core.save(root/'report.json', {'protocol': protocol, 'trials': records, 'summary': summarize(records)})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--before-cli', type=Path)
    parser.add_argument('--resume', action='store_true')
    parser.add_argument('--run-models', action='store_true')
    args = parser.parse_args()
    root = args.root.resolve()
    if root == Path('/tmp') or not root.is_relative_to('/tmp') or root.exists() and not args.resume:
        parser.error('use a fresh dedicated /tmp root, or --resume')
    repo = Path(__file__).resolve().parents[2]
    if not args.resume:
        root.mkdir()
        source = root/'source'
        source.mkdir()
        for name in ('go.mod', 'go.sum'):
            shutil.copy2(repo/name, source/name)
        for name in ('cmd', 'internal'):
            shutil.copytree(repo/name, source/name)
        small.build(root, source)
        overlay = root/'main-overlay.go'
        text = overlay.read_text()
        text = text.replace('ctx:=context.Background(); api,err', 'ctx:=context.Background(); if len(os.Args)>1 && os.Args[1]=="fs" {os.Exit(cli.Run(ctx,os.Args[1:],os.Stdin,os.Stdout,os.Stderr))}; api,err')
        overlay.write_text(text)
        subprocess.run(['go', 'build', '-buildvcs=false', '-overlay', str(root/'overlay.json'), '-o', str(root/'bin/tools'), './cmd/tools'], cwd=source, check=True)
        harness = root/'harness'
        harness.mkdir()
        for path in Path(__file__).parent.glob('*.py'):
            shutil.copy2(path, harness/path.name)
        shutil.copy2(Path(__file__).parent/'requirements.txt', harness/'requirements.txt')
    server = core.Backend(root)
    server.RequestHandlerClass = Handler
    threading.Thread(target=server.serve_forever, daemon=True).start()
    core.setup, core.verify, core.expected, core.schema, core.environment = setup, verify, expected, gh.schema, environment
    try:
        defaults = small.configured_defaults()
        if not args.resume:
            checks = preflight(root, server, args.before_cli.resolve() if args.before_cli else None)
            protocol = {'version': 1, 'measured_at_utc': datetime.now(timezone.utc).isoformat(), 'tasks': TASKS,
                'configured_defaults': defaults, 'settings': 'Inherited defaults; no model/effort override or ignore-user-config',
                'repetitions_per_method': 3, 'max_model_calls': 24, 'seed': SEED, 'schedule': schedule(),
                'source_hashes': gh.source_hashes(root/'source'), 'runner_hashes': gh.source_hashes(root/'harness'),
                'cli_sha256': digest((root/'bin/tools').read_bytes()), 'overlay_sha256': digest((root/'main-overlay.go').read_bytes()),
                'before_cli_sha256': digest(args.before_cli.read_bytes()) if args.before_cli else None,
                'preflights': checks, 'cache_controlled': False, 'fresh_sessions': True,
                'primary_metric': 'input_tokens + output_tokens', 'secondary_metric': 'input_tokens - cached_input_tokens + output_tokens',
                'prompts': {t: {m: prompt(t, m) for m in ('direct', 'tools')} for t in TASKS},
                'inclusion_policy': 'All scheduled attempts, including failures; no replacement runs. Stop on missing usage/quota/fixture defects.',
                'limitations': ['Four fixed synthetic workloads, three pairs each; not a production guarantee.',
                    'PR inspect pairs use unchanged, new-reply and resolved return fixtures, not an actual intervening model task.',
                    'Provided PR baseline creation is outside the return-workflow metric; no claim of first-baseline savings.',
                    'Model input cache is uncontrolled; fresh sessions are not proven cold.',
                    'Linux runtime with real CLI/Git/gh and local GraphQL/REST; no live GitHub latency or other-OS runtime proof.',
                    'Before-CLI probes are functional/output checks, not model-token or agent-time measurements.']}
            core.save(root/'protocol.json', protocol)
            records = []
            save_report(root, protocol, records)
        else:
            protocol = json.loads((root/'protocol.json').read_text())
            records = json.loads((root/'results.json').read_text())
            if defaults != protocol['configured_defaults']:
                raise RuntimeError('user defaults changed')
        gh.validate_frozen(root, protocol)
        if digest((root/'main-overlay.go').read_bytes()) != protocol['overlay_sha256']:
            raise RuntimeError('transport overlay changed')
        if args.run_models:
            for step in protocol['schedule']:
                if any(all(r[k] == v for k, v in step.items()) for r in records):
                    continue
                global ACTIVE_REPETITION
                ACTIVE_REPETITION = step['repetition']
                gh.validate_frozen(root, protocol)
                print(json.dumps({'event': 'trial_started', **step}), flush=True)
                record = core.run_trial(root, [server, server], step['task'], 1 + sum(r['task'] == step['task'] for r in records),
                                        step['method'], protocol['prompts'][step['task']][step['method']], None, None)
                record.update(repetition=step['repetition'])
                if record['usage']:
                    u = record['usage']
                    record['uncached_plus_output'] = u['input_tokens'] - u['cached_input_tokens'] + u['output_tokens']
                records.append(record)
                save_report(root, protocol, records)
                print(json.dumps({'event': 'trial_completed', **{k: record.get(k) for k in ('task', 'method', 'repetition', 'correct', 'input_plus_output', 'uncached_plus_output', 'command_calls', 'seconds')}}), flush=True)
                if record['usage'] is None:
                    raise RuntimeError('missing usage/quota; evidence retained, stopping without replacement')
    finally:
        server.shutdown()
        server.server_close()


if __name__ == '__main__':
    main()
