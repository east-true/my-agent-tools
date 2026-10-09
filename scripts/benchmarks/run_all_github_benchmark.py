#!/usr/bin/env python3
"""Benchmark all 15 current GitHub commands using inherited model/effort defaults."""
import argparse
import hashlib
import io
import json
import re
import shutil
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

import run_command_benchmark as core
import run_default_benchmark as small
import run_dependabot_benchmark as dep

TASKS = ['context', 'setup', 'issue-create', 'issue-branch', 'pr-create',
         'pr-reviews', 'pr-inspect', 'pr-delta', 'pr-submit', 'pr-merge',
         'ci-failures', 'ci-rerun', 'dependabot-list', 'dependabot-view', 'cleanup-apply']
ORIGINAL = {name: getattr(core, name) for name in ('setup', 'expected', 'verify', 'environment')}
FILES = [{'filename': 'new.go', 'status': 'renamed', 'additions': 2, 'deletions': 1, 'previous_filename': 'old.go'},
         {'filename': 'src/build.go', 'status': 'modified', 'additions': 3, 'deletions': 2}]
MERGE_SHA = 'c' * 40
REFERENCES = {}


class Handler(small.Handler):
    def do_PUT(self):
        self.respond()

    def respond(self):
        s = self.server
        parsed = urlsplit(self.path)
        path, params = parsed.path, parse_qs(parsed.query)
        raw = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        payload = json.loads(raw or '{}')
        query = payload.get('query', '')
        base = '/repos/' + core.REPO
        value, status = None, 200
        if path.startswith(base + '/dependabot/alerts'):
            self.rfile = io.BytesIO(raw)
            return dep.Handler.respond(self)
        if path == '/graphql' and 'headRefOid' in query and 'reviewThreads(' not in query:
            value = {'data': {'repository': {'pullRequest': {
                'url': f'https://github.com/{core.REPO}/pull/{42 if s.scenario == "pr-submit" else 7}',
                'state': 'MERGED' if s.merged else 'OPEN', 'isDraft': False, 'merged': s.merged,
                'headRefOid': s.head, 'baseRefOid': s.base, 'mergeable': 'MERGEABLE',
                'mergeStateStatus': 'CLEAN', 'reviewDecision': 'APPROVED',
                'mergeCommit': {'oid': MERGE_SHA} if s.merged else None,
                'potentialMergeCommit': None, 'mergeQueueEntry': None}}}}
        elif path == '/graphql' and 'reviewThreads(' in query:
            count = 0 if s.scenario in ('pr-submit', 'pr-merge') else 20
            threads = [{'id': f'T{i}', 'path': f'file{i}.go', 'line': 10, 'is_resolved': False,
                        'is_outdated': False, 'comments': {'nodes': [{'id': f'C{i}', 'body': f'Please validate input {i}.',
                        'updated_at': '2026-10-07T00:00:00Z'}], 'pageInfo': {'hasNextPage': False}}} for i in range(1, count+1)]
            value = {'data': {'repository': {'pullRequest': {'url': f'https://github.com/{core.REPO}/pull/7',
                     'head_sha': s.head, 'review_decision': 'APPROVED',
                     'reviewThreads': {'nodes': threads, 'pageInfo': {'hasNextPage': False}}}}}}
        elif path == '/graphql' and 'issues(states:CLOSED' in query and s.scenario == 'pr-merge':
            value = {'data': {'repository': {'issues': {'nodes': [], 'pageInfo': {'hasNextPage': False}}}}}
        elif path in (base + '/pulls/7', base + '/pulls/42'):
            number = int(path.rsplit('/', 1)[1])
            value = pr(s, number)
        elif path == base + '/pulls' and self.command == 'GET' and s.scenario in ('pr-submit', 'pr-merge'):
            value = ([pr(s, 42)] if s.created_prs else []) if s.scenario == 'pr-submit' else [pr(s, 7)]
        elif path.endswith('/reviews') and '/pulls/' in path:
            value = []
        elif path == base + '/branches' and s.scenario == 'pr-merge':
            value = [{'name': name.removeprefix('refs/heads/'), 'protected': False, 'commit': {'sha': sha}}
                     for name, sha in core.cleanup.inventory(s.root/'remote.git', 'refs/heads/').items()]
        elif path.startswith(base + '/branches/') and s.scenario == 'pr-merge':
            name = path.removeprefix(base + '/branches/')
            sha = core.cleanup.inventory(s.root/'remote.git','refs/heads/').get('refs/heads/'+name)
            value, status = ({'name': name, 'protected': False,'commit': {'sha': sha}},200) if sha else ({'message':'Not Found'},404)
        elif path.endswith('/check-runs') and '/commits/' in path:
            value = {'check_runs': [{'id': 91, 'name': 'build', 'status': 'completed',
                     'conclusion': 'success' if s.scenario in ('pr-submit','pr-merge') else 'failure',
                     'app': {'slug': 'github-actions'},
                     'details_url': f'https://github.com/{core.REPO}/actions/runs/42/job/11'}]}
        elif path == base + '/actions/runs/42':
            if s.reruns:
                s.polls += 1
            active = s.reruns and s.polls == 1
            value = {'id': 42, 'run_attempt': 3 if s.reruns else 2, 'head_sha': s.head,
                     'status': 'in_progress' if active else 'completed', 'conclusion': None if active else
                     ('success' if s.scenario in ('pr-submit','pr-merge') else 'failure')}
        elif path == base + '/actions/runs/42/rerun-failed-jobs' and self.command == 'POST':
            if s.reruns:
                status, value = 409, {'message': 'duplicate rerun'}
            else:
                s.reruns += 1
                value, status = {}, 201
        elif re.fullmatch(re.escape(base) + r'/actions/runs/42/attempts/[23]/jobs', path):
            value = {'jobs': [{'id': 11, 'name': 'build', 'status': 'completed', 'conclusion': 'failure',
                     'check_run_url': f'https://api.github.com{base}/check-runs/91',
                     'steps': [{'number': 2, 'name': 'compile', 'conclusion': 'failure',
                     'started_at': '2026-10-07T00:00:01Z','completed_at': '2026-10-07T00:00:02Z'}]}]}
        elif path.startswith(base + '/compare/') and s.scenario == 'pr-delta':
            value = {'status': 'ahead', 'total_commits': 1, 'base_commit': {'sha': s.base},
                     'merge_base_commit': {'sha': s.base}, 'commits': [{'sha': s.head}],
                     'files': [dict(f, patch='@@ -1 +1 @@\n-old\n+new') for f in reversed(FILES)]}
        elif path == base + '/pulls/7/merge-async' and self.command == 'PUT':
            if payload != {'sha': s.head, 'merge_method': 'squash', 'merge_action': 'default', 'bypass_rules': False} or s.merge_requests:
                status, value = 422, {'message': 'invalid/duplicate merge'}
            else:
                s.merge_requests += 1
                value = {'status': 'pending', 'details': {'uuid': 'fixture-merge'}}
        elif path == base + '/pulls/7/merge-async/fixture-merge':
            s.merged = bool(s.merge_requests)
            value = {'status': 'merged' if s.merged else 'failed', 'details': {'sha': MERGE_SHA}}
        if value is None:
            self.rfile = io.BytesIO(raw)
            return super().respond()
        data = json.dumps(value, ensure_ascii=False).encode()
        s.accesses.append({'method': self.command, 'endpoint': self.path, 'payload': payload,
                           'status': status, 'response_bytes': len(data)})
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def pr(server, number):
    result = dict(server.created_prs[0]) if server.created_prs else {'number': number, 'title': core.TITLE}
    return result | {'number': number, 'html_url': f'https://github.com/{core.REPO}/pull/{number}',
                     'state': 'closed' if server.merged else 'open', 'merged': server.merged,
                     'merged_at': '2026-10-08T00:00:00Z' if server.merged else None,
                     'head': {'ref': core.BRANCH, 'sha': server.head, 'repo': {'full_name': core.REPO}},
                     'base': {'ref': 'main', 'sha': server.base}}


