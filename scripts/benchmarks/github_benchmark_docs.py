"""Render only the latest result for each command from audited study batches."""
import json
from datetime import datetime, timedelta, timezone
from pathlib import Path

COMMANDS = {
 'context':('context','context.md','저장소·기존 라벨·활성 이슈 유형을 조회하고 Git 상태와 사용자 파일 보존을 검증했습니다.'),
 'setup':('setup','setup.md','기존 라벨·유형으로 설정을 생성하고 저장된 .tools.json의 내용과 Git 상태를 검증했습니다.'),
 'issue-create':('issue create','issue/create.md','한국어 본문·담당자·기존 라벨·유형의 이슈 한 개, Development 연결 브랜치와 별도 워크트리를 생성했습니다. 주 워크트리와 사용자 파일 보존을 검증했습니다.'),
 'issue-branch':('issue branch','issue/branch.md','기존 이슈의 Development 브랜치·워크트리를 만든 뒤 같은 명령을 다시 실행했습니다. 연결 생성이 한 번뿐인지와 워크트리·upstream 재사용을 검증했습니다.'),
 'pr-create':('pr create','pr/create.md','현재 브랜치로 PR 한 개를 생성하고 한국어 본문·Closes #41·기존 라벨·head·base 및 Git 상태 보존을 검증했습니다.'),
 'pr-reviews':('pr reviews','pr/reviews.md','미해결 스레드 20개와 제출된 리뷰를 수집하고 댓글·위치·본문·원문 보관을 검증했습니다.'),
 'pr-inspect':('pr inspect','pr/inspect.md','기본 전체 범위의 PR 상태·검사·미해결 스레드 20개·CI 실행·실패 자료를 수집했습니다. blocked 상태와 오류 사실, Git 상태 보존을 검증했습니다.'),
 'pr-delta':('pr delta','pr/delta.md','기준 SHA의 조상 관계·현재 head·커밋 수를 확인하고 rename을 포함한 변경 파일을 비교했습니다. 기본 patch 생략 출력과 Git 상태 보존을 검증했습니다.'),
 'pr-submit':('pr submit','pr/submit.md','기존 커밋을 임시 원격에 실제로 push하고 PR 생성·라벨 적용·전체 검사 조회를 수행했습니다. 원격 SHA·PR 내용·ready 상태와 로컬 사용자 파일 보존을 검증했습니다.'),
 'pr-merge':('pr merge','pr/merge.md','검사·머지 조건 확인, 비동기 요청 한 번, 실제 머지 성공 확인을 거쳐 해당 PR의 로컬·원격 브랜치와 깨끗한 별도 워크트리를 정리했습니다. 주 워크트리·사용자 파일 보존을 검증했습니다.'),
 'ci-failures':('ci failures','ci/failures.md','완료된 실행 42의 attempt 2에서 job·annotation·로그·컴파일 사실을 수집하고 실행 메타데이터를 다시 확인했습니다. 동일 자료 재사용과 사용자 파일 보존을 검증했습니다.'),
 'ci-rerun':('ci rerun','ci/rerun.md','재실행을 한 번 요청하고 진행 중인 새 attempt 3을 기다린 뒤 완료된 실패 자료를 반환했습니다. 이전 회차를 완료로 오인하지 않는지와 사용자 파일 보존을 검증했습니다.'),
 'dependabot-list':('dependabot list','dependabot/list.md','독립 세션에서 severity high,critical 경고의 커서 두 페이지를 수집했습니다. 목록 전체·대상 버전의 패치·null 보존과 Git 상태를 검증했습니다.'),
 'dependabot-view':('dependabot view','dependabot/view.md','독립 세션에서 경고 7의 설명과 참고 URL을 확인했습니다. 목록 재조회 없이 상세 API 한 번으로 처리하고 Git 상태를 보존했습니다.'),
 'cleanup-apply':('branch cleanup --apply','branch/cleanup.md','14개 브랜치에서 종료 작업을 실제 정리했습니다. 미게시 커밋·열린 PR·보호 브랜치·작업 중인 워크트리를 보존하고 삭제 참조·upstream 설정·사용자 파일을 검증했습니다.'),
}

