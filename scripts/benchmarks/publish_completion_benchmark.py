#!/usr/bin/env python3
"""완료 비교의 모든 실행·기본 설정·정답·상태·소스·이벤트를 보관한다."""
import argparse
import copy
import hashlib
import json
import statistics
from pathlib import Path

from publish_usage_benchmark import audit
from publish_github_study import archive
from run_completion_benchmark import TASKS

PAGES = {
    'review-source':('github/pr/reviews.md','리뷰 경로 연결·긴 원문 반영'),
    'review-return':('github/pr/inspect.md','다른 작업 후 제출 리뷰 복귀'),
    'junit-recovery':('fs/test-results.md','기본 상한을 넘은 JUnit 진단 복구'),
    'delta-recovery':('fs/delta.md','삭제 원문 복구·기준 보존'),
    'apply-report':('fs/apply.md','큰 변경 보고서·최종 상태 확인'),
    'setup-proof':('github/setup.md','설정 저장·실제 SHA·권한 검증'),
    'ci-evidence':('github/ci/failures.md','저장 CI 원문 선택·완전성 확인'),
}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def assessment(summary):
    if not summary['comparison_eligible']:
        return '실패 포함: AND 판정 보류'
    direct, tools = summary['direct']['metrics'], summary['tools']['metrics']
    lower = lambda key: tools[key]['mean'] < direct[key]['mean']
    nonincreasing = lambda key: tools[key]['mean'] <= direct[key]['mean']
    token = lower('input_plus_output') and lower('uncached_plus_output')
    efficiency = lower('seconds') and nonincreasing('command_calls') and nonincreasing('api_calls')
    if token and efficiency:
        return '소표본 평균의 AND 충족; 안정적 실사용 효과는 미확인'
    if lower('input_plus_output') != lower('uncached_plus_output'):
        return '조건부: 캐시 포함·제외 지표 상충'
    if token and lower('seconds'):
        return '조건부: 토큰·시간 감소, 호출 부담 증가'
    return '부분 개선 또는 AND 미충족'


def metrics(rows):
    result={}
    for key in ('input_plus_output','uncached_plus_output','seconds','command_calls','cli_calls','api_calls'):
        values=[row[key] for row in rows if row.get(key) is not None]
        result[key]={'n':len(values),'mean':statistics.mean(values) if values else None,'min':min(values) if values else None,'max':max(values) if values else None,'stdev':statistics.stdev(values) if len(values)>1 else 0}
    return result


def adjudicate_review_return(root, report):
    corrections=[]
    frozen=root/'harness/run_completion_benchmark.py'
    if not frozen.exists() or "'review_decision':'CHANGES_REQUESTED'" not in frozen.read_text(encoding='utf-8'):
        return corrections
    for row in report['trials']:
        if row['task']!='review-return':
            continue
        native=json.loads((root/'preflight/review-return'/str(row['repetition'])/'result.json').read_text(encoding='utf-8'))['response']
        work=native['outstanding']
        review=work['reviews'][0]
        assert native['complete'] and len(work['reviews'])==1
        assert work['pr']['review_decision']=='APPROVED'
        assert review['state']=='CHANGES_REQUESTED'
        expected={'body':review['body'],'review_id':review['id'],'state':review['state'],'commit':review['commit_id'],'head':work['pr']['head_sha'],'review_decision':work['pr']['review_decision']}
        corrected=row['exit_code']==0 and row['usage'] is not None and row['actual']==expected and row['state_correct']
        corrections.append({'task':row['task'],'index':row['index'],'method':row['method'],'repetition':row['repetition'],'original_correct':row['correct'],'corrected_correct':corrected,'expected_from_native':expected,'native_evidence':f'preflight/review-return/{row["repetition"]}/result.json','reason':'Original expected aggregate incorrectly hardcoded CHANGES_REQUESTED; frozen FixtureGraphQL explicitly returns APPROVED independently of historical submitted state. Native preflight confirms APPROVED. Raw trials, usage and original grading remain unchanged.'})
    return corrections