def setup(root, server, task):
    shutil.rmtree(root/'workspace.worktrees', ignore_errors=True)
    shutil.rmtree(root/'merge-worktree', ignore_errors=True)
    mapped = 'cleanup-preview' if task == 'cleanup-apply' else 'pr-create' if task in ('pr-submit','pr-merge') else task
    before, fixture = ORIGINAL['setup'](root, server, mapped)
    server.scenario, server.changed = task, False
    server.head, server.base = small.SHA, 'b'*40
    server.reruns, server.polls, server.merge_requests, server.merged = 0, 0, 0, False
    work = root/'workspace'
    if task=='cleanup-apply':
        # The current cleanup removes finished clean worktrees. This one has
        # authored work and must be retained; merge tests the clean removal.
        (root/'worktree/user-work.txt').write_text('preserve worktree user file\n')
    if task in ('pr-submit','pr-merge'):
        server.head = core.cleanup.git(work, 'rev-parse', 'HEAD')
        server.base = server.base_sha
        # Fixture user files are ignored in the main worktree so submit sees
        # an authored clean commit. The removable PR worktree remains empty.
        (work/'.git/info/exclude').write_text('body.md\nuser-work.txt\n.tools/\n')
        if task == 'pr-submit':
            core.cleanup.git(root/'remote.git', 'update-ref', 'refs/heads/'+core.BRANCH, server.base)
        else:
            core.cleanup.git(work, 'switch', '-q', 'main')
            core.cleanup.git(work, 'worktree', 'add', '-q', str(root/'merge-worktree'), core.BRANCH)
        before = core.snapshot(root, server)
    REFERENCES[task] = reference(task, server)
    return before, fixture


