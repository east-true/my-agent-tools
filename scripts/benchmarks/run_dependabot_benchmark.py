#!/usr/bin/env python3
"""Compare Dependabot list + view over a synthetic two-page local API fixture."""
import argparse
import hashlib
import json
import os
import subprocess
import threading
from datetime import datetime, timezone
from http.server import ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

import run_command_benchmark as core

TASK = 'dependabot'


def resource(number, severity, package, ecosystem, patch):
    return {'number': number, 'state': 'open', 'html_url': f'https://github.com/{core.REPO}/security/dependabot/{number}',
            'dependency': {'package': {'name': package, 'ecosystem': ecosystem}, 'manifest_path': 'package-lock.json' if ecosystem == 'npm' else 'go.mod', 'scope': 'runtime', 'relationship': 'direct'},
            'security_advisory': {'ghsa_id': f'GHSA-fixture-{number}', 'summary': 'Synthetic benchmark advisory',
                'description': 'Synthetic advisory description for the benchmark; no real vulnerability.',
                'severity': 'medium', 'references': [{'url': 'https://example.com/advisory'}],
                'vulnerabilities': [{'first_patched_version': {'identifier': '9.0.0'}}]},
            'security_vulnerability': {'severity': severity, 'vulnerable_version_range': '< 2.0.1' if number == 7 else '<= 1.4.0',
                                       'first_patched_version': {'identifier': patch} if patch else None}}


ALERTS = [resource(7, 'critical', '@example/parser', 'npm', '2.0.1'), resource(8, 'high', 'example.com/library', 'go', None)]
FIELDS = ['number', 'state', 'package', 'ecosystem', 'manifest_path', 'scope', 'severity', 'vulnerable_version_range', 'first_patched_version']
REFERENCE = {'count': 2, 'alerts': [
    {'number': 7, 'state': 'open', 'package': '@example/parser', 'ecosystem': 'npm', 'manifest_path': 'package-lock.json',
     'scope': 'runtime', 'severity': 'critical', 'vulnerable_version_range': '< 2.0.1', 'first_patched_version': '2.0.1'},
    {'number': 8, 'state': 'open', 'package': 'example.com/library', 'ecosystem': 'go', 'manifest_path': 'go.mod',
     'scope': 'runtime', 'severity': 'high', 'vulnerable_version_range': '<= 1.4.0', 'first_patched_version': None}],
    'detail': {'number': 7, 'description': ALERTS[0]['security_advisory']['description'], 'references': ['https://example.com/advisory']}}


class Handler(core.Handler):
    def respond(self):
        parsed = urlsplit(self.path)
        base = '/repos/' + core.REPO + '/dependabot/alerts'
        if parsed.path not in (base, base + '/7'):
            return super().respond()
        query = parse_qs(parsed.query)
        status, link = 200, None
        if self.command != 'GET':
            status, value = 405, {'message': 'read only'}
        elif parsed.path.endswith('/7'):
            value = ALERTS[0]
        elif query.get('state') != ['open'] or query.get('severity') != ['high,critical']:
            status, value = 422, {'message': 'request open high,critical alerts'}
        elif not query.get('after'):
            value = [ALERTS[0]]
            # Forced small pages exercise cursor handling without huge fixture output.
            link = f'<http://127.0.0.1:{self.server.server_port}{base}?state=open&severity=high%2Ccritical&per_page=100&after=fixture-next>; rel="next"'
        elif query.get('after') == ['fixture-next']:
            value = [ALERTS[1]]
        else:
            status, value = 422, {'message': 'invalid fixture cursor'}
        data = json.dumps(value, separators=(',', ':')).encode()
        self.server.accesses.append({'method': self.command, 'endpoint': self.path, 'payload': {}, 'status': status, 'response_bytes': len(data)})
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        if link:
            self.send_header('Link', link)
        self.end_headers()
        self.wfile.write(data)


class Backend(ThreadingHTTPServer):
    def __init__(self, root):
        self.root, self.task, self.accesses = root, TASK, []
        super().__init__(('127.0.0.1', 0), Handler)


COMMANDS = [['tools', 'github', 'dependabot', 'list', '--severity', 'high,critical', '--json'],
            ['tools', 'github', 'dependabot', 'view', '--number', '7', '--json']]
GH_GUIDE = '''Use real gh api, with its transport redirected to the local fixture. Fetch all pages:
gh api 'repos/fixture/command-benchmark/dependabot/alerts?state=open&severity=high%2Ccritical&per_page=100&sort=created&direction=desc' --paginate --jq '.[]'
Read alert 7 detail with gh api repos/fixture/command-benchmark/dependabot/alerts/7.
The installed gh prints one compact JSON object per alert with this jq expression; collect these in response order. This gh version does not support --slurp. Return count and alerts projected to number,state,package,ecosystem,manifest_path,scope,severity,vulnerable_version_range,first_patched_version. Package/ecosystem come from dependency.package, manifest_path/scope from dependency, severity/range/patch from security_vulnerability. Patch is first_patched_version.identifier or null; do not use the advisory's other release-line patch. Detail contains number, security_advisory.description and reference URL strings. jq projections and batching are allowed. Python 3 is available as python3; python is not installed. No prebuilt helper is provided.
'''
TOOLS_GUIDE = '''Use the production CLI:
tools github dependabot list --severity high,critical --json
tools github dependabot view --number 7 --json
Return count and list alerts projected to number,state,package,ecosystem,manifest_path,scope,severity,vulnerable_version_range,first_patched_version; preserve null patches. Return detail with number,description,references from the view's alert. List fetches all pages internally. Batching and local filters are allowed; do not invoke gh.
'''


