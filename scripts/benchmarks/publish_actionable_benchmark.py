#!/usr/bin/env python3
"""Audit every frozen actionable trial, publish raw evidence, render command pages."""
import argparse
import copy
import json
from pathlib import Path

import run_actionable_benchmark as study
from publish_github_study import archive
from actionable_benchmark_docs import render


def reassess_authored_input(root, records):
    """Re-audit an overly narrow fixture rule; never replace a model run."""
    corrected = copy.deepcopy(records)
    changes = []
    for row in corrected:
        if row['correct'] or row['task'] != 'apply-groups' or not row['answer_correct']:
            continue
        checks = row['state_checks']
        if [k for k, v in checks.items() if not v] != ['no_unexpected_files']:
            continue
        directory = root/'runs'/row['task']/f"{row['index']:02d}-{row['method']}"
        original_state = json.loads((directory/'state-verification.json').read_text(encoding='utf-8'))
        # This retained run authored the exact shared spec below. The command
        # and archived file digest establish its bytes; arbitrary files are not
        # admitted and no task or model output is rerun or rewritten.
        paths = [f'src/config_{i:02d}.txt' for i in range(12)]
        spec = {'version': 1, 'groups': [{'paths': paths, 'replacements': study.replacements()}]}
        encoded = json.dumps(spec, separators=(',', ':')).encode()
        authored = original_state['after'].get('batch-spec.json')
        if not authored or authored['sha256'] != study.digest(encoded) or not study.valid_authored_spec(spec, paths):
            continue
        commands = '\n'.join(row['commands'])
        if 'batch-spec.json' not in commands or '--spec batch-spec.json' not in commands:
            continue
        checks['no_unexpected_files'] = True
        row['state_correct'] = row['correct'] = True
        changes.append({'task': row['task'], 'method': row['method'], 'repetition': row['repetition'],
            'original_correct': False, 'reassessed_correct': True, 'model_replacements': 0,
            'reason': 'The prompt authorizes input authoring and FILE_OR_STDIN, but the frozen verifier permitted only the literal spec.json filename.',
            'authored_input_path': 'batch-spec.json', 'authored_input_sha256': authored['sha256'],
            'reconstructed_authored_input': spec, 'original_state_verification_sha256': study.digest((directory/'state-verification.json').read_bytes())})
    return corrected, changes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--root', type=Path)
    group.add_argument('--render-only', action='store_true')
    parser.add_argument('--before-root', type=Path, help='optional frozen root containing the before CLI used in native probes')
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[2]
    if args.render_only:
        render(repo)
        return
    root = args.root.resolve()
    report = json.loads((root/'report.json').read_text(encoding='utf-8'))
    protocol, records = report['protocol'], report['trials']
    study.gh.validate_frozen(root, protocol)
    assert protocol['schedule'] == study.schedule() and len(records) == protocol['max_model_calls'] == 24
    assert {(r['task'], r['method'], r['repetition']) for r in records} == {(r['task'], r['method'], r['repetition']) for r in protocol['schedule']}
    assert report['summary'] == study.summarize(records)
    for row in records:
        directory = root/'runs'/row['task']/f"{row['index']:02d}-{row['method']}"
        raw = (directory/'events.jsonl').read_bytes()
        events = [json.loads(line) for line in raw.splitlines() if line.strip()]
        assert study.digest(raw) == row['events_sha256']
        assert [e['usage'] for e in events if e.get('type') == 'turn.completed'] == [row['usage']]
        assert row['actual'] == json.loads((directory/'answer.json').read_text(encoding='utf-8'))
        args_used = row['execution_args']
        assert '--model' not in args_used and '--ignore-user-config' not in args_used and not any('model_reasoning_effort=' in a for a in args_used)
        usage = row['usage']
        assert row['input_plus_output'] == usage['input_tokens'] + usage['output_tokens']
        assert row['uncached_plus_output'] == usage['input_tokens'] - usage['cached_input_tokens'] + usage['output_tokens']
        state = json.loads((directory/'state-verification.json').read_text(encoding='utf-8'))
        assert row['state_checks'] == state['checks'] and row['state_correct'] == state['correct']
    out = repo/'docs/benchmarks/actionable/data'
    out.mkdir(parents=True, exist_ok=True)
    sources = [('source/'+path, (root/'source'/path).read_bytes()) for path in protocol['source_hashes']]
    sources += [('harness/'+path, (root/'harness'/path).read_bytes()) for path in protocol['runner_hashes']]
    sources += [(name, (root/name).read_bytes()) for name in ('main-overlay.go', 'overlay.json')]
    before_manifest = None
    if args.before_root:
        before_root = args.before_root.resolve()
        assert study.digest((before_root/'bin/tools').read_bytes()) == protocol['before_cli_sha256']
        before_hashes = study.gh.source_hashes(before_root/'source')
        before_manifest = [{'path': name, 'sha256': sha} for name, sha in before_hashes.items()]
        sources += [('before/source/'+name, (before_root/'source'/name).read_bytes()) for name in before_hashes]
        sources += [('before/'+name, (before_root/name).read_bytes()) for name in ('main-overlay.go', 'overlay.json')]
    archive(out/'study-source.tar.gz', sources)
    evidence = []
    for directory in ('runs', 'preflight'):
        evidence += [(str(p.relative_to(root)), p.read_bytes()) for p in sorted((root/directory).rglob('*')) if p.is_file()]
    archive(out/'study-events.tar.gz', evidence)
    public = copy.deepcopy(report)
    public['protocol']['source_manifest'] = [{'path': path, 'sha256': sha} for path, sha in public['protocol'].pop('source_hashes').items()]
    if before_manifest:
        public['protocol']['before_source_manifest'] = before_manifest
    public['evidence'] = {'sha256': {p.name: study.digest(p.read_bytes()) for p in (out/'study-source.tar.gz', out/'study-events.tar.gz')}}
    corrected, deviations = reassess_authored_input(root, records)
    public['protocol_deviations'] = deviations
    public['reassessed_trials'] = corrected
    public['reassessed_summary'] = study.summarize(corrected)
    native = {task: [json.loads((root/'preflight'/task/str(rep)/'native.json').read_text(encoding='utf-8')) for rep in (1, 2, 3)] for task in study.TASKS}
    (out/'study.json').write_text(json.dumps(public, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
    (out/'native.json').write_text(json.dumps(native, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
    render(repo)
    print(json.dumps({'published_trials': len(records), 'original_correct': sum(r['correct'] for r in records), 'reassessed_correct': sum(r['correct'] for r in corrected), 'new_model_calls': 0}))


if __name__ == '__main__':
    main()
