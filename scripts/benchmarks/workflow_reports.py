#!/usr/bin/env python3
"""Structured Go CI reports and explicit acknowledgement for experiment workflows."""
import argparse
import hashlib
import json
import os
import re
import subprocess
import tempfile
from pathlib import Path


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def stable_output(text):
    return re.sub(r'(--- (?:PASS|FAIL|SKIP): [^\n]+) \([0-9.]+s\)', r'\1', text).rstrip()


def report_from_events(text, exit_code, *, repo, revision, job, run_id, stderr=''):
    outputs, failed, terminals, test_terminals, errors = {}, set(), {}, {}, []
    known = {'start', 'run', 'pause', 'cont', 'output', 'pass', 'fail', 'skip', 'build-output', 'build-fail'}
    for number, line in enumerate(text.splitlines(), 1):
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            errors.append(f'Invalid JSON event at line {number}')
            continue
        if not isinstance(event, dict) or event.get('Action') not in known:
            errors.append(f'Unsupported event at line {number}')
            continue
        package = event.get('Package') or event.get('ImportPath')
        if not package:
            errors.append(f'Missing package at line {number}')
            continue
        test = event.get('Test', '')
        key = (package, test)
        outputs.setdefault(key, [])
        if event.get('Action') in ('output', 'build-output'):
            if not isinstance(event.get('Output'), str):
                errors.append(f'Missing output at line {number}')
            else:
                outputs[key].append(event['Output'])
        if event['Action'] in ('pass', 'fail', 'skip') and not test:
            terminals[package] = event['Action']
        elif event['Action'] in ('pass', 'fail', 'skip'):
            test_terminals[key] = event['Action']
        if event['Action'] in ('fail', 'build-fail'):
            failed.add(key)
    failures = []
    for package, test in sorted(failed):
        chunks = list(outputs.get((package, test), []))
        if not test:
            # A panic may terminate a package without a per-test fail event.
            for key, lines in outputs.items():
                if key[0] == package and key[1] and key not in failed and test_terminals.get(key) not in ('pass', 'skip'):
                    chunks.extend(lines)
            summary_only = all(not line.strip() or re.match(r'^FAIL(?:\s|$)', line.strip()) for line in ''.join(chunks).splitlines())
            if summary_only and any(p == package and t for p, t in failed):
                continue
        output = stable_output(''.join(chunks))
        if not output:
            errors.append(f'Missing failure evidence for {package}/{test}')
        identity = package + '::' + (test or '<package>')
        failures.append({'id': identity, 'package': package, 'test': test or None,
                         'output': output, 'fingerprint': digest([identity, output])})
    packages = {p for p, t in outputs}
    if not packages or any(p not in terminals and (p, '') not in failed for p in packages):
        errors.append('Missing terminal package results')
    if (exit_code == 0 and failures) or (exit_code != 0 and not failures):
        errors.append('Process result contradicts the reported failures')
    complete = not errors
    report = {'schema_version': 1, 'repo': repo, 'revision': revision, 'job': job,
              'run_id': str(run_id), 'status': ('passed' if exit_code == 0 else 'failed') if complete else 'incomplete',
              'complete': complete, 'exit_code': exit_code, 'failures': failures, 'collection_errors': errors}
    if not complete:
        report['fallback'] = {'events': text, 'stderr': stderr}
    return report


def report_digest(report):
    validate_report(report)
    return digest(report)


def validate_report(report):
    if report.get('schema_version') != 1:
        raise ValueError('Unsupported report version')
    for field in ('repo', 'revision', 'job', 'run_id'):
        if not isinstance(report.get(field), str) or not report[field]:
            raise ValueError('Missing report identity: ' + field)
    if report.get('status') not in ('passed', 'failed', 'incomplete'):
        raise ValueError('Invalid report status')
    if not isinstance(report.get('complete'), bool) or not isinstance(report.get('failures'), list):
        raise ValueError('Invalid report completeness or failures')
    ids = []
    for failure in report['failures']:
        if not isinstance(failure.get('id'), str) or not isinstance(failure.get('output'), str):
            raise ValueError('Invalid failure evidence')
        if failure.get('fingerprint') != digest([failure['id'], failure['output']]):
            raise ValueError('Failure evidence checksum mismatch')
        ids.append(failure['id'])
    if len(ids) != len(set(ids)):
        raise ValueError('Duplicate failure identities')
    if report['complete']:
        if report['status'] == 'incomplete' or report.get('collection_errors'):
            raise ValueError('Report contradicts its completeness claim')
        if (report['status'] == 'passed') != (report.get('exit_code') == 0):
            raise ValueError('Report contradicts its exit status')
        if (report['status'] == 'failed') != bool(report['failures']):
            raise ValueError('Report contradicts its failure status')


def empty_state():
    return {'schema_version': 1, 'scopes': {}}


def state_scope(report):
    return digest([report['repo'], report['job']])


