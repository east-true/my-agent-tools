#!/usr/bin/env python3
"""Compare frozen and current review output on the same fixture, without models."""
import argparse
import hashlib
import json
import shutil
import subprocess
import tarfile
import threading
from pathlib import Path
from datetime import datetime, timezone

import run_github_study as study


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,required=True)
    args=parser.parse_args();root=args.root.resolve()
    if root.exists() or root==Path('/tmp') or not root.is_relative_to('/tmp'):parser.error('fresh dedicated /tmp root required')
    repo=Path(__file__).resolve().parents[2];root.mkdir()
    reference=json.loads((repo/'docs/benchmarks/github/data/study.json').read_text())
    before=root/'before';after=root/'after'
    for stage in (before,after):(stage/'source').mkdir(parents=True)
    with tarfile.open(repo/'docs/benchmarks/github/data/study-source.tar.gz') as archive:
        for name,digest in reference['protocol']['source_hashes'].items():
            data=archive.extractfile('source/'+name).read()
            assert hashlib.sha256(data).hexdigest()==digest
            target=before/'source'/name;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(data)
    for name in ('go.mod','go.sum'):shutil.copy2(repo/name,after/'source'/name)
    for name in ('cmd','internal'):shutil.copytree(repo/name,after/'source'/name)
    for stage in (before,after):study.small.build(stage,stage/'source')
    server=study.core.Backend(root);server.RequestHandlerClass=study.Handler
    threading.Thread(target=server.serve_forever,daemon=True).start()
    try:
        snapshot,_=study.setup(root,server,'pr-reviews')
        server.interface_access_path=root/'interface-access.jsonl'
        env=study.suite.environment(root,server,root,'pr-reviews')
        env['PATH']=str(after/'bin')+':'+env['PATH']
        responses={}
        for encoding in ('o200k_base','cl100k_base'):
            for label,stage in (('before',before),('after',after)):
                command=[str(stage/'bin/tools'),'github','pr','reviews','--number','7','--json','--compact',
                         '--token-encoding',encoding,'--artifact-dir',str(root/'evidence')]
                result=subprocess.run(command,cwd=root/'workspace',env=env,capture_output=True,text=True,check=True)
                (root/f'{encoding}-{label}.json').write_text(result.stdout)
                value=json.loads(result.stdout)
                assert study.suite.normalize('pr-reviews',[value],server)==study.suite.expected('pr-reviews')
                responses[encoding+'-'+label]=value
                assert study.suite.verify(root,server,'pr-reviews',snapshot)['correct']
        for encoding in ('o200k_base','cl100k_base'):
            old,new=responses[encoding+'-before'],responses[encoding+'-after']
            assert old['evidence_sha256']==new['evidence_sha256']
            assert old['evidence_file']==new['evidence_file']
        subprocess.run(['go','test','./internal/cli','-run','^TestReviewMetadataOutputComparison$','-count=1','-v'],
                       cwd=repo,env=dict(env,TOOLS_REVIEW_METADATA_MEASUREMENT_DIR=str(root)),check=True)
        result=json.loads((root/'measurement.json').read_text())
        for row in result['measurements']:
            row['token_reduction_percent']=100*(1-row['after_tokens']/row['before_tokens'])
            row['byte_reduction_percent']=100*(1-row['after_bytes']/row['before_bytes'])
        result.update(scope='Serialized compact review JSON with same evidence reference, no trailing newline; not total agent task tokens',measured_on=datetime.now(timezone.utc).date().isoformat(),fixture='same 20-thread fixed workload as repeated pr-reviews',
                      before_cli_sha256=hashlib.sha256((before/'bin/tools').read_bytes()).hexdigest(),
                      after_cli_sha256=hashlib.sha256((after/'bin/tools').read_bytes()).hexdigest(),
                      exact_full_evidence_preserved=True,semantic_and_state_checks=True,
                      before_source_hashes=reference['protocol']['source_hashes'],
                      after_source_hashes=study.source_hashes(after/'source'),
                      response_sha256={name:hashlib.sha256((root/(name+'.json')).read_bytes()).hexdigest() for name in responses})
        (root/'measurement.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
        print(json.dumps({'model_calls':0,'checks':'passed','measurements':result['measurements']}),flush=True)
    finally:server.shutdown();server.server_close()


if __name__=='__main__':main()