NOTES = {
 'issue-create':'캐시 제외 지표의 증가는 tools 3회차의 캐시 입력 0의 영향을 받습니다. 출력·명령 수는 직접 처리보다 작고 총 토큰은 감소했습니다. 현재 응답과 검증을 유지합니다.',
 'issue-branch':'독립 CLI 호출은 이슈·저장소·연결·워크트리를 다시 검증하며 직접 스크립트는 자료를 공유할 수 있습니다. 캐시 차이와 안전한 재사용 검증을 고려해 기능을 유지합니다.',
 'pr-create':'tools의 총 토큰은 약 45천으로 일정하지만 캐시 제외 값은 3,403–30,740으로 변동했습니다. API는 양쪽 7회로 같아 캐시 제외 증가만으로 추가 구현 비용을 단정하지 않습니다.',
 'dependabot-view':'양쪽 API는 한 번입니다. 직접 처리 1회차의 캐시 적중이 높아 캐시 제외 평균에 영향을 줬습니다. 일부 필드만 요구한 실험에 맞춰 영향 패키지·버전·보안 설명을 제거하지 않습니다.',
}


def measured_date(report):
    return datetime.fromisoformat(report['protocol']['measured_at_utc']).astimezone(timezone(timedelta(hours=9))).date().isoformat()


def published_reports(repo):
    root=repo/'docs/benchmarks/github/data'
    return {p.stem:json.loads(p.read_text()) for p in sorted(root.glob('study*.json'))}


def latest_results(reports):
    selected={}
    for stem,report in sorted(reports.items(),key=lambda item:item[1]['protocol']['measured_at_utc']):
        for task in report['protocol']['tasks']:
            selected[task]=(stem,report)
    if set(selected)!=set(COMMANDS):raise ValueError('missing or unknown command results')
    return selected


def review_update(root,report):
    path=root/'pr/reviews-output.json'
    return path.exists() and json.loads(path.read_text())['measured_on'] > measured_date(report)


def change(summary,key,korean=True):
    return f"{summary['change_percent'][key]:+.1f}%".replace('-','−') if summary['comparison_eligible'] else ('비교 보류' if korean else 'Withheld')


def command_table(selected,base,korean,review_updated=False):
    lines=['| 명령 | 측정일 | 직접 처리 평균 | tools 평균 | 변화 | 캐시 포함 변화 |' if korean else
           '| Command | Date | Direct mean | tools mean | Change | Total change (incl. cache) |','|---|---|---:|---:|---:|---:|']
    for task,(name,path,_) in COMMANDS.items():
        _,report=selected[task];summary=report['summary'][task];key='uncached_plus_output'
        if task=='pr-reviews' and review_updated:
            lines.append(f"| [`github {name}`]({base}{path}) | — | — | — | {'작업 전체 미재측정' if korean else 'Task not remeasured'} | {'출력 검증' if korean else 'Output verified'} |")
        else:
            lines.append(f"| [`github {name}`]({base}{path}) | {measured_date(report)} | {summary['gh']['metrics'][key]['mean']:,.0f} | {summary['tools']['metrics'][key]['mean']:,.0f} | {change(summary,key,korean)} | {change(summary,'input_plus_output',korean)} |")
    return lines