def reference(task, server):
    if task in ('context','setup','issue-create','issue-branch','pr-create'):
        return ORIGINAL['expected'](task)
    if task == 'cleanup-apply':
        return core.cleanup.EXPECTED | {'kept_targets': 17}
    if task == 'dependabot-list':
        return {key: dep.REFERENCE[key] for key in ('count','alerts')}
    if task == 'dependabot-view':
        return dep.REFERENCE['detail']
    if task in ('ci-failures','ci-rerun'):
        result = {'run': 42, 'attempt': 3 if task == 'ci-rerun' else 2, 'head': server.head,
                  'path': 'src/build.go', 'line': 7, 'expected_arguments': 1, 'missing_method': 'Flush', 'exit_code': 1}
        return result | {'status': 'failed', 'rerun_requested': True} if task == 'ci-rerun' else result
    if task == 'pr-reviews':
        return {'head': server.head, 'review_decision': 'APPROVED', 'submitted_count': 0,
                'threads': [{'id': f'T{i}', 'path': f'file{i}.go', 'line': 10,
                             'comments': [{'id': f'C{i}', 'body': f'Please validate input {i}.'}]} for i in range(1,21)]}
    if task == 'pr-inspect':
        return {'status': 'blocked','head': server.head,'base': server.base, 'checks': ['build'], 'threads': 20,
                'run': 42,'attempt': 2,'expected_arguments': 1,'missing_method': 'Flush','exit_code': 1}
    if task == 'pr-delta':
        return {'since': server.base,'head': server.head,'complete': True,'files': FILES}
    if task == 'pr-submit':
        return {'number': 42,'head': server.head,'branch': core.BRANCH,'pushed': True,'inspection_status': 'ready'}
    if task == 'pr-merge':
        return {'number': 7,'head': server.head,'merge_sha': MERGE_SHA,'merged': True,
                'deleted_local': [core.BRANCH],'deleted_remote': [core.BRANCH],'removed_worktree': True}
    raise ValueError(task)


def expected(task):
    return REFERENCES[task]


def schema(value):
    if value is None: return {'type': 'null'}
    if isinstance(value,dict):
        props = {k: schema(v) for k,v in value.items()}
        if 'first_patched_version' in props:
            props['first_patched_version'] = {'anyOf': [{'type': 'string'},{'type': 'null'}]}
        return {'type': 'object','additionalProperties': False,'required': list(value),'properties': props}
    if isinstance(value,list):
        variants={json.dumps(schema(item),sort_keys=True):schema(item) for item in value}
        items=list(variants.values())
        return {'type':'array','items':items[0] if len(items)==1 else {'anyOf':items} if items else {'type':'string'}}
    return {'type': 'boolean' if isinstance(value,bool) else 'integer' if isinstance(value,int) else 'string'}


