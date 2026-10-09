#!/usr/bin/env python3
"""Verify and publish all-command usage, evidence, frozen source and Markdown."""
import argparse
import gzip
import hashlib
import io
import json
import tarfile
from pathlib import Path

from run_all_github_benchmark import TASKS

NAMES = {'context':'context','setup':'setup','issue-create':'issue create','issue-branch':'issue branch',
         'pr-create':'pr create','pr-reviews':'pr reviews','pr-inspect':'pr inspect','pr-delta':'pr delta',
         'pr-submit':'pr submit','pr-merge':'pr merge','ci-failures':'ci failures','ci-rerun':'ci rerun',
         'dependabot-list':'dependabot list','dependabot-view':'dependabot view','cleanup-apply':'branch cleanup --apply'}


def archive(path, entries):
    with path.open('wb') as raw:
        with gzip.GzipFile(fileobj=raw,mode='wb',mtime=0,filename='') as zipped:
            with tarfile.open(fileobj=zipped,mode='w') as tar:
                for name,data in sorted(entries):
                    item=tarfile.TarInfo(name);item.size=len(data);item.mode=0o644;item.mtime=0
                    tar.addfile(item,io.BytesIO(data))


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,required=True)
    args=parser.parse_args();root=args.root.resolve();repo=Path(__file__).resolve().parents[2]
    report=json.loads((root/'report.json').read_text());all_trials=report['trials']
    trials=[r for r in all_trials if r.get('included',True)]
    assert report['protocol']['tasks']==TASKS
    assert len(trials)==30 and len({(r['task'],r['method']) for r in trials})==30
    for record in all_trials:
        directory=root/'runs'/record['task']/f"{record['index']:02d}-{record['method']}"
        raw=(directory/'events.jsonl').read_bytes()
        assert hashlib.sha256(raw).hexdigest()==record['events_sha256']
        usages=[json.loads(line)['usage'] for line in raw.splitlines() if json.loads(line)['type']=='turn.completed']
        assert usages==([record['usage']] if record['usage'] is not None else [])
        assert '--model' not in record['execution_args'] and '--ignore-user-config' not in record['execution_args']
        assert not any('model_reasoning_effort=' in arg for arg in record['execution_args'])
    source=[]
    for name,sha in report['protocol']['source_hashes'].items():
        data=(root/'source'/name).read_bytes();assert hashlib.sha256(data).hexdigest()==sha
        source.append(('source/'+name,data))
    for name in ('run_all_github_benchmark.py','publish_all_github_benchmark.py','test_all_github_benchmark.py',
                 'run_command_benchmark.py','run_default_benchmark.py','run_branch_cleanup_benchmark.py','run_dependabot_benchmark.py'):
        source.append(('scripts/benchmarks/'+name,(repo/'scripts/benchmarks'/name).read_bytes()))
    out=repo/'docs/benchmarks/github';out.mkdir(parents=True,exist_ok=True)
    archive(out/'all-defaults-source.tar.gz',source)
    entries=[]
    allowed={'prompt.txt','schema.json','answer.json','events.jsonl','api-access.json','interface-access.jsonl','state-verification.json','results.json'}
    for folder in ('preflight','runs'):
        for path in (root/folder).rglob('*'):
            if path.is_file() and path.name in allowed:entries.append((str(path.relative_to(root)),path.read_bytes()))
    archive(out/'all-defaults-events.tar.gz',entries)
    failures=[{'task':r['task'],'method':r['method'],'answer_correct':r['answer_correct'],'state_correct':r['state_correct'],
               'actual':r['actual'],'state_checks':r['state_checks']} for r in trials if not r['correct']]
    excluded=[{'task':r['task'],'method':r['method'],'index':r['index'],'reason':r['exclusion_reason'],
               'input_plus_output':r.get('input_plus_output')} for r in all_trials if not r.get('included',True)]
    report['evidence']={'source_archive':'all-defaults-source.tar.gz','events_archive':'all-defaults-events.tar.gz',
        'sha256':{name:hashlib.sha256((out/name).read_bytes()).hexdigest() for name in ('all-defaults-source.tar.gz','all-defaults-events.tar.gz')},
        'failure_diagnostics':failures,'excluded_trials':excluded,'all_model_calls':len(all_trials),
        'compared_calls':len(trials),'correct_calls':sum(r['correct'] for r in trials),
        'total_input_plus_output':sum(r.get('input_plus_output',0) for r in all_trials),
        'cached_input':sum(r.get('cached_input',0) for r in all_trials)}
    (out/'all-defaults.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    defaults=report['protocol']['configured_defaults'];correct=sum(r['correct'] for r in trials)
    lines=['# GitHub 전체 명령: 사용자 기본 모델·추론 설정 실측','',
        f"현재 GitHub 명령 **15개 전체**를 직접 `gh`/Git과 현재 `tools`로 각각 새 세션 1회씩 비교했습니다. 최종 비교 30회 중 정답 JSON·실제 상태 검증은 {correct}/30입니다. 전체 호출은 {len(all_trials)}회이며 제외 기록도 아래에 보존합니다. 측정 시각(UTC)은 `{report['protocol']['measured_at_utc']}`입니다.",'',
        f"모델·추론 강도는 지정하지 않고 사용자 기본 설정을 상속했습니다. 당시 선택된 설정 값은 `{defaults['model']}` / `{defaults['model_reasoning_effort']}`이며 실행 인자에 `--model`, 추론 설정, `--ignore-user-config`가 없습니다. 설정 스냅샷과 실행 인자를 원본에 보관했습니다. Codex JSON 이벤트가 실제 모델·추론 강도를 노출하지 않아 런타임 값을 별도로 검증한 것은 아닙니다.",'',
        '| 명령 | 직접 처리 총 토큰 | `tools` 총 토큰 | `tools` 변화 | 정답·상태 통과 |',
        '|---|---:|---:|---:|---:|']
    for task in TASKS:
        rows={r['method']:r for r in trials if r['task']==task};gh,to=rows['gh'],rows['tools']
        difference=f"{100*(to['input_plus_output']/gh['input_plus_output']-1):+.1f}%" if gh.get('input_plus_output') and to.get('input_plus_output') else '측정 불가'
        marker=' †' if not (gh['correct'] and to['correct']) else ''
        if marker:difference='비교 보류'
        fmt=lambda r:f"{r['input_plus_output']:,}" if 'input_plus_output' in r else '미수집'
        lines.append(f"| `github {NAMES[task]}` | {fmt(gh)} | {fmt(to)} | {difference}{marker} | {int(gh['correct'])+int(to['correct'])}/2 |")
    lines+=['','음수는 감소, 양수는 증가입니다. †는 같은 작업의 성공 결과끼리 비교할 수 없는 행입니다. 실패한 실행도 비용과 원본에 포함하고 성공 결과만 추려 평균을 내지 않았습니다.','',
        '**방식별 1회이며 캐시를 통제하지 않았습니다.** 일반적인 절감률·최적 직접 처리 비용·금액 절감으로 해석하지 않습니다. 총 토큰은 `turn.completed.usage.input_tokens + output_tokens`이며 캐시 입력·도구 상호작용·출력을 포함한 에이전트 작업 전체입니다. 추론 출력은 출력에 포함되어 따로 더하지 않습니다.','',
        '| 명령 | 직접 캐시 제외 입력 | `tools` 캐시 제외 입력 | 직접 출력 | `tools` 출력 | 직접 / `tools` API 요청 |',
        '|---|---:|---:|---:|---:|---:|']
    for task in TASKS:
        rows={r['method']:r for r in trials if r['task']==task};gh,to=rows['gh'],rows['tools']
        fmt=lambda r,key:f"{r[key]:,}" if key in r else '미수집'
        lines.append(f"| `{NAMES[task]}` | {fmt(gh,'uncached_input')} | {fmt(to,'uncached_input')} | {fmt(gh,'output_tokens')} | {fmt(to,'output_tokens')} | {gh['api_calls']} / {to['api_calls']} |")
    lines+=['','## 수행 범위와 검증','',
        '- `context`, `setup`: 저장소·기존 라벨·활성 이슈 유형 조회, 설정 파일 저장과 내용 검증.',
        '- `issue create`, `issue branch`: 이슈·담당자·라벨·유형·Development 연결, 별도 워크트리 체크아웃, 주 작업 폴더·작성 파일 보존. `issue branch`는 두 번 실행해 재사용과 단일 연결 생성을 검증.',
        '- `pr create`: 현재 브랜치의 PR 1개 생성, 한국어 본문·`Closes #41`·기존 라벨·Git 상태 검증. 이슈 유형은 양쪽에서 조회하지 않음.',
        '- `pr reviews`: 독립 명령의 `--compact`로 제출 리뷰 이력과 미해결 스레드 20개의 댓글 ID·위치·본문을 수집. 리뷰 변경분만 읽는 이전 3개 작업 표본과 별도 조건.',
        '- `pr inspect`: 기본 전체 범위로 상태·검사·리뷰·CI 실패 자료 수집, 실패 검사와 미해결 리뷰가 남은 `blocked` 결과의 사실 검증.',
        '- `pr delta`: 실제 현재 명령의 기본 patch 생략 출력으로 조상·head·커밋 수 검증과 rename 포함 변경 파일 비교. 이전 patch 포함 프로토타입과 별도 조건.',
        '- `pr submit`: 기존 커밋을 실제 임시 원격으로 푸시, PR 생성·라벨 적용·전체 검사 수집, 원격 SHA·PR 내용 검증. 커밋 작성 없이 깨끗한 작업 폴더 사용.',
        '- `pr merge`: 검사·머지 조건 확인, 비동기 머지 요청 1회·실제 성공 확인 후 해당 PR의 로컬·원격 브랜치와 깨끗한 별도 워크트리 정리. 주 작업 폴더·사용자 파일 보존.',
        '- `ci failures`: `--compact`로 완료 실패 자료 수집 후 같은 실행의 메타데이터 재확인. `ci rerun`: `--compact`로 재실행 요청 1회, 진행 중인 새 attempt를 기다린 뒤 완료된 새 회차의 실패 자료 수집. 이전 회차를 완료 결과로 사용하지 않음.',
        '- `dependabot list`, `dependabot view`: 각각 독립 세션. 목록은 강제로 나눈 커서 2페이지 전체와 null 패치 보존, 상세는 경고 7의 설명·참고 URL만 조회.',
        '- `branch cleanup --apply`: 14개 브랜치의 실제 삭제·참조·upstream 설정 검증, 미게시 커밋·열린 PR·보호 브랜치·작업 중인 워크트리·사용자 파일 보존. `kept_targets`는 기존 참조 대상에 보존 워크트리 대상 2개도 더한 현재 집계 기준.',
        '', '15개 명령의 모델 없는 사전 검증을 먼저 통과했습니다. 각 명령의 대표 시나리오이며 모든 옵션·dry-run·재개·오류 경로·대형 자료를 모델로 측정한 것은 아닙니다. 구현 변경 전후 비교가 아니라 현재 CLI와 직접 처리의 비교입니다. 모든 GitHub API는 로컬 합성 서버로, Git 전송은 임시 bare 저장소로 연결했습니다. 실제 GitHub에 생성·머지·재실행 요청을 보내지 않았습니다.','',
        '## 실패와 비용 기록','']
    if failures:
        for failure in failures:
            checks=[k for k,v in failure['state_checks'].items() if not v]
            lines.append(f"- `{NAMES[failure['task']]}` / `{failure['method']}`: 정답 일치 {failure['answer_correct']}, 상태 검증 {failure['state_correct']}. 실패한 상태 항목: {', '.join(checks) or '없음'}. 실제 값은 원본 `evidence.failure_diagnostics`에 보존.")
        if any(f['task']=='pr-merge' for f in failures):
            lines.append('')
            lines.append('직접 머지는 실제 머지·워크트리·브랜치 정리와 사용자 파일 보존을 모두 수행했지만, 최종 head에 커밋 SHA 대신 브랜치명을 반환했습니다. 재측정으로 교체하지 않았으며 이 행의 토큰 차이는 정답까지 일치하는 작업의 절감률로 주장하지 않습니다. 실험 서버가 이 실행의 유효한 축소 GraphQL 조회와 combined status 조회를 지원하지 않아 재시도 비용도 발생했습니다. 그 비용이 포함된 제한된 API 환경의 결과입니다.')
    else:lines.append('최종 비교 30회 모두 정답·상태 검증을 통과했습니다. 실행 중 복구한 명령 오류도 비용에서 빼지 않았습니다.')
    if excluded:
        lines+=['','다음 실행은 실험 스키마 오류로 비교에서 제외하고 해당 명령만 재측정했습니다. 원본·이벤트·비용은 삭제하지 않았습니다.','']
        for x in excluded:lines.append(f"- `{NAMES[x['task']]}` / `{x['method']}` 실행 {x['index']}: {x['input_plus_output']:,}토큰. {x['reason']}")
        lines+=['','rename에만 존재하는 previous_filename을 모든 파일에 필수로 요구하던 스키마를 파일 형태별 anyOf로 수정했습니다. 독립 JSON Schema 검증기로 수정 파일·rename 정답을 검증한 후 두 실행을 교체했습니다.']
    lines+=['',f"이번 {len(all_trials)}회 총 입력+출력은 {report['evidence']['total_input_plus_output']:,}토큰, 이 중 캐시 입력은 {report['evidence']['cached_input']:,}토큰입니다. 앞서 수행한 3개 작업 실험의 호출은 이 합계와 비교에 포함하지 않았습니다.",'',
        '## 원본과 재현','',
        '[원본 JSON](all-defaults.json) · [전체 실행 이벤트·정답·상태 검증](all-defaults-events.tar.gz) · [고정 Go 소스·실행기](all-defaults-source.tar.gz). 원본에 소스·CLI·이벤트·압축 파일 SHA-256과 방식별 프롬프트·실행 인자를 보관했습니다.','',
        '현재 소스로 모델 없는 사전 검증만 실행합니다. 아직 존재하지 않는 전용 `/tmp` 경로를 사용합니다.','',
        '```sh','python3 scripts/benchmarks/run_all_github_benchmark.py --root /tmp/github-all-preflight','```','',
        '사전 검증 뒤 같은 경로로 `--resume --run-models`를 추가하면 사용자 기본 모델·추론 설정으로 30회 호출합니다. 중단 시 완료한 실행은 재호출하지 않고 남은 실행부터 진행합니다. 프롬프트·소스·설정·시스템 지시문·캐시는 재실행 시 달라질 수 있습니다.','',
        '고정 소스는 새 디렉터리에 압축을 풀고, `source`를 작업 저장소로 두어 그 아래에 동봉된 `scripts` 디렉터리를 복사한 뒤 동봉 실행기를 실행합니다. 고정 소스는 CLI 동작 재현용이며 모델 응답·캐시·전체 토큰의 동일성을 보장하지 않습니다.','']
    (out/'all-defaults.md').write_text('\n'.join(lines))
    print(json.dumps({'tasks':len(TASKS),'calls':len(all_trials),'compared':len(trials),'correct':correct,'report':str(out/'all-defaults.md')},ensure_ascii=False))


if __name__=='__main__':main()
