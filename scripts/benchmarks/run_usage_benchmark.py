#!/usr/bin/env python3
"""사용 기록에서 찾은 작업의 완료 비교. 모델은 명시한 옵션으로만 실행한다.

직접 처리의 GraphQL 배치도 허용하며 CLI의 REST 구조를 강제하지 않는다.
기본 설정을 상속하고 모든 시도와 부분 결과·오류 시나리오를 보존한다.
"""
import argparse
import copy
import hashlib
import io
import json
import os
import random
import shutil
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import parse_qs, urlsplit
from graphql import build_schema, parse
import run_actionable_benchmark as action
import run_command_benchmark as core
import run_default_benchmark as small
import run_github_study as gh
from github_graphql_fixture import SDL, connection
TASKS = ['pr-reviews', 'pr-conversation', 'junit', 'markdown', 'fs-file']
REPETITION = 1
ORIGINAL_ENVIRONMENT = action.environment
REFERENCES = {}

def sha(data):
    return hashlib.sha256(data).hexdigest()

class GraphQL(action.ActionGraphQL):

    def __init__(self, server):
        super().__init__(server)
        extra = 'extend type PullRequest { comments(first:Int,after:String):IssueCommentConnection! }\n        type IssueCommentConnection {nodes:[IssueComment!]! pageInfo:PageInfo!}\n        type IssueComment implements Node {id:ID! fullDatabaseId:BigInt body:String! author:Actor url:String createdAt:String updatedAt:String}\n        '
        old_schema = self.schema
        self.schema = build_schema(SDL + extra)
        for name, kind in old_schema.type_map.items():
            if name not in self.schema.type_map or not hasattr(kind, 'fields'):
                continue
            for field, value in kind.fields.items():
                if hasattr(value, 'resolve'):
                    self.schema.get_type(name).fields[field].resolve = value.resolve
        self.schema.get_type('PullRequest').fields['comments'].resolve = lambda _, info, first=None, after=None: connection([dict(c, id='IC' + str(c['id']), fullDatabaseId=str(c['id']), author=c['user'], url=c['html_url'], createdAt=c['created_at'], updatedAt=c['updated_at']) for c in server.conversation], first, after)
        self.schema.get_type('PullRequest').fields['reviews'].resolve = lambda _, info, first=None, after=None: connection([dict(c, id='R' + str(c['id']), fullDatabaseId=str(c['id']), author=c['user'], url=c['html_url'], commit={'oid': c['commit_id']}, submittedAt=c['submitted_at']) for c in server.history], first, after)

class Handler(action.Handler):

    def respond(self):
        path = urlsplit(self.path).path
        if path == '/graphql' or path.endswith('/issues/7/comments') or path.endswith('/pulls/7/reviews') or path.endswith('/pulls/7'):
            payload = json.loads(self.rfile.read(int(self.headers.get('Content-Length', 0))) or '{}')
            if path == '/graphql':
                if not payload and urlsplit(self.path).query:
                    payload = {key: values[0] for key, values in parse_qs(urlsplit(self.path).query).items()}
                value = self.server.graphql.execute(payload)
            elif path.endswith('/pulls/7'):
                value = {'number': 7, 'head': {'sha': self.server.head}}
            else:
                value = self.server.conversation if path.endswith('/comments') else self.server.history
            raw = json.dumps(value, ensure_ascii=False).encode()
            settings = self.server.root / 'workspace/settings.py'
            self.server.accesses.append({'method': self.command, 'endpoint': self.path, 'payload': payload, 'status': 200, 'response_bytes': len(raw), 'graphql_errors': value.get('errors', []) if isinstance(value, dict) else [], 'settings_sha256': sha(settings.read_bytes()) if settings.exists() else None})
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
            return
        return super().respond()

def environment(root, server, directory, task):
    env = ORIGINAL_ENVIRONMENT(root, server, directory, task)
    env['USAGE_BENCHMARK_CLI_LOG'] = str(directory / 'cli-access.jsonl')
    return env