def verify(root, server, task, before):
    if task == 'cleanup-apply':
        result=core.cleanup.verify_state(root, server.fixture)
        result['checks']['worktree_file_preserved']=(root/'worktree/user-work.txt').read_text()=='preserve worktree user file\n'
        result['correct']=all(result['checks'].values())
        return result
    after = core.snapshot(root, server)
    if task in ('issue-create','issue-branch'):
        wanted = 'refs/heads/'+core.BRANCH
        path = root/'workspace.worktrees'/core.BRANCH
        checks = {'main_head_preserved': after['head']==before['head'],
                  'user_files_preserved': all(after[k]==before[k] for k in ('file','body')),
                  'remote_branch': after['remote']==before['remote']|{wanted:server.base_sha},
                  'local_branch': after['local']==before['local']|{wanted:server.base_sha},
                  'tracking_branch': after['tracking']==before['tracking']|{'refs/remotes/origin/'+core.BRANCH:server.base_sha},
                  'worktree_checkout': path.exists() and core.cleanup.git(path,'symbolic-ref','--short','HEAD')==core.BRANCH,
                  'upstream': core.cleanup.git(root/'workspace','for-each-ref','--format=%(upstream:short)',wanted)=='origin/'+core.BRANCH,
                  'development_link': server.linked==[core.BRANCH],
                  'single_link_mutation': sum('createLinkedBranch' in a['payload'].get('query','') for a in server.accesses)==1,
                  'issue_state': server.created_issues==([core.issue()] if task=='issue-create' else [])}
        if task=='issue-branch':
            checks['resume_link_checked']=any('linkedBranches' in a['payload'].get('query','') for a in server.accesses)
    elif task in ('context','setup','pr-create'):
        return ORIGINAL['verify'](root,server,task,before)
    elif task == 'pr-submit':
        checks = {'remote_push': after['remote']['refs/heads/'+core.BRANCH]==server.head,
                  'local_preserved': all(after[k]==before[k] for k in ('local','head','file','body','worktrees')),
                  'pr_created_once': len(server.created_prs)==1,
                  'pr_fields': bool(server.created_prs) and all(server.created_prs[0].get(k)==v for k,v in
                    [('title',core.TITLE),('body',core.BODY+'\n\nCloses #41'),('head',core.BRANCH),('base','main'),('labels',[{'name':'bug'}])])}
    elif task == 'pr-merge':
        local = {k:v for k,v in before['local'].items() if k!='refs/heads/'+core.BRANCH}
        remote = {k:v for k,v in before['remote'].items() if k!='refs/heads/'+core.BRANCH}
        checks = {'merged_once': server.merged and server.merge_requests==1,
                  'local_expected_deletion': after['local']==local,'remote_expected_deletion': after['remote']==remote,
                  'tracking_expected_deletion': after['tracking']=={k:v for k,v in before['tracking'].items() if k!='refs/remotes/origin/'+core.BRANCH},
                  'worktree_removed': not (root/'merge-worktree').exists(),
                  'user_files_and_head_preserved': all(after[k]==before[k] for k in ('head','file','body'))}
    else:
        checks = {'read_only_git_and_user_files': after==before,
                  'mutations': server.reruns==1 if task=='ci-rerun' else not server.reruns and not server.merge_requests}
        if task in ('ci-failures','ci-rerun','pr-inspect'):
            checks['logs_observed']=any(a['endpoint']=='/signed-log' for a in server.accesses)
        if task=='ci-rerun':checks['completed_new_attempt']=server.polls>=2
        if task=='pr-reviews':checks['reviews_and_history_observed']=any('reviewThreads(' in a['payload'].get('query','') for a in server.accesses) and any('/reviews' in a['endpoint'] for a in server.accesses)
        if task=='dependabot-list':checks['both_cursor_pages']=sum('/dependabot/alerts?' in a['endpoint'] for a in server.accesses)>=2
    return {'correct': all(checks.values()),'checks': checks,'after': after}


def environment(root, server, directory, task):
    return ORIGINAL['environment'](root,server,directory,'cleanup-preview' if task=='cleanup-apply' else task)


COMMANDS = dict(core.COMMANDS, **{
    'pr-reviews': [['tools','github','pr','reviews','--number','7','--compact','--json']],
    'pr-inspect': [['tools','github','pr','inspect','--number','7','--json']],
    'pr-delta': [['tools','github','pr','delta','--number','7','--since','b'*40,'--json']],
    'pr-submit': [['tools','github','pr','submit','--prefix','fix','--title','measure command workflow','--body-file','body.md','--interval','10ms','--timeout','5s','--json']],
    'pr-merge': [['tools','github','pr','merge','--number','7','--interval','10ms','--timeout','5s','--json']],
    'ci-failures': [['tools','github','ci','failures','--run','42','--compact','--json']]*2,
    'ci-rerun': [['tools','github','ci','rerun','--run','42','--interval','10ms','--timeout','5s','--compact','--json']],
    'dependabot-list': [dep.COMMANDS[0]], 'dependabot-view': [dep.COMMANDS[1]],
    'cleanup-apply': [['tools','github','branch','cleanup','--apply','--json']]})
REVIEW_QUERY = 'query { repository(owner:"fixture",name:"command-benchmark") { pullRequest(number:7) { head_sha:headRefOid review_decision:reviewDecision reviewThreads(first:100) { nodes { id path line is_resolved:isResolved comments(first:100) { nodes { id body updated_at:updatedAt } pageInfo { hasNextPage endCursor } } } pageInfo { hasNextPage endCursor } } } } }'
MERGE_QUERY = 'query { repository(owner:"fixture",name:"command-benchmark") { pullRequest(number:7) { url state isDraft merged headRefOid baseRefOid mergeable mergeStateStatus reviewDecision mergeCommit { oid } potentialMergeCommit { oid } mergeQueueEntry { id } } } }'
CI_GUIDE = f'''Read gh api repos/{core.REPO}/actions/runs/42, then that attempt's jobs (repos/{core.REPO}/actions/runs/42/attempts/ATTEMPT/jobs), job 11 annotations (repos/{core.REPO}/check-runs/91/annotations) and plain text log (repos/{core.REPO}/actions/jobs/11/logs). Derive compiler location, expected arguments, missing method and exit code from the evidence. Preserve run ID, attempt and head identity. Don't modify code.\n'''
REVIEW_GUIDE = f'''Read GraphQL with gh api graphql -f query='{REVIEW_QUERY}', read submitted review history via gh api repos/{core.REPO}/pulls/7/reviews, and verify head via gh api repos/{core.REPO}/pulls/7. Return unresolved threads with comment IDs and exact bodies, not summaries.\n'''


