#!/usr/bin/env python3
"""Offline validation prototypes. These commands are not part of tools github."""
import argparse
import collections
import json
import re
from pathlib import Path

ANSI = re.compile(r'\x1b\[[0-9;]*m')
TIME = re.compile(r'^\d{4}-\d\d-\d\dT\S+\s*')
DIAGNOSTIC = re.compile(
    r'(?i)(^--- FAIL:|^FAIL(?:\s|$)|^panic:|^fatal:|^Traceback|'
    r'^.*\.(?:go|py|ts|tsx|js|rs|c|cpp):\d+(?::\d+)?:\s|'
    r'^.*(?:AssertionError|ModuleNotFoundError|ImportError|SyntaxError|TypeError|ReferenceError):|'
    r'^npm ERR!|^##\[error\]|^Error:)'
)


def parameter_count(signature):
    """Count top-level Go parameters, rejecting malformed or truncated input."""
    signature = signature.strip()
    if not signature.startswith('('):
        raise ValueError('Expected an enclosing parameter list')
    stack = []
    quote = None
    escaped = False
    count = 0
    start = 1
    pairs = {')': '(', ']': '[', '}': '{'}
    for i, character in enumerate(signature):
        if quote:
            if escaped:
                escaped = False
            elif character == '\\' and quote != '`':
                escaped = True
            elif character == quote:
                quote = None
            continue
        if character in ('"', "'", '`'):
            quote = character
        elif character in '([{':
            stack.append(character)
        elif character in ')]}':
            if not stack or stack.pop() != pairs[character]:
                raise ValueError('Unbalanced parameter list')
            if not stack:
                if signature[i + 1:].strip():
                    raise ValueError('Unexpected text after the parameter list')
                if signature[start:i].strip():
                    count += 1
                elif count:
                    raise ValueError('Empty or trailing parameter')
                return count
        elif character == ',' and len(stack) == 1:
            if not signature[start:i].strip():
                raise ValueError('Empty parameter')
            count += 1
            start = i + 1
    raise ValueError('Truncated parameter list')


def go_ci_facts(text):
    """Extract supported Go compiler facts without calling a model.

    This is syntax extraction, not a general failure diagnosis. Unsupported
    errors or signatures return the original evidence for further inspection.
    """
    if not text.strip():
        raise ValueError('Empty CI log cannot establish compiler facts')
    diagnostics = []
    expected_arguments = {}
    missing_methods = set()
    exit_codes = set()
    unsupported = []
    current_call = None
    location = re.compile(r'^(?:##\[error\])?(.+\.go):(\d+):(\d+):\s*(.+)$')
    for raw in text.splitlines():
        clean = ANSI.sub('', raw).lstrip('\ufeff')
        parts = clean.split('\t', 2)
        message = clean if TIME.match(clean) or len(parts) != 3 else parts[2]
        message = TIME.sub('', message.lstrip('\ufeff')).strip()
        diagnostic = location.match(message)
        exit_match = re.search(r'Process completed with exit code (-?\d+)\.', message)
        if diagnostic:
            path, line, column, detail = diagnostic.groups()
            diagnostics.append({'path': path, 'line': int(line), 'column': int(column), 'message': detail})
            call = re.search(r'in call to ([A-Za-z_][A-Za-z0-9_./]*)', detail)
            current_call = call.group(1) if call else None
        elif message.startswith('want '):
            if current_call:
                try:
                    value = parameter_count(message.removeprefix('want ').strip())
                    if current_call in expected_arguments and expected_arguments[current_call] != value:
                        unsupported.append(message)
                    else:
                        expected_arguments[current_call] = value
                except ValueError:
                    unsupported.append(message)
            else:
                unsupported.append(message)
        elif exit_match:
            exit_codes.add(int(exit_match.group(1)))
        elif DIAGNOSTIC.search(message) and not re.match(r'^FAIL(?:\s|$)', message):
            unsupported.append(message)
        missing_methods.update(re.findall(r'missing method ([A-Za-z_][A-Za-z0-9_]*)', message))
    unique = {(d['path'], d['line'], d['column'], d['message']): d for d in diagnostics}
    supported = bool(diagnostics) and len(exit_codes) == 1 and not unsupported
    result = {'status': 'recognized' if supported else 'needs_context',
              'diagnostics': [unique[key] for key in sorted(unique)],
              'expected_arguments': expected_arguments,
              'missing_interface_methods': sorted(missing_methods),
              'exit_codes': sorted(exit_codes), 'unsupported_evidence': unsupported}
    if not supported:
        result['fallback_log'] = text
    return result