def markdown_fixture(repetition):
    files, expected = ({}, [])
    for index in range(2):
        lines, headings = ([], [])
        ending = '\r\n' if index == 0 else '\n'

        def text(value):
            lines.extend(value.split('\n'))

        def heading(title, level, setext=False):
            start = len(lines) + 1
            text(title + '\n' + '-' * 6 if setext else '#' * level + ' ' + title)
            headings.append({'title': title, 'level': level, 'start': start, 'end': 0, 'occurrence': 1 + sum((h['title'] == title for h in headings))})
        heading(('Order', 'Delivery')[index], 1)
        text('Overview.\n\n' + '\n'.join(('Unrelated overview line ' + str(i) for i in range(45))))
        text('')
        heading('Build', 2, setext=index == 1 and repetition == 3)
        text('Run the module build.\n```sh\n# fake heading\n## Build\n```')
        heading('Verify', 3)
        text('Run exact tests; preserve this section.\n' + '\n'.join(('verify ' + str(i) for i in range(10))))
        occurrence = 1
        if repetition == 2:
            heading('Build', 2)
            text('Second build section.\n~~~text\n# still not a heading\n~~~')
            heading('Verify', 3)
            text('Only this occurrence is requested.')
            occurrence = 2
        heading('Boundary', 2)
        text('Other responsibilities.\n' + '\n'.join(('Unrelated tail ' + str(i) for i in range(40))))
        for i, h in enumerate(headings):
            h['end'] = next((other['start'] - 1 for other in headings[i + 1:] if other['level'] <= h['level']), len(lines))
        target = next((h for h in headings if h['title'] == 'Build' and h['occurrence'] == occurrence))
        name = ('order', 'delivery')[index] + '/README.md'
        body = ending.join(lines) + ('' if repetition == 3 else ending)
        raw_lines = body.splitlines(keepends=True)
        files[name] = body.encode()
        expected.append({'path': name, 'sha256': sha(files[name]), 'headings': headings, 'section': {'start': target['start'], 'text': ''.join(raw_lines[target['start'] - 1:target['end']])}})
    return (files, {'files': sorted(expected, key=lambda r: r['path'])}, occurrence)

def junit_fixture(repetition):
    detail = 'expected <2>, got <3>\n' + 'at example.Check.run(Check.java:12)\n' * 8
    case = '<testcase name="mismatch" classname="Check"><failure message="mismatch" type="AssertionError"><![CDATA[' + detail + ']]></failure></testcase>'
    test = '<testsuite name="unit" tests="3" failures="1" errors="0" skipped="1"><testcase name="ok"/>' + case + '<testcase name="disabled"><skipped message="explicit skip"/></testcase></testsuite>'
    accept = '<testsuite name="acceptance" tests="1" failures="0" errors="0" skipped="0"><testcase name="contract"/></testsuite>'
    if repetition == 2:
        test = '<testsuites tests="3" failures="1" errors="0" skipped="1">' + test + '</testsuites>'
    if repetition == 3:
        test = '<testsuite name="wrapper" tests="3" failures="1" errors="0" skipped="1">' + test + '</testsuite>'
    files = {'reports/test/TEST-unit.xml': test.encode(), 'reports/acceptanceTest/TEST-acceptance.xml': accept.encode()}
    problems = []
    if repetition == 3:
        files['reports/test/TEST-broken.xml'] = b'<testsuite tests="7"><testcase'
        problems = ['reports/test/TEST-broken.xml']
    suite = 'wrapper/unit' if repetition == 3 else 'unit'
    return (files, {'tests': 4, 'failures': 1, 'errors': 0, 'skipped': 1, 'report_count': 2, 'complete': not problems, 'execution_verified': False, 'problems': problems, 'diagnostics': [{'path': 'reports/test/TEST-unit.xml', 'suite': suite, 'name': 'mismatch', 'kind': 'failure', 'message': 'mismatch', 'type': 'AssertionError', 'details': detail}, {'path': 'reports/test/TEST-unit.xml', 'suite': suite, 'name': 'disabled', 'kind': 'skipped', 'message': 'explicit skip', 'type': '', 'details': ''}]})