def prompt(task, method):
    if task=='cleanup-apply':
        common=core.cleanup.COMMON.replace('initial local + remote + stale tracking targets excluded from deletion',
            'initial local + remote + stale tracking + worktree targets excluded from deletion; retained main and dirty worktrees count as additional targets')
        return common+'\nUse python3; python is not installed. Count retained worktrees as kept targets in addition to branch references.\n'+core.cleanup.GUIDES[method]
    shared = core.SHARED.replace('All collections fit one page.', 'Dependabot list has two cursor pages; other collections fit one page.').replace('No delegation, other agents, web/MCP tools, live GitHub operations, commits or source pushes.', 'No delegation, other agents, web/MCP tools, live GitHub operations or commits. Pushes are authorized only for the submit task through the wrapped Git into its disposable bare remote.')
    shared += f'\nTask: {task}. Repo: {core.REPO}. Use python3; python is not installed. Return only the provided schema.\n'
    if task in ('context','setup','issue-create','issue-branch','pr-create'):
        value=core.prompt_for(task,method)
        value=value.replace('Fetch with git fetch --no-tags origin refs/heads/NAME:refs/remotes/origin/NAME and switch/create a local tracking branch with git switch --create NAME --track origin/NAME, or git switch NAME if an existing local branch already tracks that remote.',
            'Keep the main worktree and HEAD unchanged. Destination is the absolute Git root plus .worktrees/BRANCH_NAME (a sibling, not inside the root). Fetch with git fetch --no-tags origin refs/heads/NAME:refs/remotes/origin/NAME; create a separate worktree with git worktree add --track -b NAME -- ABSOLUTE_DESTINATION origin/NAME. If the linked branch and worktree exist, verify Development association, upstream and checked-out branch, then reuse them; never recreate or switch the main worktree.')
        value=value.replace('compare/main...HEAD','compare/main...CURRENT_BRANCH_NAME')
        if task=='pr-create':value=value.replace(', and gh api repos/fixture/command-benchmark/issue-types','').replace('; only enabled types are selectable','')
        return value+'\nUse python3; python is not installed. Issue checkout means a separate worktree, preserving main HEAD.\n'
    if method=='tools':
        return shared+'Use tools only; never gh. Run:\n'+'\n'.join(subprocess.list2cmdline(c) for c in COMMANDS[task])+f'\nNormalize successful returned data to the schema. {NORMALIZATION[task]}\n'
    guide = {
        'pr-reviews': REVIEW_GUIDE,
        'ci-failures': CI_GUIDE+'Revalidate run identity/attempt once after collection and reuse completed facts if unchanged. Do not rerun CI.',
        'ci-rerun': CI_GUIDE+f'Read current completed attempt and recheck immediately before requesting exactly one POST gh api repos/{core.REPO}/actions/runs/42/rerun-failed-jobs --method POST --input a JSON file containing {{}}. Poll run metadata until the new expected attempt completes (short sleeps are allowed), then collect that attempt failure evidence. A rerun accepted response or old completed attempt is not completion. Return status failed if new attempt fails and rerun_requested true.',
        'pr-delta': f"Read merge metadata using gh api graphql -f query='{MERGE_QUERY}'. Baseline is {'b'*40}. Fetch gh api repos/{core.REPO}/compare/BASELINE...CURRENT_HEAD?per_page=100. Verify baseline equals base_commit.sha and merge_base_commit.sha, ahead/identical status, commit count and final commit equals head. Recheck PR head before returning. Sort files by filename and return filename,status,additions,deletions and previous_filename only when present; omit patches.",
        'pr-inspect': f"Read merge metadata using gh api graphql -f query='{MERGE_QUERY}', head check runs (gh api repos/{core.REPO}/commits/HEAD/check-runs) and statuses (gh api repos/{core.REPO}/commits/HEAD/statuses). "+REVIEW_GUIDE+CI_GUIDE+'Recheck head/base after all reads. Return blocked if checks fail or unresolved threads remain; derive check names, thread count and CI facts.',
        'dependabot-list': dep.GH_GUIDE.split('Read alert 7 detail',1)[0]+"Return count and alerts only. Project number,state,dependency.package.name/ecosystem,dependency.manifest_path/scope, security_vulnerability.severity/vulnerable_version_range/first_patched_version.identifier. Keep null patch values; don't use advisory patch. No detail lookup.",
        'dependabot-view': f'Only read gh api repos/{core.REPO}/dependabot/alerts/7. Return number, security_advisory.description, and reference URL strings. No list lookup.',
        'pr-submit': core.GH_GUIDES['pr-create'].replace(core.CATALOG_GUIDE,'Read repository metadata and labels, not issue types.\n').replace('compare/main...HEAD','compare/main...CURRENT_BRANCH_NAME').replace('Do not change branches/commits or create another issue.','Do not change branches/commits or create another issue. A source push is required here.')+f" Before creating the PR, validate a clean git status, repository fetch/push URL, current branch and HEAD; read gh api 'repos/{core.REPO}/pulls?state=open&head=fixture:CURRENT_BRANCH_NAME' and reuse a matching PR if present. Push existing HEAD using wrapped git push --porcelain --no-follow-tags -- origin SHA:refs/heads/BRANCH. Verify remote ref SHA with gh api repos/{core.REPO}/git/ref/heads/BRANCH. Create/apply labels only if no matching open PR. Inspect the resulting PR's merge metadata (same query as below with PR number 42), checks, statuses, unresolved threads and submitted history; wait for completed checks; do not merge. Return number,head,branch,pushed and inspection_status ready when checks pass and no unresolved threads. GraphQL: "+MERGE_QUERY.replace('number:7','number:42')+' Review query: '+REVIEW_QUERY.replace('number:7','number:42'),
        'pr-merge': f"Read gh api repos/{core.REPO}/pulls/7 for source ref/SHA/repo and merge metadata using gh api graphql -f query='{MERGE_QUERY}'. Check current SHA checks/statuses and review/merge blockers. Recheck head/base before merging. Send exactly one PUT gh api repos/{core.REPO}/pulls/7/merge-async --method PUT --input a JSON file containing sha=current_head,merge_method=squash,merge_action=default,bypass_rules=false. Poll GET repos/{core.REPO}/pulls/7/merge-async/UUID until merged, then confirm actual merged state. After success, clean ONLY that source branch at the captured SHA: main/default/current worktree stay; inspect git worktree list and status including ignored/untracked files before removing its separate clean worktree with git worktree remove -- PATH. Validate local ancestry and expected SHA; delete remote via git push --force-with-lease=refs/heads/BRANCH:SHA origin :refs/heads/BRANCH; local via git update-ref --no-deref -d refs/heads/BRANCH SHA; remove branch configuration when present. Keep all user files and main HEAD. Return observed merge SHA and deleted branch names and worktree removal. Never clean another branch."
    }[task]
    return shared+'Use gh and Git directly; never tools. Batching, jq and local scripts allowed.\n'+guide