def graded_rows(report, task):
    rows=copy.deepcopy([r for r in report['trials'] if r['task']==task])
    corrections={(r['task'],r['index']):r for r in report.get('adjudications',[])}
    for row in rows:
        if (row['task'],row['index']) in corrections:
            row['correct']=corrections[(row['task'],row['index'])]['corrected_correct']
    return rows


def render(repo):
    out=repo/'docs/benchmarks/completion'
    index=json.loads((out/'data/index.json').read_text(encoding='utf-8'))
    table=[]
    for task in TASKS:
        cohort=index['latest'][task]
        report=json.loads((out/'data'/f'{cohort}.json').read_text(encoding='utf-8'))
        rows=graded_rows(report,task)
        summary={'direct':{'metrics':metrics([r for r in rows if r['method']=='direct'])},'tools':{'metrics':metrics([r for r in rows if r['method']=='tools'])},'comparison_eligible':len(rows)==6 and all(r['correct'] and r['usage'] is not None for r in rows)}
        change=lambda key:100*(summary['tools']['metrics'][key]['mean']/summary['direct']['metrics'][key]['mean']-1)
        decision=assessment(summary)
        page,title=PAGES[task]
        table.append(f'| [{title}]({page}) | {change("input_plus_output"):+.1f}% | {change("uncached_plus_output"):+.1f}% | {change("seconds"):+.1f}% | {sum(r["correct"] for r in rows)}/6 | {decision} |')
        path=out/page
        path.parent.mkdir(parents=True,exist_ok=True)
        relative='../'*len(Path(page).parts[:-1])
        lines=[f'# {title}', '', '직접 처리와 tools의 같은 완료 작업을 비교한다. 이전의 작은 결과·다른 입력·다른 기본 모델 실험과 절감률을 합치지 않는다.', '', f'판정: **{decision}**. 직접 3회·tools 3회, 정답과 파일 상태 {sum(r["correct"] for r in rows)}/6.', '', '| 지표 | 직접 평균 (범위; 표준편차) | tools 평균 (범위; 표준편차) | 변화 |', '|---|---|---|---|']
        for key,label in [('input_plus_output','캐시 포함 입력+출력'),('uncached_plus_output','캐시 제외 입력+출력'),('seconds','완료 시간(초)'),('command_calls','모델 셸 실행'),('cli_calls','내부 tools CLI'),('api_calls','원격 API')]:
            a,b=summary['direct']['metrics'][key],summary['tools']['metrics'][key]
            fmt=lambda m:f'{m["mean"]:.2f} ({m["min"]:.2f}–{m["max"]:.2f}; {m["stdev"]:.2f})'
            delta=f'{change(key):+.1f}%' if a['mean'] else '기준 0'
            lines.append(f'| {label} | {fmt(a)} | {fmt(b)} | {delta} |')
        lines += ['', '| 회차 | 방식 | 총 토큰 | 캐시 제외 | 셸/CLI/API | 시간(초) | 정답·상태 |', '|---|---|---|---|---|---|---|']
        for r in rows:
            lines.append(f'| {r["repetition"]} | {r["method"]} | {r["input_plus_output"]} | {r["uncached_plus_output"]} | {r["command_calls"]}/{r["cli_calls"]}/{r["api_calls"]} | {r["seconds"]:.3f} | {r["correct"]} |')
        lines += ['', '조회·입력 작성·저장·복구·편집·최종 확인과 오류/재시도는 전부 포함한다. 직접 처리에도 필터·배치·한 스크립트의 저장/read-back을 허용한다. 실제 파일 I/O syscall 수는 계측하지 않았으며 CLI 수와 혼동하지 않는다. 보고서 저장·재확인에 따른 추가 I/O와 호출은 부분 개선의 부담이다.', '', '독립 1회 작업이며 provider 입력 캐시는 통제하지 않았다. 기본 설정을 상속하고 모델/추론 옵션을 지정하지 않았다. 회차 3개는 안정성이나 캐시 없는 최초 실행을 증명하지 않는다. 모델 도구 이벤트는 원본 events.jsonl에 보존하며 표의 셸 횟수는 command_execution이다.', '', f'[공통 조건]({relative}README.md) · [전체 원본]({relative}data/{cohort}.json) · [고정 소스]({relative}data/{cohort}-source.tar.gz) · [모든 실행 이벤트]({relative}data/{cohort}-events.tar.gz)']
        path.write_text('\n'.join(lines)+'\n',encoding='utf-8')
    first=json.loads((out/'data'/f'{index["latest"][TASKS[0]]}.json').read_text())
    defaults=first['protocol']['configured_defaults']
    readme=['# 변경 작업 완료 비교', '', f'A1–A6의 변경된 작업 7개만 직접 처리와 tools로 각 3회 비교한다. 원문 복구 뒤 최초 선택 응답으로 개선한 세 작업을 별도 재측정했다. 현재 표는 작업당 최신 6건이며 총 {sum(c["trials"] for c in index["cohorts"].values())}개 실제 모델 세션의 입력·출력 usage, 정답·최종 파일/권한·호출·시간을 보존한다. 실패와 불리한 실행도 제외하거나 교체하지 않는다. OS 런타임 검증은 Linux이며 다른 OS는 빌드와 구분한다.', '', f'기본 설정: `{defaults}`. 옵션으로 덮어쓰지 않았으며 provider 입력 캐시는 통제하지 않았다. 토큰은 캐시 포함 입력+출력이 주지표이고 캐시 제외 입력+출력도 함께 판정한다. 둘 중 유리한 수치만 고르지 않는다.', '', '| 작업 | 총 토큰 변화 | 캐시 제외 변화 | 시간 변화 | 정답·상태 | 판단 |', '|---|---|---|---|---|---|']+table+['', 'AND의 소표본 판정은 두 토큰 지표·시간 감소와 셸/API 호출 비증가를 함께 확인한다. 두 토큰 지표가 상충하거나 호출 부담이 증가하면 조건부로 기록한다. 새로운 보관 I/O도 있으므로 수치가 좋아졌다고 모든 비용이 감소하거나 실사용에서 안정적이라고 보장하지 않는다.', '', '최신 fs 비교는 --diagnostic-pattern/--report-pattern으로 실제 요청 구간과 검증 영수증을 첫 응답에서 받는다. apply의 mode_preserved도 실제 read-back 근거다. 저장 뒤 복구하는 이전 18건도 첫 코호트에 보존했다. 일반 크기의 한 번 조회와 기본 상한을 넘은 보고서 복구를 구분한다. 리뷰 복귀와 delta에는 이전에 만들어진 관찰을 작업 입력으로 공급했다. 기존 관찰 생성 비용은 복귀 작업 밖이며 동일 명령 연속 실행이나 이전 모델 문맥을 가정하지 않는다. CI 원문도 공급된 이전 자료이며 수집부터 수행한 CI 실패/재실행/제출/머지 전체의 새 측정이 아니다.', '', '직접 처리의 자연스러운 배치·필터·Python 저장/검증을 허용했다. tools의 큰 보고서는 저장과 필요한 부분 읽기까지 포함한다. 모델의 셸 실행, 내부 tools CLI, API 호출을 나눴다. 실제 syscall/I/O 횟수는 계측하지 않았으며 저장·해시 확인 비용은 시간과 호출에 포함한다.', '', '공유 실행기의 이전 usage 코호트용 helper·malformed JUnit 제한 문구가 원본 protocol에 남아 있지만 이번 코호트에는 integer helper나 malformed JUnit 작업이 없다. 이번 입력은 재현한 결함의 합성 fixture이며 모든 개인 세션을 재생한 시험은 아니다. 로컬 API 지연은 실제 GitHub 네트워크 지연과 다르다.', '', '제출 리뷰 복귀 6건의 원채점 기대값에 오류가 있었다. 과거 CHANGES_REQUESTED를 현재 종합 상태로 하드코딩했으나 고정 fixture와 native 응답은 APPROVED이다. 원본의 false 채점·usage·이벤트를 모두 유지하며 adjudications에 독립 native 근거와 정정 채점을 별도로 기록했다. 위 표는 정정 채점이다. 재실행·대체·제외는 하지 않았다.', '', '첫 코호트 뒤 자동 저장의 os.Root 경계와 삭제된 부모 경로 검증을 보강했다. 최초 선택 응답 코호트는 보강된 소스를 고정해 측정했고 일반 복구·오류 경로는 Go 시험으로 검증했다. 각 표의 토큰은 해당 코호트의 보관된 소스/입력에 대한 결과다.', '', '소스·실행기는 코호트별로 먼저 고정하고 사전 검증했다 (원문 복구 21건, 최초 선택 응답 9건). 정답·파일 상태 검증은 모델 밖의 실행기가 담당한다. 출력 사용성 [관찰 재현](../command-usability-audit.json)은 토큰 측정과 별도이다.', '', '재현:', '', '```sh', 'python3 scripts/benchmarks/run_completion_benchmark.py --root /tmp/fresh-completion', 'python3 /tmp/fresh-completion/harness/run_completion_benchmark.py --root /tmp/fresh-completion --resume --run-models', 'python3 scripts/benchmarks/publish_completion_benchmark.py --root /tmp/fresh-completion --name NEW_NAME', '```', '', '기존 원본은 불변 코호트로 보존한다. 최신 문서는 각 명령의 최신 코호트만 표시한다. [개선안·적용 범위](../../command-improvements.md).']
    (out/'README.md').write_text('\n'.join(readme)+'\n',encoding='utf-8')