def setup(root, server, task):
    action.ACTIVE_REPETITION = REPETITION
    action.setup(root, server, 'pr-reviews')
    work = root / 'workspace'
    server.conversation = [{'id': i, 'user': None if i == 2 else {'login': 'reviewer'}, 'body': body, 'html_url': f'https://github.com/{core.REPO}/pull/7#issuecomment-{i}', 'created_at': '2026-10-10T00:00:00Z', 'updated_at': '2026-10-10T01:00:00Z'} for i, body in enumerate(['Please see the inline request.', 'Review execution was limited; verify inline requests and CI separately.'], 1)]
    server.history = [{'id': i, 'state': state, 'body': 'submitted request' if i == 1 else 'unsubmitted', 'user': {'login': 'reviewer'}, 'commit_id': server.head, 'submitted_at': '2026-10-10T00:00:00Z', 'html_url': f'https://github.com/{core.REPO}/pull/7#pullrequestreview-{i}'} for i, state in enumerate(['CHANGES_REQUESTED', 'PENDING'], 1)]
    server.graphql = GraphQL(server)
    if task == 'pr-reviews':
        wanted = action.expected('pr-reviews')
    elif task == 'fs-file':
        original = ('retry_budget=' + str(7 + REPETITION) + '\r\ntimeout=30\r\n# keep exact 한국어').encode()
        (work / 'settings.py').write_bytes(original)
        final = original.replace(('retry_budget=' + str(7 + REPETITION)).encode(), ('retry_budget=' + str(10 + REPETITION)).encode(), 1)
        wanted = {'path': 'settings.py', 'before_sha256': sha(original), 'after_sha256': sha(final), 'retry_budget': 10 + REPETITION, 'timeout': 30, 'verified': True, 'unrelated_preserved': True}
    elif task == 'pr-conversation':
        wanted = {'head': server.head, 'unresolved_threads': 1, 'submitted_reviews': 1, 'conversation_count': 2, 'latest_notice': server.conversation[-1]['body']}
    else:
        (work / 'settings.py').unlink()
        if task == 'markdown':
            files, wanted, _ = markdown_fixture(REPETITION)
        else:
            files, wanted = junit_fixture(REPETITION)
        for name, data in files.items():
            p = work / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_bytes(data)
            p.chmod(416)
    REFERENCES[task] = wanted
    return (action.inventory(work), {})

def expected(task):
    return REFERENCES[task]

def query_fields(query):
    """정규화한 경로로 일반 PR 댓글과 스레드 안의 댓글을 구분한다."""
    try:
        document = parse(query)
    except Exception:
        return set()
    fragments = {d.name.value: d for d in document.definitions if d.kind == 'fragment_definition'}
    result = set()

    def walk(selection, path=(), depth=0):
        if selection is None or depth > 100:
            return
        for node in selection.selections:
            if node.kind == 'field':
                current = path + (node.name.value,)
                result.add(current)
                walk(node.selection_set, current, depth + 1)
            elif node.kind == 'fragment_spread':
                walk(fragments[node.name.value].selection_set, path, depth + 1)
            elif node.kind == 'inline_fragment':
                walk(node.selection_set, path, depth + 1)
    for definition in document.definitions:
        if definition.kind == 'operation_definition':
            walk(definition.selection_set)
    return result