def prepare(report, state, expected_revision=None):
    """Read-only decision: observing a report does not mark its work handled."""
    validate_report(report)
    if expected_revision is not None and report['revision'] != expected_revision:
        raise ValueError('Report revision does not match the expected CI revision')
    if state.get('schema_version') != 1 or not isinstance(state.get('scopes'), dict):
        raise ValueError('Invalid state; reset explicitly instead of hiding work')
    scope = state['scopes'].get(state_scope(report), {})
    previous = scope.get('last_report', {})
    before = {f['id']: f for f in previous.get('failures', [])}
    after = {f['id']: f for f in report['failures']}
    delta = {'added': [after[i] for i in sorted(after.keys() - before.keys())],
             'changed': [after[i] for i in sorted(after.keys() & before.keys())
                         if after[i]['fingerprint'] != before[i]['fingerprint']],
             'resolved': sorted(before.keys() - after.keys())}
    already_handled = report_digest(report) in scope.get('acknowledged', [])
    if not report['complete']:
        needed, reason = True, 'incomplete_report'
    elif already_handled:
        needed, reason = False, 'acknowledged_event'
        delta = {'added': [], 'changed': [], 'resolved': []}
    elif report['status'] == 'passed':
        needed, reason = False, 'checks_passed'
    else:
        needed, reason = True, 'unhandled_failure'
    return {'model_needed': needed, 'reason': reason,
            'revision_changed': previous.get('revision') != report['revision'],
            'delta': delta, 'report': report}


def acknowledge(report, state, *, verification_passed=False):
    validate_report(report)
    if not report['complete']:
        raise ValueError('Incomplete reports cannot be acknowledged')
    if report['status'] == 'failed' and not verification_passed:
        raise ValueError('Failure acknowledgement requires successful independent verification')
    result = json.loads(json.dumps(state))
    key = state_scope(report)
    scope = result['scopes'].setdefault(key, {'acknowledged': []})
    fingerprint = report_digest(report)
    if fingerprint not in scope['acknowledged']:
        scope['acknowledged'].append(fingerprint)
    scope['acknowledged'] = scope['acknowledged'][-128:]
    scope['last_report'] = report
    return result


def code_context(root, paths):
    root = Path(root).resolve()
    files = []
    for name in paths:
        path = (root / name).resolve()
        if not path.is_relative_to(root) or Path(name).is_absolute():
            raise ValueError('Context path escapes the checkout')
        text = path.read_text(encoding='utf-8')
        if len(text.encode()) > 65536:
            raise ValueError('Context file is too large; select a smaller explicit range')
        files.append({'path': name, 'content': text, 'sha256': hashlib.sha256(text.encode()).hexdigest()})
    return files


def apply_replacement(root, name, content, expected_sha256, allowed_paths):
    """Apply one model-proposed file only if its preimage still matches."""
    if name not in allowed_paths or not isinstance(content, str):
        raise ValueError('Replacement is outside the allowed change scope')
    root = Path(root).resolve()
    path = (root / name).resolve()
    if Path(name).is_absolute() or not path.is_relative_to(root):
        raise ValueError('Replacement path escapes the checkout')
    if hashlib.sha256(path.read_bytes()).hexdigest() != expected_sha256:
        raise ValueError('Source changed after context preparation; refresh the request')
    if len(content.encode('utf-8')) > 65536:
        raise ValueError('Replacement exceeds the supported file size')
    with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', dir=path.parent, delete=False) as file:
        temporary = Path(file.name)
        file.write(content)
    try:
        temporary.chmod(path.stat().st_mode & 0o777)
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def atomic_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', dir=path.parent, delete=False) as file:
        temporary = Path(file.name)
        json.dump(value, file, ensure_ascii=False, indent=2)
        file.write('\n')
    try:
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    collect = commands.add_parser('capture-go')
    collect.add_argument('--repo', required=True)
    collect.add_argument('--revision', required=True)
    collect.add_argument('--job', required=True)
    collect.add_argument('--run-id', required=True)
    collect.add_argument('--output', type=Path, required=True)
    inspect = commands.add_parser('prepare')
    inspect.add_argument('--report', type=Path, required=True)
    inspect.add_argument('--state', type=Path, required=True)
    inspect.add_argument('--context-root', type=Path)
    inspect.add_argument('--expected-revision')
    inspect.add_argument('--include', action='append', default=[])
    ack = commands.add_parser('ack')
    ack.add_argument('--report', type=Path, required=True)
    ack.add_argument('--state', type=Path, required=True)
    ack.add_argument('--verification-passed', action='store_true')
    args = parser.parse_args()
    if args.command == 'capture-go':
        result = subprocess.run(['go', 'test', '-json', '-count=1', './...'], capture_output=True,
                                text=True, encoding='utf-8', errors='replace')
        report = report_from_events(result.stdout, result.returncode, repo=args.repo,
                                    revision=args.revision, job=args.job, run_id=args.run_id, stderr=result.stderr)
        atomic_json(args.output, report)
        print(json.dumps({'report': str(args.output), 'status': report['status'], 'complete': report['complete']}))
        raise SystemExit(result.returncode or (0 if report['complete'] else 1))
    report = json.loads(args.report.read_text(encoding='utf-8'))
    state = json.loads(args.state.read_text(encoding='utf-8')) if args.state.exists() else empty_state()
    if args.command == 'prepare':
        result = prepare(report, state, args.expected_revision)
        if args.context_root:
            result['source_files'] = code_context(args.context_root, args.include)
        print(json.dumps(result, ensure_ascii=False, separators=(',', ':')))
    else:
        atomic_json(args.state, acknowledge(report, state, verification_passed=args.verification_passed))