def prompt(method):
    return core.SHARED.replace('All collections fit one page.', 'Dependabot list has two cursor pages. Fetch both.') + '''
Task: list all open high/critical Dependabot alerts, then view alert 7. Return count, normalized alerts and detail. This is synthetic read-only data; do not interpret it as a real security advisory or change dependencies.
''' + (GH_GUIDE if method == 'gh' else TOOLS_GUIDE)


def verify(root, server, task, before):
    after = core.snapshot(root, server)
    checks = {'read_only_state': after == before, 'api_read_only': all(a['method'] == 'GET' for a in server.accesses),
              'both_pages_read': any(parse_qs(urlsplit(a['endpoint']).query).get('after') == ['fixture-next'] for a in server.accesses),
              'detail_read': any(urlsplit(a['endpoint']).path.endswith('/dependabot/alerts/7') for a in server.accesses)}
    return {'correct': all(checks.values()), 'checks': checks, 'after': after}


def install_hooks():
    original_schema = core.schema
    core.expected = lambda task: REFERENCE
    core.verify = verify
    def output_schema(value):
        result = original_schema(value)
        if value == REFERENCE:
            result['properties']['alerts']['items']['properties']['first_patched_version'] = {'anyOf': [{'type': 'string'}, {'type': 'null'}]}
        return result
    core.schema = output_schema


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--run-models', action='store_true')
    parser.add_argument('--model', default='gpt-6.1-sol')
    parser.add_argument('--reasoning-effort', choices=('low', 'medium', 'high', 'xhigh', 'max'), default='high')
    args = parser.parse_args()
    root = args.root.resolve()
    if root.exists() or root == Path('/tmp') or not root.is_relative_to(Path('/tmp')):
        parser.error('use a fresh dedicated /tmp directory')
    root.mkdir(parents=True)
    repo_root = Path(__file__).resolve().parents[2]
    core.cleanup.build_interface(root, repo_root)
    server = Backend(root)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    install_hooks()
    try:
        before, _ = core.setup(root, server, TASK)
        directory = root / 'preflight'
        directory.mkdir()
        env = core.environment(root, server, directory, TASK)
        results = []
        for command in COMMANDS:
            result = subprocess.run(['codex', 'sandbox', '--permission-profile', 'command_benchmark', *core.permission_args(root),
                                     '--cd', str(root / 'workspace'), '--', *command], env=env, capture_output=True, text=True, check=True)
            results.append(json.loads(result.stdout))
        actual = {'count': results[0]['count'], 'alerts': [{key: a[key] for key in FIELDS} for a in results[0]['alerts']],
                  'detail': {key: results[1]['alert'][key] for key in REFERENCE['detail']}}
        verification = verify(root, server, TASK, before)
        assert actual == REFERENCE and verification['correct'], (actual, verification)
        # Exercise the real gh cursor transport independently before any model calls.
        gh = subprocess.run(['gh', 'api', 'repos/' + core.REPO + '/dependabot/alerts?state=open&severity=high%2Ccritical&per_page=100', '--paginate', '--jq', '.[]'],
                            cwd=root / 'workspace', env=env, capture_output=True, text=True, check=True)
        assert [json.loads(line) for line in gh.stdout.splitlines()] == ALERTS
        preflight = {'correct': True, 'native_model_calls': 0, 'actual': actual, 'checks': verification['checks'], 'real_gh_cursor_pages': 2}
        core.save(root / 'preflight.json', preflight)
        protocol = {'measured_at_utc': datetime.now(timezone.utc).isoformat(), 'model': args.model, 'reasoning_effort': args.reasoning_effort,
                    'repetitions_per_method': 3, 'order': core.ORDER, 'max_model_calls': 6, 'fresh_sessions': True, 'cache_controlled': False,
                    'usage_source': 'codex exec --json turn.completed.usage; input + output, including cached input',
                    'expected': REFERENCE, 'synthetic_resources': ALERTS, 'forced_cursor_pages': 2,
                    'source_sha256': {str(p.relative_to(repo_root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in
                        [Path(__file__), Path(core.__file__), Path(core.cleanup.__file__), *sorted((repo_root / 'internal').rglob('*.go'))] if not p.name.endswith('_test.go')},
                    'source_revision': subprocess.check_output([core.cleanup.REAL_GIT, 'rev-parse', 'HEAD'], cwd=repo_root, text=True).strip(),
                    'prompts': {method: prompt(method) for method in ('gh', 'tools')},
                    'transport': 'Production CLI and real gh use a synthetic local HTTP API; read-only disposable Git repository.',
                    'limitations': ['Synthetic tiny forced cursor pages, not live GitHub alerts or production volume.', 'Cache uncontrolled; no monetary savings claim.', 'Combined list + view task, not independent per-subcommand measurements.']}
        core.save(root / 'protocol.json', protocol)
        print(json.dumps({'event': 'preflight_passed', 'task': TASK}), flush=True)
        records = []
        if args.run_models:
            for index, method in enumerate(core.ORDER, 1):
                print(json.dumps({'event': 'trial_started', 'task': TASK, 'index': index, 'method': method}), flush=True)
                row = core.run_trial(root, [server], TASK, index, method, prompt(method), args.model, args.reasoning_effort)
                records.append(row)
                core.save(root / 'results.json', records)
                print(json.dumps({'event': 'trial_completed', 'index': index, 'method': method, 'correct': row['correct'], 'usage': row['usage']}), flush=True)
            report = {'protocol': protocol, 'preflight': preflight, 'summary': core.cleanup.summarize(records), 'trials': records,
                      'correctness': {'passed': sum(r['correct'] for r in records), 'total': len(records)}}
            core.save(root / 'report.json', report)
            print(json.dumps({'event': 'experiment_completed', 'correctness': report['correctness']}), flush=True)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


if __name__ == '__main__':
    main()