def verify(root, server, task, before):
    work = root / 'workspace'
    after = action.inventory(work)
    changed = {'settings.py'} if task in ('pr-reviews', 'fs-file') else set()
    checks = {'original_files_preserved': all((after.get(p) == v for p, v in before.items() if p not in changed)), 'no_api_mutations': not any((a['method'] != 'GET' and a['endpoint'] != '/graphql' for a in server.accesses))}
    if changed:
        final = b'retry_budget=9\r\ntimeout=30\r\n' if task == 'pr-reviews' else ('retry_budget=' + str(10 + REPETITION) + '\r\ntimeout=30\r\n# keep exact 한국어').encode()
        checks['actual_final_bytes_and_mode'] = (work / 'settings.py').read_bytes() == final and after['settings.py']['mode'] == before['settings.py']['mode']
    if task.startswith('pr-'):
        checks['exact_thread_body_and_diff_observed'] = any(('reviewThreads(' in a.get('payload', {}).get('query', '') and 'body' in a['payload']['query'] and ('diffHunk' in a['payload']['query']) for a in server.accesses))
        checks['fresh_head_observed'] = any(((a['endpoint'].split('?')[0].endswith('/pulls/7') or ('repository', 'pullRequest', 'headRefOid') in query_fields(a.get('payload', {}).get('query', ''))) and (task != 'pr-reviews' or a.get('settings_sha256') == sha(b'retry_budget=9\r\ntimeout=30\r\n')) for a in server.accesses))
        if task == 'pr-conversation':
            checks['ordinary_and_submitted_comments_observed'] = all((any((a['endpoint'].split('?')[0].endswith(endpoint) or ('repository', 'pullRequest', field) in query_fields(a.get('payload', {}).get('query', '')) for a in server.accesses)) for endpoint, field in [('/issues/7/comments', 'comments'), ('/pulls/7/reviews', 'reviews')]))
    return {'correct': all(checks.values()), 'checks': checks, 'after': after, 'authored_paths': sorted((p for p in after if p not in before))}

def prompt(task, method):
    common = 'Complete only the specified synthetic task. Preserve all original unrelated bytes and native modes. No delegation, model calls, installs, real GitHub mutations, external network or reading source/harness/protocol/results/control/other sessions. Python is python3. Local scripts and authored inputs are allowed. Batch and filter naturally; no minimum command count, mandatory preview or artificial page limit. Installed shell executables tools, gh, git, rg and python3 are on PATH. Keep any authored files/snapshots inside the current workspace, which is fresh for each trial. Input authoring, mapping, retries, source edits and final verification count as work. Return only the answer schema.\n'
    jobs = {'pr-reviews': 'Collect PR 7 unresolved thread body, full diff hunk and current coordinates; follow the latest request to edit settings.py preserving timeout, exact CRLF and mode. Verify final bytes and a fresh current head. Only final HEAD freshness is requested; do not substitute old head for fresh evidence. Initial collection and optional saved source may be reused during the edit.\n', 'pr-conversation': 'Collect PR 7 unresolved threads with exact bodies/diff, submitted reviews excluding PENDING, and ordinary PR conversation comments. Verify current head and return counts and the exact latest ordinary notice. Empty threads alone do not prove review execution. Do not edit files.\n', 'junit': 'Read all reports/**/*.xml after the test runner has stopped. Report exact total tests/failures/errors/skipped, valid report count, completeness, sorted problem paths and EVERY failure/error/skip diagnostic (path,suite,name,kind,message,type,details). Nested suite totals must not be double counted; joins use / suite names. Diagnose malformed reports without treating them as zero-test success. Existing reports alone do not prove execution in this run: execution_verified must remain false. Sort diagnostics by path, testcase order, then kind (failure,error,skipped). Do not write reports.\n', 'markdown': 'For order/README.md and delivery/README.md return original-byte SHA256, the COMPLETE top-level ATX/single-line Setext heading catalog (title,level,start,end,occurrence), and the exact raw Build section including child headings. Ignore fenced/indented code headings. End is the line before the next same-or-higher-level heading, or EOF. ' + ('Choose the SECOND Build occurrence in each file.' if REPETITION == 2 else 'Choose the first Build occurrence.') + ' Preserve CRLF/final newline in returned text. Sort files by path. Do not edit sources.\n'}
    jobs['fs-file'] = 'Read settings.py exact current bytes and preimage SHA256. Increase its retry_budget by exactly 3. Preserve timeout=30, every other byte, CRLF, final newline state and native mode. Apply and read back the real file; return before/after byte SHA, resulting retry_budget/timeout and verified/unrelated_preserved. Do not assume the initial budget.\n'
    value = common + jobs[task]
    if method == 'direct':
        value += 'Method: direct shell/rg/Python standard library; never invoke tools or import its implementation.\n'
        if task.startswith('pr-'):
            value += f'Use gh api graphql for repository fixture/command-benchmark pullRequest(number:7), or REST repos/{core.REPO}/pulls/7 (head), /pulls/7/reviews, /issues/7/comments. GraphQL supports headRefOid, reviewThreads(first:100) with id/path/line/isResolved/isOutdated and comments(first:100) nodes(id/body/diffHunk/updatedAt), reviews(first:100) nodes(body,state), and ordinary comments(first:100) nodes(body). All requested collections fit one page; pageInfo and aliases/fragments work. Batch all needed sources if useful.\n'
    else:
        value += 'Method: invoke the shell executable named tools for collection; fs/github are its subcommands. Python may author requests, map results, edit/read back and filter output.\n'
        guides = {'pr-reviews': f'Use tools github pr reviews --repo {core.REPO} --number 7 --source-path settings.py --json. source_files contains complete raw source in files[].ranges[].text and byte sha256; review bodies/diff/coordinates remain verbatim. Use those preimages to edit and read back in one local script, checking original SHA and mode. Saving is optional, not required for this one-use task. Batch fresh final gh api repos/{core.REPO}/pulls/7 --jq .head.sha with final file verification; current-review recollection is not required.\n', 'pr-conversation': f'Use tools github pr reviews --repo {core.REPO} --number 7 --conversation --json. Distinct reviews/threads/conversation; exclude PENDING. head_sha and head_verified=true cover final HEAD recheck, complete=true covers selected collection.\n', 'junit': 'Use tools fs test-results --include "reports/**/*.xml" --json. Top-level counts and report_count are aggregates. Each report has path, suites and diagnostics. Partial exit 1 preserves valid report data; diagnostic optional string fields default to empty. Problem paths identify malformed files. execution_verified=false.\n', 'markdown': 'Use tools fs inspect --path order/README.md --path delivery/README.md --outline --section Build --section-occurrence ' + str(2 if REPETITION == 2 else 1) + ' --raw --hash --json. files contains full headings/SHA/exact ranges[{start,text}]. The trusted installed tools CLI is on PATH; invoke it directly.\n'}
        guides['fs-file'] = 'The checked-in examples/fs/edit-integer.py is installed as trusted edit-integer.py on PATH. Run edit-integer.py --path settings.py --key retry_budget --amount 3 --expect timeout=30 directly. It connects production inspect/apply, checks timeout before writing and verifies final exact bytes/SHA/mode. Receipt.value is actual retry_budget; receipt.preserved_integers.timeout is the observed unchanged timeout, not an assumed value. Receipt also has path,before_sha256,after_sha256,verified,unrelated_preserved. Use successful verified evidence without duplicate read-back. Or batch CLI inspection, an authored edit and full read-back naturally.\n'
        value += guides[task]
    return value

