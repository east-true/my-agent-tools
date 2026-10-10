#!/usr/bin/env python3
"""고정된 모든 시도를 공개하며 최신 결과 선택으로 과거 실험을 덮어쓰지 않는다."""
import argparse
import copy
import json
from pathlib import Path
from publish_github_study import archive
from run_usage_benchmark import sha
import run_github_study as gh
from usage_benchmark_docs import render

def audit(root):
    report = json.loads((root / 'report.json').read_text())
    protocol, rows = (report['protocol'], report['trials'])
    gh.validate_frozen(root, protocol)
    assert sha((root / 'main-overlay.go').read_bytes()) == protocol['overlay_sha256']
    if 'helper_sha256' in protocol:
        assert sha((root / 'bin/edit-integer.py').read_bytes()) == protocol['helper_sha256']
    assert len(rows) == protocol['max_model_calls'] == len(protocol['schedule'])
    assert {(r['task'], r['method'], r['repetition']) for r in rows} == {(r['task'], r['method'], r['repetition']) for r in protocol['schedule']}
    for row in rows:
        directory = root / 'runs' / row['task'] / f'{row['index']:02d}-{row['method']}'
        raw = (directory / 'events.jsonl').read_bytes()
        events = [json.loads(l) for l in raw.splitlines() if l.strip()]
        assert sha(raw) == row['events_sha256']
        assert [e['usage'] for e in events if e.get('type') == 'turn.completed'] == [row['usage']]
        assert row['actual'] == json.loads((directory / 'answer.json').read_text())
        assert '--model' not in row['execution_args'] and '--ignore-user-config' not in row['execution_args']
        assert not any(('model_reasoning_effort=' in arg for arg in row['execution_args']))
        usage = row['usage']
        assert usage is not None
        assert row['input_plus_output'] == usage['input_tokens'] + usage['output_tokens']
        assert row['uncached_plus_output'] == usage['input_tokens'] - usage['cached_input_tokens'] + usage['output_tokens']
        state = json.loads((directory / 'state-verification.json').read_text())
        assert state['checks'] == row['state_checks'] and state['correct'] == row['state_correct']
        calls = directory / 'cli-access.jsonl'
        actual = [json.loads(l) for l in calls.read_text().splitlines()] if calls.exists() else []
        assert actual == row['cli_invocations'] and len(actual) == row['cli_calls']
        assert row['api_access'] == json.loads((directory / 'api-access.json').read_text())
    return report

def publish(repo, root, name, label, latest):
    report = audit(root)
    protocol = report['protocol']
    out = repo / 'docs/benchmarks/usage/data'
    out.mkdir(parents=True, exist_ok=True)
    source = [('source/' + p, (root / 'source' / p).read_bytes()) for p in protocol['source_hashes']]
    source += [('harness/' + p, (root / 'harness' / p).read_bytes()) for p in protocol['runner_hashes']]
    source += [(p, (root / p).read_bytes()) for p in ('main-overlay.go', 'overlay.json')]
    archive(out / f'{name}-source.tar.gz', source)
    evidence = []
    for folder in ('runs', 'preflight'):
        evidence += [(str(p.relative_to(root)), p.read_bytes()) for p in sorted((root / folder).rglob('*')) if p.is_file()]
    archive(out / f'{name}-events.tar.gz', evidence)
    public = copy.deepcopy(report)
    public['protocol']['source_manifest'] = [{'path': p, 'sha256': value} for p, value in public['protocol'].pop('source_hashes').items()]
    public['evidence'] = {'sha256': {p.name: sha(p.read_bytes()) for p in (out / f'{name}-source.tar.gz', out / f'{name}-events.tar.gz')}}
    file = name + '.json'
    target = out / file
    encoded = json.dumps(public, ensure_ascii=False, indent=2) + '\n'
    if target.exists() and target.read_text() != encoded:
        raise RuntimeError('published cohort is immutable; choose a new name')
    target.write_text(encoded)
    native = {task: [json.loads((root / 'preflight' / task / str(rep) / 'result.json').read_text()) for rep in (1, 2, 3)] for task in protocol['tasks']}
    (out / f'{name}-native.json').write_text(json.dumps(native, ensure_ascii=False, indent=2) + '\n')
    path = out / 'index.json'
    index = json.loads(path.read_text()) if path.exists() else {'cohorts': {}, 'latest': {}}
    index['cohorts'][name] = {'file': file, 'label': label, 'trials': len(report['trials'])}
    for task in latest:
        if task not in protocol['tasks']:
            raise RuntimeError('latest selector outside measured scope')
        index['latest'][task] = name
    path.write_text(json.dumps(index, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps({'published': name, 'trials': len(report['trials']), 'correct': sum((r['correct'] for r in report['trials'])), 'new_model_calls': 0}))

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--root', type=Path)
    group.add_argument('--render-only', action='store_true')
    parser.add_argument('--name')
    parser.add_argument('--label')
    parser.add_argument('--latest', nargs='*', default=[])
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[2]
    if args.root:
        if not args.name or not args.label or (not args.name.replace('-', '').isalnum()):
            parser.error('safe cohort name and label required')
        publish(repo, args.root.resolve(), args.name, args.label, args.latest)
    render(repo)
if __name__ == '__main__':
    main()