def publish(repo,root,name):
    report=audit(root)
    protocol=report['protocol']
    out=repo/'docs/benchmarks/completion/data'
    out.mkdir(parents=True,exist_ok=True)
    sources=[('source/'+p,(root/'source'/p).read_bytes()) for p in protocol['source_hashes']]
    sources += [('harness/'+p,(root/'harness'/p).read_bytes()) for p in protocol['runner_hashes']]
    sources += [(p,(root/p).read_bytes()) for p in ('main-overlay.go','overlay.json','protocol.json')]
    archive(out/f'{name}-source.tar.gz',sources)
    events=[(str(p.relative_to(root)),p.read_bytes()) for folder in ('runs','preflight','seed') for p in sorted((root/folder).rglob('*')) if p.is_file()]
    archive(out/f'{name}-events.tar.gz',events)
    public=copy.deepcopy(report)
    public['adjudications']=adjudicate_review_return(root, report)
    public['evidence']={p.name:digest(p.read_bytes()) for p in (out/f'{name}-source.tar.gz',out/f'{name}-events.tar.gz')}
    encoded=json.dumps(public,ensure_ascii=False,indent=2)+'\n'
    target=out/f'{name}.json'
    if target.exists() and target.read_text(encoding='utf-8')!=encoded:
        raise RuntimeError('published cohort is immutable; use a new name')
    target.write_text(encoded,encoding='utf-8')
    index_path=out/'index.json'
    index=json.loads(index_path.read_text()) if index_path.exists() else {'cohorts':{},'latest':{}}
    index['cohorts'][name]={'trials':len(report['trials']),'correct':sum(r['correct'] for r in report['trials'])}
    for task in protocol['tasks']:
        index['latest'][task]=name
    index_path.write_text(json.dumps(index,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({'published':name,'trials':len(report['trials']),'correct':sum(r['correct'] for r in report['trials'])}))


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    group=parser.add_mutually_exclusive_group(required=True)
    group.add_argument('--root',type=Path)
    group.add_argument('--render-only',action='store_true')
    parser.add_argument('--name')
    args=parser.parse_args()
    repo=Path(__file__).resolve().parents[2]
    if args.root:
        if not args.name or not args.name.replace('-','').isalnum():
            parser.error('safe cohort name required')
        publish(repo,args.root.resolve(),args.name)
    render(repo)


if __name__=='__main__':
    main()