NORMALIZATION = {
    'pr-reviews': 'Use head_sha,review_decision,len(reviews), and threads mapped to id,path,line and comments id/body.',
    'pr-inspect': 'Use status,pr.head_sha/base_sha,check names,len(reviews.threads), and failures[0].run plus evidence Go facts.',
    'pr-delta': 'Return since,head,complete,files without patches; preserve previous_filename only when present.',
    'pr-submit': 'Use number,head_sha,branch,pushed and inspection.status. Inspect is included in this command.',
    'pr-merge': 'Use number,head_sha,merge_sha,merged and cleanup.actions with status deleted for local/remote. removed_worktree means deleted local action had worktree_path.',
    'ci-failures': 'Use run.id/run_attempt/head_sha and evidence diagnostic path/line,expected_arguments pkg.Build,missing_interface_methods and exit_codes.',
    'ci-rerun': 'Use status,rerun_requested,run.id/run_attempt/head_sha and failure.evidence facts from the NEW attempt. Exit 1 with status failed and complete failure evidence means the new attempt finished unsuccessfully; use its evidence and never request another rerun.',
    'dependabot-list': 'Use count and alerts fields, preserving null first_patched_version. No detail lookup.',
    'dependabot-view': 'Use alert.number,description,references.'}


def normalize(task, results, server):
    last=results[-1]
    if task=='context':
        c=last['catalog'];return {'repo':c['repository']['full_name'],'default_branch':c['repository']['default_branch'],
          'labels':[x['name'] for x in c['labels']],'issue_types':[x['name'] for x in c['issue_types'] if x.get('is_enabled',True)]}
    if task=='setup':return last['plan']['config']
    if task in ('issue-create','issue-branch'):
        return {'number':last['number'],**{k:last['branch'][k] for k in ('linked','checked_out')},'branch':last['branch']['name']}
    if task=='pr-create':
        p=server.created_prs[0];return {k:([x['name'] for x in p[k]] if k=='labels' else p[k]) for k in expected(task)}
    if task=='cleanup-apply':return {'deleted_'+kind:sorted(x['name'] for x in last['actions'] if x['scope']==kind and x['status']=='deleted') for kind in ('local','remote','tracking')}|{'kept_targets':last['summary']['skipped']}
    if task=='dependabot-list':return {'count':last['count'],'alerts':[{k:x[k] for k in dep.FIELDS} for x in last['alerts']]}
    if task=='dependabot-view':return {k:last['alert'][k] for k in expected(task)}
    if task=='pr-delta':return {k:last[k] for k in expected(task)}
    if task=='pr-submit':return {'number':last['number'],'head':last['head_sha'],'branch':last['branch'],'pushed':last['pushed'],'inspection_status':last['inspection']['status']}
    if task=='pr-merge':return {'number':last['number'],'head':last['head_sha'],'merge_sha':last['merge_sha'],'merged':last['merged'],
          'deleted_local':sorted(x['name'] for x in last['cleanup']['actions'] if x['scope']=='local' and x['status']=='deleted'),
          'deleted_remote':sorted(x['name'] for x in last['cleanup']['actions'] if x['scope']=='remote' and x['status']=='deleted'),
          'removed_worktree':any(x.get('worktree_path') and x['status']=='deleted' for x in last['cleanup']['actions'])}
    if task=='pr-reviews':return {'head':last['head_sha'],'review_decision':last['review_decision'],'submitted_count':len(last['reviews']),
          'threads':[{'id':t['id'],'path':t['path'],'line':t['line'],'comments':[{k:c[k] for k in ('id','body')} for c in t['comments']]} for t in last['threads']]}
    failure = last['failure'] if task=='ci-rerun' else last['failures'][0] if task=='pr-inspect' else last
    evidence=failure['evidence'][0]
    facts={'run':failure['run']['id'],'attempt':failure['run']['run_attempt'],'head':failure['run']['head_sha'],
          'path':evidence['diagnostics'][0]['path'],'line':evidence['diagnostics'][0]['line'],
          'expected_arguments':evidence['expected_arguments']['pkg.Build'],'missing_method':evidence['missing_interface_methods'][0],'exit_code':evidence['exit_codes'][0]}
    if task=='pr-inspect':return {k:facts[k] for k in ('run','attempt','expected_arguments','missing_method','exit_code')}|{'status':last['status'],'head':last['pr']['head_sha'],'base':last['pr']['base_sha'],'checks':[x['name'] for x in last['checks']],'threads':len(last['reviews']['threads'])}
    return facts|{'status':last['status'],'rerun_requested':last['rerun_requested']} if task=='ci-rerun' else facts