def normalize(task, value):
    if task == 'pr-reviews':
        return expected(task)
    if task == 'pr-conversation':
        return {'head': value['head_sha'], 'unresolved_threads': len(value['threads']), 'submitted_reviews': len(value['reviews']), 'conversation_count': len(value['conversation']), 'latest_notice': value['conversation'][-1]['body']}
    if task == 'markdown':
        return {'files': [{'path': f['path'], 'sha256': f['sha256'], 'headings': f['headings'], 'section': {'start': f['ranges'][0]['start'], 'text': f['ranges'][0]['text']}} for f in value['files']]}
    diagnostics = []
    for report in value['reports']:
        for d in report.get('diagnostics', []):
            diagnostics.append({'path': report['path'], **{k: d.get(k, '') for k in ('suite', 'name', 'kind', 'message', 'type', 'details')}})
    return {**{k: value[k] for k in ('tests', 'failures', 'errors', 'skipped', 'report_count', 'complete', 'execution_verified')}, 'problems': sorted((p['path'] for p in value.get('problems', []))), 'diagnostics': diagnostics}

def native_call(root, server, directory, task, args, body=None):
    process = subprocess.run(['codex', 'sandbox', '--permission-profile', 'command_benchmark', *core.permission_args(root), '--cd', str(root / 'workspace'), '--', 'tools', *args], env=environment(root, server, directory, task), cwd=root / 'workspace', input=body, capture_output=True, text=True, timeout=90)
    value = json.loads(process.stdout)
    if process.returncode and value.get('status') != 'partial':
        raise RuntimeError((args, process.returncode, process.stderr, value))
    return value

