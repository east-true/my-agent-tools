#!/usr/bin/env python3
"""Audit all scheduled filesystem trials and publish latest command pages and evidence."""
import argparse
import gzip
import hashlib
import io
import json
import tarfile
from datetime import datetime,timedelta,timezone
from pathlib import Path

import run_fs_benchmark as study


def archive(path,entries):
    with path.open('wb') as raw:
        with gzip.GzipFile(fileobj=raw,mode='wb',mtime=0,filename='') as packed:
            with tarfile.open(fileobj=packed,mode='w') as tar:
                for name,data in sorted(entries):
                    entry=tarfile.TarInfo(name);entry.size=len(data);entry.mode=0o644;entry.mtime=0;tar.addfile(entry,io.BytesIO(data))


def and_assessment(summary,korean=True):
    if not summary['comparison_eligible']:
        return '검증 부족·판정 보류' if korean else 'Withheld: validation incomplete'
    direct,tools=(summary[method]['metrics'] for method in ('direct','tools'))
    lower=lambda key:tools[key]['mean']<direct[key]['mean']
    calls_ok=tools['command_calls']['mean']<=direct['command_calls']['mean']
    efficiency=lower('seconds') and calls_ok
    tokens=lower('input_plus_output')
    if efficiency and tokens:
        if tools['uncached_plus_output']['mean']>direct['uncached_plus_output']['mean']:
            return '조건부·캐시 지표 상충' if korean else 'Conditional: cache metrics conflict'
        return '표본에서 AND 충족' if korean else 'AND met in sample'
    if lower('seconds') or tokens:
        return '부분 개선·AND 미충족' if korean else 'Partial improvement; AND unmet'
    return 'AND 미충족' if korean else 'AND unmet'


def table(report,base,korean):
    lines=['| 명령 | 직접 평균 | tools 평균 | 캐시 제외 변화 | 캐시 포함 변화 | 정답·상태 | 동시 개선 판단 |' if korean else '| Command | Direct mean | tools mean | Uncached change | Total change (incl. cache) | Answer + state | Joint assessment |','|---|---:|---:|---:|---:|---:|---|']
    for task in study.TASKS:
        s=report['summary'][task];key='uncached_plus_output'
        changes=[f"{s['change_percent'][k]:+.1f}%".replace('-','−') if s['comparison_eligible'] else ('비교 보류' if korean else 'Withheld') for k in ('uncached_plus_output','input_plus_output')]
        lines.append(f"| [`fs {task}`]({base}{task}.md) | {s['direct']['metrics'][key]['mean']:,.0f} | {s['tools']['metrics'][key]['mean']:,.0f} | {changes[0]} | {changes[1]} | {s['direct']['correct']+s['tools']['correct']}/6 | {and_assessment(s,korean)} |")
    return lines