def save_report(root, protocol, records):
    core.save(root/'results.json',records)
    core.save(root/'report.json',{'protocol':protocol,'trials':records,'summary':{task:core.cleanup.summarize([r for r in records if r['task']==task and r.get('included',True)]) for task in TASKS}})


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,required=True)
    parser.add_argument('--resume',action='store_true')
    parser.add_argument('--run-models',action='store_true')
    parser.add_argument('--repair-delta',action='store_true',help='replace the two original heterogeneous-file schema trials, preserving exclusions')
    args=parser.parse_args();root=args.root.resolve()
    if args.repair_delta and not (args.resume and args.run_models):parser.error('repair needs --resume --run-models')
    if root==Path('/tmp') or not root.is_relative_to('/tmp') or root.exists() and not args.resume:parser.error('fresh dedicated /tmp root, or --resume')
    if not args.resume:
        root.mkdir(parents=True);source=root/'source';source.mkdir();repo=Path(__file__).resolve().parents[2]
        for name in ('go.mod','go.sum'):shutil.copy2(repo/name,source/name)
        for name in ('cmd','internal'):shutil.copytree(repo/name,source/name)
        small.build(root,source)
        wrapper=root/'bin/git'
        wrapper.write_text(wrapper.read_text().replace("if 'push' in args:","if 'fetch' in args:\n  args=[os.environ['BRANCH_BENCHMARK_REMOTE'] if a=='origin' else a for a in args]\n if 'push' in args:"))
    servers=[core.Backend(root),core.cleanup.Backend(root)];servers[0].RequestHandlerClass=Handler
    for server in servers:threading.Thread(target=server.serve_forever,daemon=True).start()
    for name in ORIGINAL:setattr(core,name,globals()[name])
    core.schema=schema
    try:
        defaults=small.configured_defaults()
        if not args.resume or not (root/'protocol.json').exists():
            preflights=[]
            for task in TASKS:
                server=servers[task=='cleanup-apply'];before,_=setup(root,server,task)
                directory=root/'preflight'/task;directory.mkdir(parents=True,exist_ok=True)
                server.interface_access_path=directory/'interface-access.jsonl'
                server.interface_access_path.write_text('')
                results=[]
                for command in COMMANDS[task]:
                    result=subprocess.run(['codex','sandbox','--permission-profile','command_benchmark',*core.permission_args(root),'--cd',str(root/'workspace'),'--',*command],
                       cwd=root/'workspace',env=environment(root,server,directory,task),capture_output=True,text=True,timeout=90)
                    if result.returncode and not (task=='ci-rerun' and result.returncode==1 and
                        json.loads(result.stdout).get('status')=='failed' and json.loads(result.stdout).get('failure',{}).get('complete')):
                        raise RuntimeError(task+': '+result.stderr+'\n'+result.stdout)
                    results.append(json.loads(result.stdout))
                verification=verify(root,server,task,before);actual=normalize(task,results,server)
                if actual!=expected(task) or not verification['correct']:raise RuntimeError(str((task,actual,expected(task),verification)))
                core.save(directory/'results.json',{'responses':results,'actual':actual,'verification':verification,'api_access':server.accesses})
                preflights.append({'task':task,'correct':True,'native_model_calls':0})
                print(json.dumps({'event':'preflight_passed','task':task}),flush=True)
            protocol={'measured_at_utc':datetime.now(timezone.utc).isoformat(),'tasks':TASKS,'configured_defaults':defaults,
                'settings':'Inherited user defaults; no model/effort override or --ignore-user-config',
                'repetitions_per_method':1,'max_model_calls':30,'fresh_sessions':True,'cache_controlled':False,
                'source_hashes':{str(p.relative_to(root/'source')):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((root/'source').rglob('*')) if p.is_file()},
                'cli_sha256':hashlib.sha256((root/'bin/tools').read_bytes()).hexdigest(),
                'prompts':{task:{method:prompt(task,method) for method in ('gh','tools')} for task in TASKS},
                'preflights':preflights,'usage_source':'codex exec --json turn.completed.usage input + output; cached input included',
                'limitations':['One synthetic successful path per command; not all options or production scale.','One trial per method, uncontrolled cache; no monetary savings claim.','Current CLI vs direct gh/Git, not a before/after CLI experiment.','System/MCP prompts inherit user config. Runtime model/effort not exposed by JSON events.']}
            core.save(root/'protocol.json',protocol);records=[]
        else:
            protocol=json.loads((root/'protocol.json').read_text());records=json.loads((root/'results.json').read_text())
            if defaults!=protocol['configured_defaults']:raise RuntimeError('user defaults changed')
        if args.repair_delta:
            reason='Original output schema incorrectly required previous_filename on non-renamed files; heterogeneous array schemas corrected.'
            if not protocol.get('delta_schema_repair'):
                protocol['delta_schema_repair']=reason
                protocol['max_model_calls']+=2
                for record in records:
                    if record['task']=='pr-delta':record.update(included=False,exclusion_reason=reason)
                core.save(root/'protocol.json',protocol);save_report(root,protocol,records)
            for method in ('tools','gh'):
                if any(r['task']=='pr-delta' and r['method']==method and r.get('included',True) for r in records):continue
                index=1+max(r['index'] for r in records if r['task']=='pr-delta')
                print(json.dumps({'event':'trial_started','task':'pr-delta','method':method}),flush=True)
                record=core.run_trial(root,servers,'pr-delta',index,method,protocol['prompts']['pr-delta'][method],None,None)
                record.update(configured_defaults=defaults,included=True);records.append(record);save_report(root,protocol,records)
                print(json.dumps({'event':'trial_completed',**{k:record.get(k) for k in ('task','method','correct','usage','seconds','api_calls')}}),flush=True)
                if record['usage'] is None and record['command_calls']==0:raise RuntimeError('model unavailable; stopping')
            return
        if args.run_models:
            for task in TASKS:
                for method in (['gh','tools'] if TASKS.index(task)%2==0 else ['tools','gh']):
                    if any(r['task']==task and r['method']==method for r in records):continue
                    index=1+sum(r['task']==task for r in records)
                    print(json.dumps({'event':'trial_started','task':task,'method':method}),flush=True)
                    record=core.run_trial(root,servers,task,index,method,protocol['prompts'][task][method],None,None)
                    record['configured_defaults']=defaults;records.append(record);save_report(root,protocol,records)
                    print(json.dumps({'event':'trial_completed',**{k:record.get(k) for k in ('task','method','correct','usage','seconds','api_calls')}}),flush=True)
                    if record['usage'] is None and record['command_calls']==0:raise RuntimeError('model unavailable or execution rejected; retained evidence; stopping')
        else:save_report(root,protocol,records)
    finally:
        for server in servers:server.shutdown();server.server_close()


if __name__=='__main__':main()