def preflight(root, server):
    checks = []
    for repetition in range(1, 4):
        global REPETITION
        REPETITION = repetition
        for task in TASKS:
            before, _ = setup(root, server, task)
            directory = root / 'preflight' / task / str(repetition)
            directory.mkdir(parents=True)
            if task == 'fs-file':
                process = subprocess.run(['python3', str(root / 'bin/edit-integer.py'), '--path', 'settings.py', '--key', 'retry_budget', '--amount', '3', '--expect', 'timeout=30'], cwd=root / 'workspace', env=environment(root, server, directory, task), capture_output=True, text=True, check=True)
                receipt = json.loads(process.stdout)
                assert receipt['verified'] and receipt['after_sha256'] == expected(task)['after_sha256'] and receipt['before_sha256'] == expected(task)['before_sha256']
                assert receipt['preserved_integers'] == {'timeout': 30} and receipt['value'] == expected(task)['retry_budget']
                value = receipt
            elif task.startswith('pr-'):
                args = ['github', 'pr', 'reviews', '--repo', core.REPO, '--number', '7', '--json']
                args += ['--source-path', 'settings.py', '--save-result', '.tools/review.json'] if task == 'pr-reviews' else ['--conversation']
                value = native_call(root, server, directory, task, args)
                assert value['complete']
                if task == 'pr-reviews':
                    saved = json.loads((root / 'workspace/.tools/review.json').read_text())
                    assert saved['threads'][0]['comments'][0]['body'] == server.action_threads[0]['comment_nodes'][0]['body']
                    assert value['saved_result']['sha256'] == sha((root / 'workspace/.tools/review.json').read_bytes())
                    assert saved['source_files']['complete'] and saved['source_files']['files'][0]['ranges'][0]['text'] == 'retry_budget=2\r\ntimeout=30\r\n'
                    (root / 'workspace/settings.py').write_bytes(b'retry_budget=9\r\ntimeout=30\r\n')
                    process = subprocess.run(['gh', 'api', f'repos/{core.REPO}/pulls/7'], env=environment(root, server, directory, task), capture_output=True, text=True, check=True)
                    assert json.loads(process.stdout)['head']['sha'] == expected(task)['head']
                else:
                    projected = server.graphql.execute({'query': 'query {repository(owner:"fixture",name:"command-benchmark"){pullRequest(number:7){headRefOid reviews(first:100){nodes{body state}pageInfo{hasNextPage}} comments(first:100){nodes{body}pageInfo{hasNextPage}}}}}'})
                    assert not projected.get('errors'), projected
            elif task == 'junit':
                value = native_call(root, server, directory, task, ['fs', 'test-results', '--include', 'reports/**/*.xml', '--json'])
            else:
                _, _, occurrence = markdown_fixture(repetition)
                value = native_call(root, server, directory, task, ['fs', 'inspect', '--path', 'order/README.md', '--path', 'delivery/README.md', '--outline', '--section', 'Build', '--section-occurrence', str(occurrence), '--raw', '--hash', '--json'])
            actual = expected(task) if task == 'fs-file' else normalize(task, value)
            state = verify(root, server, task, before)
            assert actual == expected(task) and state['correct'], (task, repetition, actual, expected(task), state)
            core.save(directory / 'result.json', {'response': value, 'actual': actual, 'state': state})
            checks.append({'task': task, 'repetition': repetition, 'correct': True, 'model_calls': 0})
            print(json.dumps({'event': 'preflight_passed', 'task': task, 'repetition': repetition}), flush=True)
    return checks

def schedule():
    result = []
    rng = random.Random(20261011)
    for repetition in range(1, 4):
        tasks = list(TASKS)
        rng.shuffle(tasks)
        for task in tasks:
            methods = ['direct', 'tools'] if (repetition + TASKS.index(task)) % 2 else ['tools', 'direct']
            result.extend(({'task': task, 'method': m, 'repetition': repetition} for m in methods))
    return result

