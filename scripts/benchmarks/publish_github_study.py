#!/usr/bin/env python3
"""Publish an audited study into command pages, or refresh docs without model calls."""
import argparse
import gzip
import hashlib
import io
import json
import tarfile
from pathlib import Path

from github_benchmark_docs import command_table, render, review_update, measured_date, published_reports, latest_results


def archive(path,entries):
    with path.open('wb') as raw:
        with gzip.GzipFile(fileobj=raw,mode='wb',mtime=0,filename='') as compressed:
            with tarfile.open(fileobj=compressed,mode='w') as tar:
                for name,data in sorted(entries):
                    item=tarfile.TarInfo(name);item.size=len(data);item.mode=0o644;item.mtime=0
                    tar.addfile(item,io.BytesIO(data))


def refresh_readmes(repo,reports):
    selected=latest_results(reports)
    updated=review_update(repo/'docs/benchmarks/github',selected['pr-reviews'][1])
    conditions=[]
    for stem in dict.fromkeys(stem for stem,_ in selected.values()):
        report=reports[stem];settings=report['protocol']['configured_defaults']
        conditions.append(f"{measured_date(report)}: `{settings['model']}` / `{settings['model_reasoning_effort']}`")
    for relative,korean in [('README.md',False),('docs/README.ko.md',True)]:
        path=repo/relative;base='benchmarks/github/' if korean else 'docs/benchmarks/github/'
        title='## 토큰 사용량 실측' if korean else '## Token usage measurements'
        end='## 설치' if korean else '## Install'
        lines=[title,'']
        if korean:
            lines += ['명령별 최신 실측을 모았습니다. 방식별 3회씩 비교했으며, 표는 **캐시 제외 입력+출력 평균**과 캐시 입력을 포함한 총 토큰 변화입니다. 측정일은 한국시간이며 범위·정답/상태 검증은 각 명령 문서에 있습니다.','',
                      '측정 당시 기본 설정: '+'; '.join(conditions)+'. 서로 다른 설정의 실험을 합산하거나 과거와의 차이를 코드 개선 효과로 해석하지 않습니다.','']
        else:
            lines += ['Latest results per command, with three trials per method. The table shows **mean uncached input + output** and the change in total tokens including cached input. Dates use Korea Standard Time; each command page provides ranges and answer/state checks.','',
                      'Inherited defaults: '+'; '.join(conditions)+'. Batches with different settings are not pooled; changes from earlier batches cannot be attributed to code improvements alone.','']
        lines+=command_table(selected,base,korean,updated)+['']
        if updated:
            lines += [f'[리뷰의 최신 출력 검증]({base}pr/reviews.md) 이후 작업 전체 토큰은 재측정하지 않았습니다.' if korean else
                      f'Whole-task tokens have not been remeasured after the [latest review output check]({base}pr/reviews.md).','']
        lines += [f'고정 합성 자료·소표본 결과이며 캐시 적중 차이가 남습니다. 일반적인 절감률이나 요금 절감을 뜻하지 않습니다. [공통 조건·한계·원본·재현]({base}README.md).' if korean else
                  f'These are small-sample results for a fixed synthetic workload; cache hit differences remain. They do not establish general or monetary savings. [Shared protocol, limits, evidence, and reproduction]({base}README.md).','']
        content=path.read_text(encoding='utf-8');start=content.index(title);stop=content.index(end,start)
        path.write_text(content[:start]+'\n'.join(lines)+'\n'+content[stop:], encoding='utf-8')


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    mode=parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--root',type=Path)
    mode.add_argument('--render-only',action='store_true',help='refresh published pages without changing frozen evidence')
    args=parser.parse_args();repo=Path(__file__).resolve().parents[2]
    out=repo/'docs/benchmarks/github';data=out/'data'
    if args.render_only:
        reports=published_reports(repo)
        render(repo,reports);refresh_readmes(repo,reports)
        print(json.dumps({'model_calls':0,'report':str(out/'README.md')},ensure_ascii=False))
        return
    from run_github_study import TASKS, REPETITIONS, summarize, validate_frozen
    root=args.root.resolve()
    report=json.loads((root/'report.json').read_text(encoding='utf-8'));protocol=report['protocol'];trials=report['trials']
    tasks=protocol['tasks'];expected_calls=len(tasks)*2*REPETITIONS
    assert tasks and len(set(tasks))==len(tasks) and set(tasks)<=set(TASKS)
    assert protocol['repetitions_per_method']==REPETITIONS and protocol['max_model_calls']==expected_calls
    expected_slots={(task,method,repetition) for task in tasks for method in ('gh','tools') for repetition in range(1,REPETITIONS+1)}
    assert len(trials)==expected_calls and {(r['task'],r['method'],r['repetition']) for r in trials}==expected_slots
    assert len(protocol['schedule'])==expected_calls and {(r['task'],r['method'],r['repetition']) for r in protocol['schedule']}==expected_slots
    validate_frozen(root,protocol)
    assert report['summary']==summarize(trials,tasks)
    for record in trials:
        directory=root/'runs'/record['task']/f"{record['index']:02d}-{record['method']}"
        raw=(directory/'events.jsonl').read_bytes();assert hashlib.sha256(raw).hexdigest()==record['events_sha256']
        events=[json.loads(line) for line in raw.splitlines()]
        assert [e['usage'] for e in events if e['type']=='turn.completed']==[record['usage']]
        assert record['uncached_plus_output']==record['usage']['input_tokens']-record['usage']['cached_input_tokens']+record['usage']['output_tokens']
        assert record['actual']==json.loads((directory/'answer.json').read_text(encoding='utf-8'))
        assert '--model' not in record['execution_args'] and '--ignore-user-config' not in record['execution_args']
        assert not any('model_reasoning_effort=' in arg for arg in record['execution_args'])
    data.mkdir(parents=True,exist_ok=True)
    tag=measured_date(report).replace('-','')+'-'+hashlib.sha256(','.join(tasks).encode()).hexdigest()[:6]
    stem='study' if tasks==TASKS else 'study-'+tag
    source=[('source/'+name,(root/'source'/name).read_bytes()) for name in protocol['source_hashes']]
    source += [('scripts/benchmarks/'+name,(root/'harness'/name).read_bytes()) for name in protocol['runner_hashes']]
    archive(data/(stem+'-source.tar.gz'),source)
    allowed={'prompt.txt','schema.json','answer.json','events.jsonl','api-access.json','interface-access.jsonl','state-verification.json','native.json'}
    evidence=[(str(path.relative_to(root)),path.read_bytes()) for folder in ('runs','preflight') for path in (root/folder).rglob('*') if path.is_file() and path.name in allowed]
    evidence += [(name,(root/name).read_bytes()) for name in ('protocol.json','preflight.json','environment.json')]
    archive(data/(stem+'-events.tar.gz'),evidence)
    report['evidence']={'sha256':{name:hashlib.sha256((data/name).read_bytes()).hexdigest() for name in (stem+'-source.tar.gz',stem+'-events.tar.gz')},
        'environment':json.loads((root/'environment.json').read_text(encoding='utf-8')),
        'total_input_plus_output':sum(r['input_plus_output'] for r in trials),'total_uncached_plus_output':sum(r['uncached_plus_output'] for r in trials),
        'failure_diagnostics':[{'task':r['task'],'method':r['method'],'repetition':r['repetition'],'answer_correct':r['answer_correct'],'state_correct':r['state_correct'],'actual':r['actual'],'state_checks':r['state_checks']} for r in trials if not r['correct']]}
    (data/(stem+'.json')).write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n', encoding='utf-8')
    reports=published_reports(repo)
    render(repo,reports);refresh_readmes(repo,reports)
    print(json.dumps({'calls':len(trials),'correct':sum(r['correct'] for r in trials),'state_correct':sum(r['state_correct'] for r in trials),'report':str(out/'README.md')},ensure_ascii=False))


if __name__=='__main__':main()