def command_page(task,stem,report,output=None):
    name,path,scope=COMMANDS[task];prefix='../'*(len(Path(path).parts)-1)
    summary=report['summary'][task];gh,to=summary['gh'],summary['tools'];settings=report['protocol']['configured_defaults']
    lines=[f'# `tools github {name}` 벤치마크','',
           f"최신 작업 전체 측정: **{measured_date(report)}**(한국시간), 방식별 3회. 기본 설정: `{settings['model']}` / `{settings['model_reasoning_effort']}`. [공통 조건·원본·재현]({prefix}README.md).",'',scope,'',
           '| 지표 | 직접 gh / Git 평균 [최소–최대] | tools 평균 [최소–최대] | 변화 |','|---|---:|---:|---:|']
    for key,label in [('uncached_plus_output','캐시 제외 입력+출력'),('input_plus_output','캐시 포함 총 토큰')]:
        fmt=lambda m:f"{m['mean']:,.0f} [{m['min']:,}–{m['max']:,}]"
        lines.append(f"| {label} | {fmt(gh['metrics'][key])} | {fmt(to['metrics'][key])} | {change(summary,key)} |")
    lines+=['',f"정답·상태 검증: 직접 처리 {gh['correct']}/{gh['n']}, tools {to['correct']}/{to['n']}."]
    if task in NOTES and measured_date(report)=='2026-10-08':lines+=['',NOTES[task]]
    if stem!='study' and task in ('pr-reviews','pr-inspect'):lines+=['','리뷰 compact 개선 후 같은 프롬프트·자료로 재측정했습니다. 이전 전체 실험과 기본 추론 강도가 달라 과거 수치와의 차이를 코드 개선 효과로 단정하지 않습니다.']
    if task=='pr-reviews' and stem!='study':
        lines+=['','tools의 2회차는 캐시 입력 적중이 높아 캐시 제외 값이 3,351로 줄었습니다. 캐시 제외 변화와 캐시 포함 총 토큰 변화를 함께 확인합니다.']
    if task=='pr-reviews' and output and output['after_source_hashes']['internal/cli/compact.go']==report['protocol']['source_hashes']['internal/cli/compact.go']:
        lines+=['','같은 구현의 별도 직렬화 출력 검증은 다음과 같습니다. 같은 스레드 20개와 동일한 원문 파일 참조를 비교했고, 정답·Git 상태·원문 SHA가 일치했습니다. 추가 모델 호출은 0회입니다.','',
                '| 인코딩 | 개선 전 출력 토큰 | 개선 후 출력 토큰 | 감소 |','|---|---:|---:|---:|']
        for row in output['measurements']:
            lines.append(f"| {row['encoding']} | {row['before_tokens']:,} | {row['after_tokens']:,} | {row['token_reduction_percent']:.2f}% |")
        lines+=['','이 표는 파일 참조와 SHA-256을 포함하고 마지막 개행을 제외한 출력만 측정합니다. 작업 전체 절감률과 다른 지표이며 재현 시 파일 경로 길이에 따라 값이 달라질 수 있습니다. [출력 검증 원본](reviews-output.json).']
    lines+=['',f"[사용법]({prefix}../../github/{path}) · [원본 JSON: summary.{task}]({prefix}data/{stem}.json)",'']
    return '\n'.join(lines)


def review_page(root):
    data=json.loads((root/'pr/reviews-output.json').read_text())
    lines=['# `tools github pr reviews` 벤치마크','',
           f'최신 검증: **{data["measured_on"]}**, 개선 전·후 CLI의 같은 스레드 20개 출력 비교. 추가 모델 호출은 0회입니다.','',
           '`--compact`에서 빈 선택적 위치, 같은 현재·원래 위치와 diff 방향, 생성 시각과 같은 수정 시각을 생략했습니다. 현재 line:null, 다른 원래 위치·실제 수정 시각·작성자·URL·본문·부분 수집 결과는 유지합니다. 정확한 전체 JSON은 원문 파일과 SHA-256으로 보관합니다.','',
           '| 인코딩 | 개선 전 출력 토큰 | 개선 후 출력 토큰 | 감소 |','|---|---:|---:|---:|']
    for row in data['measurements']:lines.append(f"| {row['encoding']} | {row['before_tokens']:,} | {row['after_tokens']:,} | {row['token_reduction_percent']:.2f}% |")
    row=data['measurements'][0]
    lines+=['',f"UTF-8 출력: {row['before_bytes']:,} → {row['after_bytes']:,}바이트.",'',
            '최종 정답·Git 상태·전체 원문 SHA가 같은지 검증했습니다. 파일 참조와 SHA-256을 포함하고 마지막 개행을 제외한 직렬화 출력 측정입니다. 개선 후 작업 전체 토큰은 재측정하지 않았으므로 전체 절감률로 해석하지 않습니다. 원문 파일 경로가 달라지면 참조 문자열의 바이트·토큰 수도 달라질 수 있습니다.',
            '', '```sh','python3 scripts/benchmarks/measure_review_metadata.py --root /tmp/review-metadata-check','```','',
            '[사용법](../../../github/pr/reviews.md) · [출력 검증 원본](reviews-output.json) · [공통 조건·의존성](../README.md)','']
    return '\n'.join(lines)


