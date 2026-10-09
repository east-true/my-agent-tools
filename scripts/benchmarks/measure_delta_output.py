#!/usr/bin/env python3
"""Compare delta output bytes on a pinned snapshot; makes no model/API calls."""
import argparse
import hashlib
import json
import subprocess
from pathlib import Path

from github_candidates import delta


def encoded(value):
    return (json.dumps(value, ensure_ascii=False, separators=(',', ':')) + '\n').encode()


def measure(snapshot, since, head):
    data = json.loads(snapshot)
    # Reconstruct the historical projection, including its unconditional patch
    # and null previous_filename fields. Preserve historical token results.
    legacy = {'since': since, 'head': head, 'files': [
        {key: file[key] for key in ('filename', 'status', 'additions', 'deletions')}
        | {'patch': file.get('patch'), 'previous_filename': file.get('previous_filename')}
        for file in data.get('files', [])]}
    improved = delta(data, since, head)
    # A direct gh --jq projection can request precisely the same metadata.
    expression = '{since:$since,head:$head,files:[.files|sort_by(.filename)[]|{filename,status,additions,deletions}+(if .previous_filename != null then {previous_filename} else {} end)]}'
    direct_bytes = subprocess.run(['jq', '-c', '--arg', 'since', since, '--arg', 'head', head, expression],
                                  input=snapshot, capture_output=True, check=True).stdout
    direct = json.loads(direct_bytes)
    assert improved == direct
    before, after = len(encoded(legacy)), len(encoded(improved))
    return {'measurement': 'deterministic_output_bytes', 'model_calls': 0,
            'snapshot_sha256': hashlib.sha256(snapshot).hexdigest(),
            'implementation_sha256': hashlib.sha256(Path(__file__).with_name('github_candidates.py').read_bytes()).hexdigest(),
            'since': since, 'head': head, 'files': len(improved['files']),
            'legacy_output_bytes': before, 'metadata_output_bytes': after,
            'direct_metadata_output_bytes': len(direct_bytes),
            'direct_jq_expression': expression,
            'metadata_equal_to_direct_projection': True,
            'output_byte_reduction_percent': round(100 * (before - after) / before, 2),
            'include_patch_output_bytes': len(encoded(delta(data, since, head, include_patch=True))),
            'token_remeasurement': False,
            'limitation': 'Output bytes measure payload size, not total agent tokens or price. Direct gh filtering can emit identical metadata.'}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--snapshot', required=True, type=Path)
    parser.add_argument('--since', required=True)
    parser.add_argument('--head', required=True)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    result = measure(args.snapshot.read_bytes(), args.since, args.head)
    args.output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False, separators=(',', ':')))