def ci(text, context=5):
    """Retain diagnostic windows and every unknown failed step in full."""
    if not text.strip():
        raise ValueError('Empty CI log cannot establish the evidence for a failed run')
    grouped = collections.OrderedDict()
    for raw in text.splitlines():
        parts = raw.split('\t', 2)
        job, step, message = parts if len(parts) == 3 else ('unknown', 'unknown', raw)
        message = TIME.sub('', ANSI.sub('', message).lstrip('\ufeff'))
        grouped.setdefault((job, step), []).append(message)
    blocks = collections.OrderedDict()
    signatures = set()
    fallbacks = []
    for target, lines in grouped.items():
        anchors = [i for i, line in enumerate(lines) if DIAGNOSTIC.search(line)]
        signatures.update(lines[i] for i in anchors)
        intervals = []
        for i in anchors:
            start, end = max(0, i - context), min(len(lines), i + context + 1)
            if intervals and start <= intervals[-1][1]:
                intervals[-1][1] = max(intervals[-1][1], end)
            else:
                intervals.append([start, end])
        if not anchors:
            intervals = [[0, len(lines)]]
            fallbacks.append({'job': target[0], 'step': target[1]})
        for start, end in intervals:
            excerpt = tuple(lines[start:end])
            block = blocks.setdefault(excerpt, {'lines': list(excerpt), 'occurrences': []})
            block['occurrences'].append({'job': target[0], 'step': target[1],
                                         'start_line': start + 1, 'end_line': end})
    return {'failure_signatures': sorted(signatures),
            'failed_jobs': sorted({job for job, step in grouped}),
            'blocks': list(blocks.values()), 'fallback_steps': fallbacks,
            'raw_lines': len(text.splitlines()),
            'unique_excerpt_lines': sum(len(b['lines']) for b in blocks.values()),
            'complete_diagnosis': not fallbacks,
            'notice': 'Pattern-based extraction, not a root-cause diagnosis. Nonmatching failed steps are retained in full.'}


def reviews(data):
    connection = data['data']['repository']['pullRequest']['reviewThreads']
    if connection['pageInfo']['hasNextPage']:
        raise ValueError('Review thread snapshot is incomplete; fetch every page first')
    threads = []
    for thread in connection['nodes']:
        if thread['isResolved']:
            continue
        if thread['comments']['pageInfo']['hasNextPage']:
            raise ValueError('Review comment snapshot is incomplete; fetch every page first')
        # Outdated location does not mean that the requested work is resolved.
        threads.append({'id': thread['id'], 'path': thread['path'], 'line': thread['line'],
                        'is_outdated': thread['isOutdated'],
                        'comments': [c['body'] for c in thread['comments']['nodes']]})
    return {'threads': threads}


def delta(data, since, head, include_patch=False):
    if data.get('status') not in ('ahead', 'identical'):
        raise ValueError('Baseline is not an ancestor of head; a reset or full refresh is required')
    if data['base_commit']['sha'] != since:
        raise ValueError('Response does not match the requested baseline')
    commits = data.get('commits', [])
    if data.get('total_commits', 0) != len(commits):
        raise ValueError('Commit snapshot is incomplete; fetch every page first')
    actual_head = commits[-1]['sha'] if commits else since
    if actual_head != head:
        raise ValueError('Response does not match the requested head')
    # GitHub compare reports at most 300 changed files. Do not assume an exact
    # 300-file response is complete; use a local Git diff or another full source.
    if len(data.get('files', [])) >= 300:
        raise ValueError('Potential compare file limit; obtain a complete local Git diff')
    files = []
    for file in sorted(data.get('files', []), key=lambda item: item['filename']):
        item = {key: file[key] for key in ('filename', 'status', 'additions', 'deletions')}
        if file.get('previous_filename') is not None:
            item['previous_filename'] = file['previous_filename']
        if include_patch:
            item['patch'] = file.get('patch')
        files.append(item)
    return {'since': since, 'head': head, 'files': files}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('kind', choices=('ci', 'ci-facts', 'reviews', 'delta'))
    parser.add_argument('snapshot', type=Path)
    parser.add_argument('--since')
    parser.add_argument('--head')
    parser.add_argument('--include-patch', action='store_true', help='delta: include patches only when needed')
    args = parser.parse_args()
    text = args.snapshot.read_text()
    if args.kind == 'ci':
        result = ci(text)
    elif args.kind == 'ci-facts':
        result = go_ci_facts(text)
    elif args.kind == 'reviews':
        result = reviews(json.loads(text))
    else:
        if not args.since or not args.head:
            parser.error('delta requires --since and --head')
        result = delta(json.loads(text), args.since, args.head, include_patch=args.include_patch)
    print(json.dumps(result, ensure_ascii=False, separators=(',', ':')))