def render(repo,reports):
    root=repo/'docs/benchmarks/github';root.mkdir(parents=True,exist_ok=True)
    selected=latest_results(reports);updated=review_update(root,selected['pr-reviews'][1])
    used={stem for stem,_ in selected.values()}
    output_path=root/'pr/reviews-output.json'
    output=json.loads(output_path.read_text()) if output_path.exists() else None
    for task,(_,path,_) in COMMANDS.items():
        stem,report=selected[task];target=root/path;target.parent.mkdir(parents=True,exist_ok=True)
        target.write_text(review_page(root) if task=='pr-reviews' and updated else command_page(task,stem,report,output))
    lines=['# GitHub 벤치마크','',
           '명령별 최신 결과입니다. 경로는 [사용법 문서](../../github/README.md)와 같습니다. 측정일은 한국시간입니다. 작업 전체 토큰과 출력 토큰은 별도 지표로 다룹니다.','',
           '| 명령 | 최신 검증 |','|---|---|']
    for task,(name,path,_) in COMMANDS.items():
        _,report=selected[task]
        label=json.loads((root/'pr/reviews-output.json').read_text())['measured_on']+': compact 출력' if task=='pr-reviews' and updated else measured_date(report)+': 작업 전체, 방식별 3회'
        lines.append(f"| [`{name}`]({path}) | {label} |")
    lines+=['','## 공통 측정 조건','',
        '| 측정일 | 범위 | 실행·정답/상태 통과 | 당시 기본 설정 |','|---|---|---|---|']
    for stem,report in reports.items():
        if stem not in used:continue
        protocol=report['protocol'];settings=protocol['configured_defaults'];tasks=protocol['tasks']
        scope='15개 전체 명령' if len(tasks)==len(COMMANDS) else ', '.join('`'+COMMANDS[t][0]+'`' for t in tasks)
        lines.append(f"| {measured_date(report)} | {scope} | {len(report['trials'])}회 · {sum(r['correct'] for r in report['trials'])}/{len(report['trials'])} | `{settings['model']}` / `{settings['model_reasoning_effort']}` |")
    lines+=['',
        '모델·추론 강도는 매 실험의 사용자 기본값을 상속했습니다. 실행 인자에 모델·추론 강도와 ignore-user-config 옵션을 지정하지 않았습니다. JSON 이벤트가 실제 선택 모델·추론 강도를 노출하지 않아 설정 스냅샷과 옵션 생략으로 기록했습니다. 설정이 다른 실험을 합산하거나 과거 수치와의 차이를 코드 개선 효과로 해석하지 않습니다.','',
        '소스·CLI·실행기·프롬프트·정답 스키마·seed 20261008·실행 순서를 모델 호출 전에 고정했습니다. 명령 순서는 매 반복 섞고 각 명령의 방식 순서를 교차했으며, 매번 새 세션·임시 Git 저장소·CLI 캐시로 시작했습니다. 직접 처리도 배치·jq·로컬 스크립트를 허용했습니다. 실패·재시도도 포함하고 실패한 모델 실행을 교체하지 않았습니다.','',
        '캐시 제외 입력+출력은 input_tokens − cached_input_tokens + output_tokens, 총 토큰은 input_tokens + output_tokens입니다. 추론 출력을 중복 합산하지 않습니다. 어느 방식이든 3회 전부 정답·상태 검증을 통과하지 않으면 변화율 비교를 보류합니다. 원본에는 평균·중앙값·표본 표준편차·범위·각 쌍의 변화율·명령·API 수가 있습니다.','',
        '고정 합성 자료의 제어 실험입니다. 실제 CLI·gh·Git과 임시 bare 원격을 사용하고, API는 선택 필드·별칭·페이지를 처리하는 로컬 GraphQL 서버로 연결했습니다. 캐시는 강제로 초기화할 수 없어 캐시 제외 지표에도 적중 차이가 남습니다. 방식별 3회와 대표 시나리오에 한정하며 모든 옵션·대형 자료·오류 경로·실제 GitHub 성능·금액 절감을 대표하지 않습니다.','']
    if len(reports)>1:lines+=['최신 재측정은 리뷰 compact 출력이 바뀌는 두 명령에 한정했습니다. PR 제출의 기존 ready 시나리오는 리뷰 스레드가 비어 있고 머지 성공 시나리오에도 대상 메타데이터가 없어 기존 결과를 유지합니다.','']
    if updated:lines+=['리뷰의 최신 compact 출력 검증 이후 작업 전체 토큰은 재측정하지 않았습니다. 공통 원본의 이전 리뷰 결과를 개선 후 작업 전체 수치로 사용하지 않습니다.','']
    lines+=['## 원본과 재현','']
    for stem,report in reports.items():
        if stem not in used:continue
        lines.append(f"- {measured_date(report)}: [작업 전체 원본](data/{stem}.json) · [고정 소스·실행기](data/{stem}-source.tar.gz) · [이벤트·답변·API·사전 검증](data/{stem}-events.tar.gz)")
    lines+=['','압축 파일의 SHA-256은 각 JSON의 evidence.sha256에 있습니다. 명령별 문서에서는 최신 결과만 사용하고, 실험의 고정 소스·원본은 감사와 재현을 위해 한 번씩 보관합니다.','',
        '```sh','python3 -m pip install --require-hashes -r scripts/benchmarks/requirements.txt',
        '# 변경된 명령만 모델 없는 사전 검증',
        'python3 scripts/benchmarks/run_github_study.py --root /tmp/github-study --tasks pr-reviews pr-inspect',
        '# 같은 기록에서 선택한 2개 명령 × 2개 방식 × 3회 실행',
        'python3 /tmp/github-study/harness/run_github_study.py --root /tmp/github-study --resume --run-models',
        'python3 scripts/benchmarks/publish_github_study.py --root /tmp/github-study','```','',
        '`--tasks`를 생략하면 15개 전체 명령을 측정합니다. 문서만 갱신하려면 `python3 scripts/benchmarks/publish_github_study.py --render-only`를 실행합니다. 모델 호출과 압축 원본 변경 없이 같은 명령별 구조를 생성합니다.','',
        '고정 소스 재현은 해당 source.tar.gz를 새 디렉터리에 풀고 scripts를 source/scripts로 복사한 뒤 그 안의 run_github_study.py를 실행합니다. 소스·작업의 재현을 위한 자료이며 모델 응답·토큰 수의 동일성을 보장하지 않습니다.','']
    (root/'README.md').write_text('\n'.join(lines))
    (repo/'docs/benchmarks/README.md').write_text('# 벤치마크\n\n명령별 최신 측정 결과와 검증 자료입니다. 사용법 문서와 같은 디렉터리 구조를 사용합니다. 공통 조건·원본·재현 방법은 그룹 안내에 모아 두었습니다.\n\n| 그룹 | 측정 범위 |\n|---|---|\n| [GitHub](github/README.md) | 15개 명령의 작업 전체 실측, 변경 명령만 재측정 |\n')