def render(repo,reports):
    selected={}
    for stem,batch in sorted(reports.items(),key=lambda item:item[1]['protocol']['measured_at_utc']):
        for task in batch['protocol']['tasks']:selected[task]=(stem,batch)
    if set(selected)!=set(study.TASKS):raise ValueError('incomplete command coverage')
    report=max(reports.values(),key=lambda r:r['protocol']['measured_at_utc'])
    out=repo/'docs/benchmarks/fs';settings=report['protocol']['configured_defaults'];date=datetime.fromisoformat(report['protocol']['measured_at_utc']).astimezone(timezone(timedelta(hours=9))).date().isoformat()
    scopes={'inspect':'텍스트 파일 40개에서 두 리터럴 패턴의 일치 16개를 수집했습니다. 제외 디렉터리·바이너리·사용자 파일의 원문 보존을 확인했습니다.',
            'delta':'40개 파일의 SHA-256·원문 기준을 먼저 만들고, 같은 크기·mtime의 수정, 삭제, 생성과 rename을 관찰했습니다. 변경 구간·추적 건수·사용자 파일 보존을 확인했습니다. 기준 생성 비용도 양쪽에 포함했습니다.',
            'apply':'같은 호출자 계획으로 기존 파일 두 개의 정확한 치환과 새 파일 생성을 미리 검증한 뒤 실제 반영했습니다. 최종 바이트·기존 권한·계획 파일·사용자 파일 보존을 독립 검증했습니다.'}
    for task in study.TASKS:
        stem,batch=selected[task];s=batch['summary'][task];task_scope=scopes[task]
        if batch['protocol'].get('workflows'):
            task_scope={'inspect':'동일한 요청 파일로 세 파일의 서로 다른 구간·패턴·문맥 14줄을 수집했습니다. tools는 512바이트 출력 페이지를 끝까지 이어 읽었습니다. 직접 스크립트와 최종 답변에는 페이지 상한을 강제하지 않았습니다.', 'delta':'40개 파일의 해시·원문 기준을 만들고 같은 크기·mtime의 10·990줄 수정과 생성·삭제·rename을 관찰했습니다. 기준을 갱신하지 않는 비교를 두 번 수행하고 기준 파일 바이트 보존을 독립 검증했습니다.', 'apply':'동일한 해시 없는 치환 조건으로 원본 SHA 계획을 생성·저장하고 두 파일 수정과 새 파일 생성을 수행했습니다. 실제 파일 재검증과 값 확인까지 포함하며 저장 계획의 SHA가 원본과 같은지 검증했습니다.'}[task]
        if batch['protocol'].get('completion_workflows'):
            task_scope={
                'inspect':'요청 파일의 세 원문 구간을 조회한 뒤 각 구간의 줄 순서를 실제로 뒤집고 검증했습니다. CRLF·혼합 줄바꿈·마지막 개행·기존 권한과 나머지 모든 바이트를 보존했습니다. tools는 기본 출력 상한에서 --raw --hash로 조회하고 해시 계획을 만들어 반영·재검증했습니다. 페이지 스트레스 시험은 이 측정에서 제외했습니다.',
                'delta':'40개 파일의 해시·원문 기준을 만들고 같은 크기·mtime의 떨어진 두 수정과 생성·삭제·rename을 관찰했습니다. 양쪽 모두 두 번의 실제 새 파일 스캔과 기준 바이트 보존을 검증했습니다. tools는 --peek --content-kinds modified --comparisons 2로 동일 보고서의 중복 출력·외부 비교 루프를 줄였습니다.',
                'apply':'제공된 수정 조건의 잘못된 치환 횟수를 쓰기 전에 진단하고, 원본 JSON을 보존한 복사본에서 해당 횟수만 수정했습니다. 원본 SHA 계획 저장·두 파일 수정·새 파일 생성·바이트와 권한 재검증까지 포함했습니다. tools는 구조화된 오류의 경로·치환 번호·기대·실제 횟수로 재조회 없이 조건을 바로잡았습니다.'}[task]
        task_settings=batch['protocol']['configured_defaults']
        task_date=datetime.fromisoformat(batch['protocol']['measured_at_utc']).astimezone(timezone(timedelta(hours=9))).date().isoformat();lines=[f'# `tools fs {task}` 벤치마크','',f"최신 측정: **{task_date}**(한국시간), 방식별 3회. 당시 기본 설정 `{task_settings['model']}` / `{task_settings['model_reasoning_effort']}`. [공통 조건·원본·재현](README.md).",'',task_scope,'',
            '| 지표 | 직접 처리 평균 [최소–최대] | tools 평균 [최소–최대] | 변화 |','|---|---:|---:|---:|']
        for key,label in [('uncached_plus_output','캐시 제외 입력+출력'),('input_plus_output','캐시 포함 총 토큰'),('command_calls','모델의 셸 명령 호출 수'),('seconds','작업 시간(초)')]:
            a,b=s['direct']['metrics'][key],s['tools']['metrics'][key]
            fmt=lambda m:(f"{m['mean']:.2f} [{m['min']:g}–{m['max']:g}]" if key in ('command_calls','seconds') else f"{m['mean']:,.0f} [{m['min']:,}–{m['max']:,}]")
            percent=s.get('change_percent',{}).get(key)
            if percent is None and s['comparison_eligible']:
                percent=100*(b['mean']/a['mean']-1) if a['mean'] else None
            change=f"{percent:+.1f}%".replace('-','−') if percent is not None else '비교 보류'
            lines.append(f'| {label} | {fmt(a)} | {fmt(b)} | {change} |')
        lines+=['',f"정답·상태: 직접 {s['direct']['correct']}/3, tools {s['tools']['correct']}/3. 전체 원본에는 캐시·출력·명령 수·시간과 실패를 포함했습니다.",'']
        lines+=[f'동시 개선 판단: **{and_assessment(s)}**. 같은 완료 상태에서 총 토큰·시간이 줄고 모델의 셸 호출이 늘지 않는지 확인하며, 캐시 제외 지표가 증가하면 조건부로 분류합니다. 평균·소표본의 판정이며 안정적인 실사용 효과를 보장하지 않습니다.','']
        lines+=['각 실행은 새 세션이지만 모델 입력 캐시는 초기화·통제하지 않았습니다. 명령 하나를 독립적으로 한 번 쓰는 상황이나 다른 실제 작업 후 돌아오는 상황을 별도 조건으로 측정하지 않았으므로, 캐시 없는 최초 실행·비연속 재사용의 AND 달성은 미검증입니다. 연속 반복의 캐시 이득을 일반 사용의 전제로 삼지 않습니다.','']
        if s['comparison_eligible'] and s['change_percent']['uncached_plus_output']>0:lines+=['이 작업에서는 캐시 제외 평균이 직접 처리보다 높았습니다. 캐시 포함 총 토큰·범위와 함께 판단하며 일반적인 절감 효과를 주장하지 않습니다.','']
        elif s['comparison_eligible'] and s['change_percent']['input_plus_output']>0:lines+=['캐시 제외 평균은 줄었지만 캐시 포함 총 토큰은 증가했습니다. 캐시 적중은 입력 토큰의 분류를 바꾸며 캐시 포함 총량 증가 자체의 원인은 아닙니다. 호출 수·후처리·시간·실행별 편차를 함께 판단해야 하며 안정적인 작업 전체 절감 효과를 확인했다고 해석하지 않습니다.','']
        if task=='apply' and not batch['protocol'].get('completion_workflows'):
            a,b=s['direct']['metrics']['command_calls'],s['tools']['metrics']['command_calls']
            lines+=[f"실제 셸 명령 호출은 직접 {a['min']}–{a['max']}회, tools {b['min']}–{b['max']}회였습니다. 반복 호출에 따른 입력 재전송과 캐시 적중 차이가 집계에 포함됩니다. 이미 승인한 계획은 --apply 한 번으로 검증·반영·재검증하도록 안내를 개선했습니다.",'']
        if batch['protocol'].get('compact_content'):
            lines+=['같은 반복 비교 작업에서 --content-kinds modified로 추가·삭제의 본문만 생략했습니다. 경로·종류·SHA·건수와 수정 구간, 저장 기준은 보존했습니다. 앞선 18회 기록을 교체하지 않고 delta만 별도 6회 측정했습니다.','']
        if batch['protocol'].get('workflows'):
            lines+=['이전 실험과 요구 작업이 달라 과거 토큰 수치와의 차이를 코드 개선 효과로 해석하지 않습니다. 직접 처리에도 동일한 최종 정답·파일 상태·저장 계획 검증을 적용했습니다. 모델의 셸 명령 호출 수는 스크립트 내부 CLI 호출 수와 구분합니다.','']
        if batch['protocol'].get('completion_workflows') and batch['protocol'].get('completion_revision',1)==1 and task=='apply':
            lines+=['저장 계획 검사는 특정 version 1 형식을, 치환 번호 검사는 1부터 시작하는 번호를 요구했지만 실행 프롬프트에는 두 기준이 명시되지 않았습니다. 다른 형식에 같은 해시·검증 결과를 저장한 직접 실행도 실패할 수 있으므로, 이 실패를 직접 편집 기능의 결함으로 해석하지 않으며 변화율을 보류합니다. 실패·비용·작성한 명령은 수정하지 않고 보존했습니다.','']
        if batch['protocol'].get('completion_revision',1)==2:
            lines+=['앞선 전체 작업 실험에서 조회→편집의 계획 형식 탐색·잘못된 제출과 저장 계획 형식 안내 누락을 확인했습니다. CLI 도움말에 치환 JSON·필수 count·생성 형식을 넣고, 조회 안내와 양쪽의 저장 계획 형식을 명시했습니다. 변경한 inspect·apply만 12회 별도로 고정했고 delta는 재측정하지 않았습니다. 앞선 실패·불리한 비용은 그대로 보존했습니다.','']
        if batch['protocol'].get('batch_workflows') and (not batch['protocol'].get('recount_workflows') or task!='apply'):
            lines+=['직접 처리와 tools 모두 연속 작업의 입력 조회·처리·반영·검증을 로컬 스크립트로 묶을 수 있다고 동일하게 안내했습니다. 명령 수는 강제하지 않았습니다. tools의 저장 계획은 실제 바이트를 반영 전후 재검증하고 saved_plan으로 형식·파일 수·SHA 근거를 반환합니다. 중간 판단이 이미 요청 범위와 검증된 데이터로 결정되는 경우 모델에 다시 묻는 호출을 줄였습니다. 변경한 delta·apply만 별도 12회 측정했으며 원래 요구 결과·검증 범위는 같습니다.','']
        if batch['protocol'].get('recount_workflows') and task=='apply':
            lines+=['앞선 별도 12회 실험에서 apply가 오류 진단과 수정 호출을 나누면서 총 토큰이 높았습니다. 사용자가 이미 지시한 첫 파일·첫 치환의 횟수 수정만 --recount 1:1로 내부 진단·복사본 수정·전체 검증·계획 저장·반영·재검증에 통합했습니다. 0건·다른 항목의 오류·원본 SHA 변경은 계속 거부합니다. 원래 진단은 corrections에, 실제 저장 계획의 증명은 saved_plan에 유지합니다. 변경한 apply만 별도 6회 측정했으며 직접 처리에도 같은 배치 안내·수정 권한·정답·파일 상태 검증을 적용했습니다. 앞선 실패·높은 토큰 기록을 교체하지 않았습니다.','']
            rows=[row for row in batch['trials'] if row['task']==task and row['method']=='tools']
            if len(rows)==3 and all(row['command_calls']==1 for row in rows) and len({tuple(row['commands']) for row in rows})==1:
                total,uncached=(s['tools']['metrics'][key] for key in ('input_plus_output','uncached_plus_output'))
                lines+=[f"같은 단일 셸 명령으로 세 번 실행했습니다. tools 총 토큰은 {total['min']:,}–{total['max']:,}, 캐시 제외 토큰은 {uncached['min']:,}–{uncached['max']:,}였습니다. 캐시 적중을 통제하지 못했으므로 총 토큰·시간·호출의 감소와 캐시 제외 지표의 증가를 함께 보고하며 무조건적인 AND 달성으로 해석하지 않습니다.",'']
        if batch['protocol'].get('efficient_usage'):
            lines+=['추가 실험은 같은 CLI·설정·파일 작업에서 python3 실행 환경을 명시하고, 승인된 수정의 중복 미리보기 호출과 불필요한 전체 파일 출력을 줄였습니다. 이전 실패·비용을 교체하지 않았으며 아래 원본은 별도 조건의 '+str(len(batch['trials']))+'회 실험입니다.','']
        if batch['protocol'].get('guidance_revision',1)>1 and not batch['protocol'].get('workflows'):
            lines+=['delta 전용 변경 공개 안내를 apply 프롬프트에서 제거했고, 실행별 쓰기 범위를 작업 폴더와 fixture 제어 디렉터리로 제한했습니다. 이전 안내가 유발한 불필요한 조회·실패 호출은 과거 원본에 보존합니다.','']
        if task=='delta' and batch.get('evidence',{}).get('delta_output_comparison'):
            check=batch['evidence']['delta_output_comparison'];lines+=['동일한 원본·수정 바이트를 사용한 별도 CLI 출력 비교입니다. 이전 CLI는 전체 결과가 담기도록 결과 상한을 1 MiB로 높였습니다. compact 실험에서는 요청한 수정 본문만 남기고 생성·삭제 본문을 생략합니다. 모델 추가 호출은 0회이며 에이전트 작업 전체 지표와 구분합니다.','',f"직렬화 출력: {check['before_bytes']:,} → {check['after_bytes']:,}바이트. 수정 파일의 현재 줄 수: {check['before_modified_lines']} → {check['after_modified_lines']}. 이전·현재 JSON은 이벤트 자료의 preflight/delta/output-comparison.json에 보관합니다.",'']
        lines+=[f'[사용법](../../fs/{task}.md) · [원본 JSON: summary.{task}](data/{stem}.json)','']
        (out/(task+'.md')).write_text('\n'.join(lines),encoding='utf-8')
    combined={'summary':{task:selected[task][1]['summary'][task] for task in study.TASKS}}
    correct=sum(r['correct'] for batch in reports.values() for r in batch['trials']);calls=sum(len(batch['trials']) for batch in reports.values())
    lines=['# 파일시스템 벤치마크','',f'명령별 최신 결과입니다. 고정 실험 {len(reports)}개, 총 **{calls}회**를 보관했습니다. 정답·파일 상태 검증은 **{correct}/{calls}**회 통과했습니다. 실패한 실행을 제외하거나 교체하지 않았습니다.','']+table(combined,'',True)+['',
        '## 공통 조건과 한계','',f"사용자 기본 설정 `{settings['model']}` / `{settings['model_reasoning_effort']}`를 상속했습니다. 모델·추론 강도와 ignore-user-config 옵션은 지정하지 않았습니다. 실제 선택 모델·추론 강도는 이벤트에 나타나지 않아 설정 스냅샷과 인자로 기록했습니다.",'',
        '실제 프로덕션 CLI를 고정 소스에서 빌드하고 프롬프트·스키마·seed 20261010·각 실험의 순서·파일 작업을 모델 호출 전에 고정했습니다. 각 명령의 방식 순서를 교차하고 매번 새 세션·새 합성 작업 폴더를 사용했습니다. 직접 처리에도 rg·필터·Python·배치를 허용했고 명령 수를 강제하지 않았습니다. 직접 스크립트를 작성하는 비용은 포함하고 CLI 설치·빌드 비용은 제외했습니다.','',
        '캐시 제외 입력+출력은 input_tokens − cached_input_tokens + output_tokens, 캐시 포함 총 토큰은 input_tokens + output_tokens입니다. 추론 출력을 중복 합산하지 않습니다. 동시 개선은 총 토큰·완료 시간이 줄고 모델의 셸 호출이 늘지 않으며 캐시 제외 지표도 증가하지 않는 표본에서만 충족으로 표시합니다. 두 토큰 지표가 상충하면 조건부, 한쪽만 좋아지면 부분 개선으로 표시합니다. 캐시는 강제로 초기화하지 못했고 소표본 판정은 일반적 절감률을 뜻하지 않습니다. 둘 중 어느 방식이든 3회 모두 정답·상태를 만족하지 않으면 비교를 보류합니다. 실패·재시도도 비용에 포함하고 실패한 모델 실행을 교체하지 않았습니다.','',
        '실사용의 기본 평가 조건은 명령의 독립 1회 사용입니다. 다른 작업 후 재사용과 연속 반복은 별도 조건으로 구분합니다. 현재 자료는 고정된 실험 순서에서 새 세션으로 수행한 반복 표본이며, 입력 캐시를 통제하지 않았고 두 사용 조건을 별도로 검증하지 않았습니다. 첫 번째 표본에도 캐시가 있으므로 캐시 없는 최초 사용 결과가 아닙니다. 표의 AND 충족은 관측한 평균의 판정이며 독립 1회 사용·비연속 재사용에서도 성립한다고 주장하지 않습니다.','',
        '합성 자료의 대표 시나리오와 방식별 3회의 소표본입니다. 모든 파일 크기·옵션·오류 경로·실운영 평균·금액 절감을 대표하지 않습니다. 결과는 후처리 작성·추가 조회·실패 복구·반영·재검증을 포함한 에이전트 작업 전체 토큰이며 CLI 자체는 모델을 호출하지 않습니다. 모델 호출 전 샌드박스에서 실제 출력·파일 바이트·권한을 검증했습니다. 파일 읽기·수정만 사용하고 실제 GitHub 리소스는 변경하지 않았습니다.','',
        '토큰 측정은 Linux의 Codex sandbox와 /tmp에서 수행했습니다. CLI는 Go 표준 파일 API를 사용하며 Linux·macOS·Windows 빌드와 세 OS의 CI 테스트 구성을 제공합니다. 이번 Windows·macOS 실행 테스트를 완료했다는 뜻은 아닙니다.','',
        '최초 실험의 직접 delta 3회는 없는 python 명령을 사용한 뒤 변경 공개 단계에 실패해 비교에서 절감률을 보류했습니다. 오류·토큰을 원본에 유지했습니다. 추가 실험은 양쪽에 python3 실행 환경을 명시했고 같은 CLI·기본 설정·파일 작업으로 두 명령을 다시 비교했습니다. 도구 방식은 내부 검증이 있는 --apply 한 번과 필요한 줄 조회를 사용합니다.','',
        '두 번째 실험의 apply 안내에 delta 전용 변경 공개 문장이 섞여 불필요한 조회·실패 호출이 발생했습니다. 해당 비용과 정답 기록을 보존하되 안내와 쓰기 범위를 바로잡은 별도 6회 실험도 보존했습니다. 처음 세 실험의 CLI 구현은 동일합니다. 최신 개선 실험은 새 CLI와 확장된 작업으로 별도 비교했습니다.','',
        '전체 편집 실험의 최초 apply 안내는 저장 계획 형식과 치환 번호의 기준을 명시하지 않았지만 검사는 특정 version 1 형식·1부터 시작하는 번호를 요구했습니다. 결함 발견 시 중단한다는 계획과 달리 고정 순서를 끝까지 실행했으며 해당 편차를 원본 evidence.measurement_issues에 기록했습니다. 관련 실패·비용은 그대로 보존하고 변화율을 보류했습니다. 후속 실험에서는 양쪽에 같은 형식·번호 기준을 명시했습니다. inspect 안내도 실제 치환 스키마를 제시했습니다. 안내·CLI가 함께 바뀌어 각각의 효과를 분리하지 못합니다.','',
        '## 원본과 재현','',
        *[f"- {stem}: [전체 원본](data/{stem}.json) · [고정 소스·실행기](data/{stem}-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/{stem}-events.tar.gz)" for stem in sorted(reports)],
        '', '원본 JSON의 evidence.sha256과 protocol의 소스·실행기·CLI 지문으로 감사할 수 있습니다.','',
        '```sh','python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-study',
        '# 사전 검증 후, 같은 고정 자료로 18회 모델 실행',
        'python3 /tmp/fs-study/harness/run_fs_benchmark.py --root /tmp/fs-study --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-study',
        '# 안내·호출 방식 개선 실험은 두 명령만 별도 고정',
        'python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-efficient --tasks delta apply --efficient-usage',
        'python3 /tmp/fs-efficient/harness/run_fs_benchmark.py --root /tmp/fs-efficient --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-efficient',
        '# apply 안내를 바로잡은 최신 조건만 6회 실행',
        'python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-apply --tasks apply --efficient-usage',
        'python3 /tmp/fs-apply/harness/run_fs_benchmark.py --root /tmp/fs-apply --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-apply',
        '# 최신 여섯 개선을 세 작업으로 묶어 18회 실행',
        'python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-workflows --workflows',
        'python3 /tmp/fs-workflows/harness/run_fs_benchmark.py --root /tmp/fs-workflows --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-workflows',
        '# delta 본문 선택을 보완한 같은 작업만 6회 실행',
        'python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-delta-compact --workflows --compact-content --tasks delta',
        'python3 /tmp/fs-delta-compact/harness/run_fs_benchmark.py --root /tmp/fs-delta-compact --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-delta-compact',
        '# 원문 조회→실제 편집, 실제 반복 관찰, 오류 복구→반영의 전체 작업',
        'python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-completion --completion-workflows',
        'python3 /tmp/fs-completion/harness/run_fs_benchmark.py --root /tmp/fs-completion --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-completion',
        '# 계획 형식 안내를 보완한 작업만 12회 별도 측정',
        'python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-completion-v2 --completion-workflows --tasks inspect apply',
        'python3 /tmp/fs-completion-v2/harness/run_fs_benchmark.py --root /tmp/fs-completion-v2 --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-completion-v2',
        '# 실제 저장 계획 증명과 연속 실행으로 delta·apply의 AND 조건 재검증',
        'python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-and --batch-workflows --tasks delta apply',
        'python3 /tmp/fs-and/harness/run_fs_benchmark.py --root /tmp/fs-and --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-and',
        '# 이미 지시된 특정 항목의 횟수 수정을 통합한 apply만 6회',
        'python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-recount --recount-workflows --tasks apply',
        'python3 /tmp/fs-recount/harness/run_fs_benchmark.py --root /tmp/fs-recount --resume --run-models',
        'python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-recount','```','',
        '현재 저장소 실행기는 안내 수정본입니다. 과거 조건 그대로의 재현에는 각 실험에 동봉한 고정 실행기를 사용합니다.','',
        '공개 source.tar.gz의 source에서 CLI를 빌드하고 동봉 실행기로 같은 파일 작업을 재현할 수 있습니다. 모델 답변과 토큰 수가 동일해지는 것을 보장하지 않습니다. [사용법](../../fs/README.md).','']
    (out/'README.md').write_text('\n'.join(lines),encoding='utf-8')
    index=repo/'docs/benchmarks/README.md'
    text=index.read_text(encoding='utf-8');row='| [파일시스템](fs/README.md) | inspect·delta·apply의 작업 전체 토큰·파일 상태 비교 |\n'
    if '| [파일시스템]' not in text:index.write_text(text.rstrip()+'\n'+row,encoding='utf-8')
    for relative,korean in [('README.md',False),('docs/README.ko.md',True)]:
        p=repo/relative;text=p.read_text(encoding='utf-8');heading='### 파일시스템 실측' if korean else '### Filesystem measurements';end='## 설치' if korean else '## Install';base='benchmarks/fs/' if korean else 'docs/benchmarks/fs/'
        block=[heading,'',f"{date} 기준 `{settings['model']}` / `{settings['model_reasoning_effort']}`, 명령별 방식마다 3회. 캐시 제외 입력+출력 평균과 캐시 포함 총 토큰 변화를 함께 비교했습니다." if korean else f"Measured on {date} with inherited `{settings['model']}` / `{settings['model_reasoning_effort']}` defaults: three trials per method for each command. Means use uncached input + output; total changes include cached input.",'']+table(combined,base,korean)+['','독립 1회 사용·다른 실제 작업 후 재사용은 별도 검증하지 않았습니다. 캐시를 통제하지 않아 최초 표본도 캐시 없는 실행이 아닙니다. AND 판정은 측정 표본의 평균에 한정합니다.' if korean else 'Standalone one-off use and return after other real work have not been tested separately. Cache was uncontrolled, including in the first trial; AND assessments apply only to observed sample means.','',f'[조건·한계·원본·재현]({base}README.md).' if korean else f'[Protocol, limits, evidence, and reproduction]({base}README.md).','']
        stop=text.index(end)
        start=text.find(heading,text.index('## 토큰 사용량 실측' if korean else '## Token usage measurements'),stop)
        if start>=0:text=text[:start]+text[stop:];stop=start
        p.write_text(text[:stop]+'\n'.join(block)+'\n'+text[stop:],encoding='utf-8')
    from actionable_benchmark_docs import render as render_actionable
    render_actionable(repo)


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--root',type=Path,required=True);args=parser.parse_args();root=args.root.resolve();repo=Path(__file__).resolve().parents[2]
    report=json.loads((root/'report.json').read_text(encoding='utf-8'));protocol=report['protocol'];rows=report['trials'];study.validate_frozen(root,protocol)
    tasks=protocol['tasks'];calls=len(tasks)*2*study.REPETITIONS
    assert len(rows)==protocol['max_model_calls']==calls and protocol['schedule']==study.schedule(tasks)
    assert len({(r['task'],r['method'],r['repetition']) for r in rows})==calls
    assert report['summary']==study.summarize(rows,tasks)
    for row in rows:
        p=root/'runs'/row['task']/f"{row['repetition']}-{row['method']}";raw=(p/'events.jsonl').read_bytes();assert study.sha(raw)==row['events_sha256']
        events=[json.loads(line) for line in raw.splitlines()];assert [e['usage'] for e in events if e.get('type')=='turn.completed']==[row['usage']]
        assert row['actual']==json.loads((p/'answer.json').read_text(encoding='utf-8'))
        args=row['execution_args'];assert '--model' not in args and '--ignore-user-config' not in args and not any('model_reasoning_effort=' in a for a in args)
        u=row['usage'];assert row['input_plus_output']==u['input_tokens']+u['output_tokens'];assert row['uncached_plus_output']==u['input_tokens']-u['cached_input_tokens']+u['output_tokens']
    stem=('study-completion-v'+str(protocol['completion_revision']) if protocol.get('completion_revision',1)>1 else 'study-completion') if protocol.get('completion_workflows') else 'study-workflows-compact' if protocol.get('compact_content') else 'study-workflows' if protocol.get('workflows') else (('study-efficient-v'+str(protocol['guidance_revision']) if protocol.get('guidance_revision',1)>1 else 'study-efficient') if protocol.get('efficient_usage') else 'study')
    out=repo/'docs/benchmarks/fs/data';out.mkdir(parents=True,exist_ok=True)
    source=[('source/'+r['path'],(root/'source'/r['path']).read_bytes()) for r in protocol['source_manifest']]
    source.append(('scripts/benchmarks/run_fs_benchmark.py',(root/'harness/run_fs_benchmark.py').read_bytes()))
    source.extend(('scripts/benchmarks/'+name,(root/'harness'/name).read_bytes()) for name in protocol.get('runner_manifest',{}))
    archive(out/(stem+'-source.tar.gz'),source)
    allowed={'events.jsonl','prompt.txt','schema.json','answer.json','state-verification.json','actual-files.json','native.json','output-comparison.json'}
    evidence=[(str(p.relative_to(root)),p.read_bytes()) for folder in ('runs','preflight') for p in (root/folder).rglob('*') if p.is_file() and p.name in allowed]
    evidence.append(('protocol.json',(root/'protocol.json').read_bytes()));archive(out/(stem+'-events.tar.gz'),evidence)
    report['evidence']={'sha256':{name:study.sha((out/name).read_bytes()) for name in (stem+'-source.tar.gz',stem+'-events.tar.gz')},'total_input_plus_output':sum(r['input_plus_output'] for r in rows),'total_uncached_plus_output':sum(r['uncached_plus_output'] for r in rows)}
    if protocol.get('completion_workflows') and protocol.get('completion_revision',1)==1:
        report['evidence']['measurement_issues']=[{
            'task':'apply',
            'issue':'The frozen prompt omitted the exact saved-plan format and replacement-index origin required by the reference verifier.',
            'comparison':'Withheld; original answers, verification failures and costs are retained unchanged.',
            'protocol_deviation':'The runner completed the fixed schedule despite its inclusion policy to abort on fixture defects. The subsequent correction uses a separate frozen batch and does not replace these trials.'}]
    if (root/'preflight/delta/output-comparison.json').exists():
        comparison=json.loads((root/'preflight/delta/output-comparison.json').read_text())
        report['evidence']['delta_output_comparison']={key:comparison[key] for key in ('before_bytes','after_bytes')}
        for name in ('before','after'):
            report['evidence']['delta_output_comparison'][name+'_modified_lines']=sum(len(span['lines']) for change in comparison[name]['changes'] if change['kind']=='modified' for span in change.get('ranges',[]))
    (out/(stem+'.json')).write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n',encoding='utf-8');render(repo,{p.stem:json.loads(p.read_text(encoding='utf-8')) for p in sorted(out.glob('study*.json'))})
    print(json.dumps({'calls':calls,'correct':sum(r['correct'] for r in rows),'report':str(out.parent/'README.md')}))


if __name__=='__main__':main()