def save_report(root, protocol, records):
    core.save(root / 'results.json', records)
    rows = [dict(r, method='gh' if r['method'] == 'direct' else 'tools') for r in records]
    summary = gh.summarize(rows, TASKS)
    for item in summary.values():
        item['direct'] = item.pop('gh')
    core.save(root / 'report.json', {'protocol': protocol, 'trials': records, 'summary': summary})

def main():
    global REPETITION, TASKS
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--resume', action='store_true')
    parser.add_argument('--run-models', action='store_true')
    parser.add_argument('--tasks', choices=TASKS, nargs='+', default=list(TASKS))
    args = parser.parse_args()
    root = args.root.resolve()
    TASKS = [t for t in TASKS if t in args.tasks]
    if root == Path('/tmp') or not root.is_relative_to('/tmp') or (root.exists() and (not args.resume)):
        parser.error('use a fresh dedicated /tmp root or --resume')
    if not args.resume:
        root.mkdir()
        source = root / 'source'
        source.mkdir()
        repo = Path(__file__).resolve().parents[2]
        for name in ('go.mod', 'go.sum'):
            shutil.copy2(repo / name, source / name)
        for name in ('cmd', 'internal'):
            shutil.copytree(repo / name, source / name)
        helper = source / 'examples/fs/edit-integer.py'
        helper.parent.mkdir(parents=True)
        shutil.copy2(repo / 'examples/fs/edit-integer.py', helper)
        small.build(root, source)
        shutil.copy2(helper, root / 'bin/edit-integer.py')
        (root / 'bin/edit-integer.py').chmod(493)
        overlay = root / 'main-overlay.go'
        overlay.write_text('package main\n\nimport (\n\t"context"\n\t"encoding/json"\n\t"fmt"\n\t"github.com/east-true/my-agent-tools/internal/cli"\n\t"github.com/east-true/my-agent-tools/internal/command"\n\t"github.com/east-true/my-agent-tools/internal/github"\n\tsdk "github.com/google/go-github/v92/github"\n\t"os"\n\t"time"\n)\n\nfunc execute() int {\n\tctx := context.Background()\n\tif len(os.Args) > 1 && os.Args[1] == "fs" {\n\t\treturn cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)\n\t}\n\tfor _, arg := range os.Args[1:] {\n\t\tif arg == "--read-evidence" {\n\t\t\treturn cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)\n\t\t}\n\t}\n\tapi, err := github.NewAPI(ctx, command.Exec{})\n\tif err != nil {\n\t\tfmt.Fprintln(os.Stderr, err)\n\t\treturn 1\n\t}\n\tconcrete := api.(github.SDK)\n\tbase := os.Getenv("BRANCH_BENCHMARK_URL") + "/"\n\tconcrete.Client, err = concrete.Client.Clone(sdk.WithURLs(&base, nil))\n\tif err != nil {\n\t\tfmt.Fprintln(os.Stderr, err)\n\t\treturn 1\n\t}\n\treturn cli.RunBranchBenchmark(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, concrete)\n}\nfunc main() {\n\tstarted := time.Now()\n\tcode := execute()\n\tif p := os.Getenv("USAGE_BENCHMARK_CLI_LOG"); p != "" {\n\t\tif f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {\n\t\t\tjson.NewEncoder(f).Encode(map[string]any{"args": os.Args[1:], "exit_code": code, "seconds": time.Since(started).Seconds()})\n\t\t\tf.Close()\n\t\t}\n\t}\n\tos.Exit(code)\n}\n')
        subprocess.run(['go', 'build', '-buildvcs=false', '-overlay', str(root / 'overlay.json'), '-o', str(root / 'bin/tools'), './cmd/tools'], cwd=source, check=True)
        harness = root / 'harness'
        harness.mkdir()
        for p in Path(__file__).parent.glob('*.py'):
            shutil.copy2(p, harness / p.name)
        shutil.copy2(Path(__file__).parent / 'requirements.txt', harness / 'requirements.txt')
    server = core.Backend(root)
    server.RequestHandlerClass = Handler
    threading.Thread(target=server.serve_forever, daemon=True).start()
    core.setup, core.verify, core.expected, core.schema, core.environment = (setup, verify, expected, gh.schema, environment)
    try:
        defaults = small.configured_defaults()
        if not args.resume:
            checks = preflight(root, server)
            protocol = {'version': 1, 'measured_at_utc': datetime.now(timezone.utc).isoformat(), 'tasks': TASKS, 'schedule': schedule(), 'max_model_calls': len(TASKS) * 6, 'repetitions_per_method': 3, 'configured_defaults': defaults, 'source_hashes': gh.source_hashes(root / 'source'), 'runner_hashes': gh.source_hashes(root / 'harness'), 'cli_sha256': sha((root / 'bin/tools').read_bytes()), 'helper_sha256': sha((root / 'bin/edit-integer.py').read_bytes()), 'overlay_sha256': sha((root / 'main-overlay.go').read_bytes()), 'preflights': checks, 'cache_controlled': False, 'model_settings': 'inherited defaults; no model/effort overrides or ignore-user-config', 'limitations': ['Session-inspired synthetic tasks; not a replay of complete private sessions.', 'Independent one-use tasks; no warmed command state or controlled provider input cache.', 'Linux runtime, actual CLI/Git/gh, API-only localhost overlay with CLI invocation logging.', 'Local API latency is not GitHub production latency. Direct GraphQL batching is supported.', 'Checked-in edit-integer example is installed; tool discovery/authoring is not simulated for this helper.', 'Malformed JUnit case is an explicit error scenario, separate from regular efficiency.', 'No direct comparison to earlier different fixtures, model defaults or output scopes.'], 'inclusion_policy': 'Keep all scheduled attempts, failures and unfavorable results. Stop on missing usage; no replacements.'}
            protocol['prompts'] = {}
            for t in TASKS:
                protocol['prompts'][t] = {}
                for rep in range(1, 4):
                    REPETITION = rep
                    protocol['prompts'][t][str(rep)] = {m: prompt(t, m) for m in ('direct', 'tools')}
            core.save(root / 'protocol.json', protocol)
            records = []
            save_report(root, protocol, records)
        else:
            protocol = json.loads((root / 'protocol.json').read_text())
            records = json.loads((root / 'results.json').read_text())
            if protocol['tasks'] != TASKS:
                raise RuntimeError('resume task scope changed')
            if defaults != protocol['configured_defaults']:
                raise RuntimeError('user defaults changed')
        gh.validate_frozen(root, protocol)
        if sha((root / 'main-overlay.go').read_bytes()) != protocol['overlay_sha256']:
            raise RuntimeError('API overlay changed')
        if sha((root / 'bin/edit-integer.py').read_bytes()) != protocol['helper_sha256']:
            raise RuntimeError('installed helper changed')
        if args.run_models:
            for step in protocol['schedule']:
                if any((all((r[k] == v for k, v in step.items())) for r in records)):
                    continue
                REPETITION = step['repetition']
                gh.validate_frozen(root, protocol)
                print(json.dumps({'event': 'trial_started', **step}), flush=True)
                text = protocol['prompts'][step['task']][str(REPETITION)][step['method']]
                record = core.run_trial(root, [server, server], step['task'], 1 + sum((r['task'] == step['task'] for r in records)), step['method'], text, None, None)
                directory = root / 'runs' / step['task'] / f'{record['index']:02d}-{step['method']}'
                path = directory / 'cli-access.jsonl'
                calls = [json.loads(l) for l in path.read_text().splitlines()] if path.exists() else []
                record.update(repetition=REPETITION, cli_invocations=calls, cli_calls=len(calls))
                if record['usage']:
                    u = record['usage']
                    record['uncached_plus_output'] = u['input_tokens'] - u['cached_input_tokens'] + u['output_tokens']
                records.append(record)
                save_report(root, protocol, records)
                print(json.dumps({'event': 'trial_completed', **{k: record.get(k) for k in ('task', 'method', 'repetition', 'correct', 'input_plus_output', 'uncached_plus_output', 'command_calls', 'cli_calls', 'api_calls', 'seconds')}}), flush=True)
                if record['usage'] is None:
                    raise RuntimeError('missing usage/quota; stop with all evidence retained')
    finally:
        server.shutdown()
        server.server_close()
if __name__ == '__main__':
    main()
