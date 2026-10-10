"""Render latest completion evidence without mixing different work scopes."""
import json
from datetime import datetime, timedelta, timezone
from pathlib import Path

CASES = {
 'apply-errors': ('fs apply: 일괄 오류 진단', 'fs/apply.md', '12개 파일의 잘못된 횟수 8개를 모아 보고하고 파일·입력·계획을 쓰지 않는 작업입니다. 같은 파일의 첫 잘못된 치환 뒤 조건은 추측하지 않습니다.'),
 'apply-groups': ('fs apply: 공통 치환', 'fs/apply.md', '자연어 지시에서 입력/스크립트를 작성해 12개 파일의 두 치환을 적용하고, 기존 바이트·CRLF/LF·권한·원본 SHA 계획 저장·최종 파일과 저장 계획을 검증했습니다.'),
 'pr-reviews': ('github pr reviews: 리뷰 반영', 'github/pr/reviews.md', '현재 미해결 리뷰의 전체 본문·위치·diff와 최신 head를 확인해 실제 settings.py를 수정하고 원래 CRLF·권한·timeout 값을 검증했습니다. 긴 본문 두 쌍과 짧은 본문 한 쌍입니다.'),
 'pr-inspect': ('github pr inspect: 복귀 후 반영', 'github/pr/inspect.md', '이전 원문 없이 제공된 상태 기준으로 복귀해 현재 미해결 요청을 실제 파일에 반영했습니다. 변화 없음·새 답글·해결 완료를 각각 한 쌍씩 비교했습니다. 해결 완료에서는 파일을 바꾸지 않습니다.')}


def remove_notes(lines, prefixes):
    result = []
    skip_blank = False
    for line in lines:
        if any(line.startswith(prefix) for prefix in prefixes):
            skip_blank = True
            continue
        if skip_blank and not line:
            skip_blank = False
            continue
        skip_blank = False
        if not line and result and not result[-1]:
            continue
        result.append(line)
    return result


def published(repo):
    path = repo/'docs/benchmarks/actionable/data/study.json'
    if not path.exists():
        return None
    report = json.loads(path.read_text(encoding='utf-8'))
    # Original failures remain in the raw record. A documented verifier defect
    # can be reassessed from the same immutable state/commands with no reruns.
    if report.get('protocol_deviations'):
        report['summary'] = report['reassessed_summary']
    return report


def measured_date(report):
    return datetime.fromisoformat(report['protocol']['measured_at_utc']).astimezone(timezone(timedelta(hours=9))).date().isoformat()


def assessment(summary, korean=True):
    if not summary['comparison_eligible']:
        return '정답/상태 부족·판정 보류' if korean else 'Withheld: validation incomplete'
    direct, tools = (summary[m]['metrics'] for m in ('direct', 'tools'))
    efficiency = tools['seconds']['mean'] < direct['seconds']['mean'] and tools['command_calls']['mean'] <= direct['command_calls']['mean']
    tokens = tools['input_plus_output']['mean'] < direct['input_plus_output']['mean']
    if efficiency and tokens:
        if tools['uncached_plus_output']['mean'] > direct['uncached_plus_output']['mean']:
            return '조건부·캐시 지표 상충' if korean else 'Conditional: cache metrics conflict'
        return '표본에서 AND 충족' if korean else 'AND met in sample'
    return '부분 개선·AND 미충족' if korean else 'Partial improvement; AND unmet'


def change(summary, key):
    if not summary['comparison_eligible']:
        return '비교 보류'
    a, b = (summary[m]['metrics'][key]['mean'] for m in ('direct', 'tools'))
    return f'{100*(b/a-1):+.1f}%'.replace('-', '−') if a else '—'


def metrics_table(summary):
    lines = ['| 지표 | 직접 처리 평균 [최소–최대] | tools 평균 [최소–최대] | 변화 |', '|---|---:|---:|---:|']
    for key, label in [('input_plus_output', '캐시 포함 총 입력+출력'), ('uncached_plus_output', '캐시 제외 입력+출력'),
                       ('seconds', '완료 시간(초)'), ('command_calls', '셸 실행 항목 수')]:
        def fmt(method):
            m = summary[method]['metrics'][key]
            return f"{m['mean']:,.2f} [{m['min']:,.2f}–{m['max']:,.2f}]" if key in ('seconds', 'command_calls') else f"{m['mean']:,.0f} [{m['min']:,}–{m['max']:,}]"
        lines.append(f'| {label} | {fmt("direct")} | {fmt("tools")} | {change(summary, key)} |')
    return lines


def render_command(repo, report, target, cases, common):
    path = repo/'docs/benchmarks'/target
    prefix = '../' * (len(Path(target).parts)-1)
    lines = [f'# `tools {target.removesuffix(".md").replace("/", " ")}` 벤치마크', '',
        f"최신 작업 전체 측정: **{measured_date(report)}**(한국시간). 각 시나리오·방식별 3회, 기본 `{report['protocol']['configured_defaults']['model']}` / `{report['protocol']['configured_defaults']['model_reasoning_effort']}`.", '',
        f'[공통 조건·원본·재현]({prefix}actionable/README.md). 이전에 다른 작업으로 측정한 숫자와 코드 효과를 직접 비교하지 않습니다.', '']
    for case in cases:
        summary = report['summary'][case]
        lines += ['## '+CASES[case][0].split(': ', 1)[1], '', CASES[case][2], ''] + metrics_table(summary) + ['']
        direct, tools = summary['direct'], summary['tools']
        lines += [f"정답·최종 상태: 직접 {direct['correct']}/{direct['n']}, tools {tools['correct']}/{tools['n']}. **{assessment(summary)}**.", '']
    if 'apply-groups' in cases:
        proof = common['apply-groups'][0]['response']
        lines += [f"같은 치환을 펼친 입력은 {proof['expanded_input_bytes']:,}바이트, 그룹 입력은 {proof['grouped_input_bytes']:,}바이트입니다. 입력 직렬화 비교이며 작업 전체 토큰과 구분합니다. 저장 계획은 원래 개별 파일·SHA 형식으로 검증했습니다.", '']
    if 'apply-errors' in cases:
        proof = common['apply-errors'][0]['response']
        lines += [f"모델 없는 개선 전 CLI 비교에서는 잘못된 파일만 제외하며 재검증해 {proof.get('before_cli_calls', 0)}회, 개선 후에는 1회로 같은 8개 오류를 수집했습니다. 셸에서 배치하면 모델 호출은 추가하지 않아도 되므로 이 값을 모델 호출 감소라고 해석하지 않습니다.", '']
    if 'apply-groups' in cases and report.get('protocol_deviations'):
        lines += ['원래 검증기는 입력 파일 이름을 spec.json으로 제한해 tools 2회차의 허용된 batch-spec.json을 실패로 판정했습니다. 원래 실패·전체 실행 기록을 보존하고, 기록된 파일 SHA와 명령으로 정확한 공통 입력을 복원해 같은 결과를 재검증했습니다. 모델 실행을 교체하지 않았으며 표의 토큰·시간·실행 수는 원래 그대로입니다. 이 표의 정답/상태 판정은 그 검증 조건 보정을 반영합니다. [원본의 protocol_deviations·summary·reassessed_summary 참조]('+prefix+'actionable/data/study.json).', '']
    lines += ['모델 입력 캐시는 통제하지 못했습니다. AND 판정은 이 표본의 평균에 한정하며 캐시 없는 최초 실행이나 안정적인 실사용 효과를 보장하지 않습니다. PR 복귀는 다른 모델 작업을 실제 수행한 세션이 아닌 문맥이 없는 복귀 fixture입니다.', '',
              '셸 실행 항목 수는 events의 command_execution 수입니다. 모델 도구 호출·명령 안의 CLI 호출·API 요청 수와 같은 지표가 아닙니다. 실패·재시도·입력 작성·후처리·실제 최종 검증은 작업 전체 토큰·시간에 포함됩니다.', '',
              f'[사용법]({prefix}../{target}) · [이번 원본 JSON]({prefix}actionable/data/study.json)', '']
    path.write_text('\n'.join(lines), encoding='utf-8')


def render(repo):
    report = published(repo)
    if not report:
        return
    root = repo/'docs/benchmarks/actionable'
    common = json.loads((root/'data/native.json').read_text(encoding='utf-8'))
    rows = ['| 시나리오 | 총 토큰 변화 | 캐시 제외 변화 | 시간 변화 | 셸 실행 직접→tools | 정답·상태 | 판단 |', '|---|---:|---:|---:|---:|---:|---|']
    for case, (name, target, _) in CASES.items():
        s = report['summary'][case]
        a, b = s['direct'], s['tools']
        rows.append(f"| [{name}](../{target}) | {change(s,'input_plus_output')} | {change(s,'uncached_plus_output')} | {change(s,'seconds')} | {a['metrics']['command_calls']['mean']:.2f}→{b['metrics']['command_calls']['mean']:.2f} | {a['correct']+b['correct']}/{a['n']+b['n']} | {assessment(s)} |")
    lines = ['# 필요한 정보를 유지하는 완료 작업 비교', '', f"{measured_date(report)} 기준 네 변경 시나리오를 직접 처리와 각 3회 비교했습니다. 기본 모델·추론 강도를 상속했고 코드·실행기·프롬프트·정답·순서를 모델 실행 전에 고정했습니다.", ''] + rows + ['',
        '기준은 같은 요청·정답·파일 상태·검증을 정확히 끝내면서 작업 효율 향상 AND 전체 입력+출력 절감을 함께 충족하는 것입니다. 시간은 줄고 셸 실행 항목 수는 늘지 않아야 하며, 캐시 제외 토큰이 증가하면 조건부로 표시합니다. 어느 방식이든 3회 모두 정답·상태 검증에 통과하지 않으면 변화율 비교를 보류합니다.', '',
        '각 실행은 새 세션·임시 작업 폴더로 시작하며 직접 처리도 자연스러운 배치·필터·스크립트 작성을 허용했습니다. 그룹 치환은 자연어 지시부터 입력 작성·전체 검증·저장·반영·실제 파일/계획 재검증을 포함합니다. 오류 진단은 실제 수정 없이 전체 오류 보고와 파일 보존을 완료합니다. 리뷰는 자료 조회 뒤 실제 파일 수정·검증까지 포함합니다.', '',
        '실행 인자에는 모델·추론 강도·ignore-user-config를 지정하지 않았습니다. 모델 이름·강도는 사용자 설정 스냅샷이며 실행 이벤트가 실제 선택 값을 별도로 제공하지 않는 한계가 있습니다.', '',
        'PR inspect는 변화 없음·새 답글·해결 완료를 각각 한 쌍씩 사용했습니다. 이전 상태 기준은 작업 입력으로 제공했으므로 그 생성 비용은 복귀 측정에서 제외했습니다. 실제 다른 모델 작업을 수행한 뒤 복귀하거나 최초 기준 생성의 절감을 검증한 것은 아닙니다. 새 세션은 모델 입력 캐시 없는 실행을 뜻하지 않으며 캐시 적중을 강제로 통제하지 못했습니다.', '',
        '셸 실행 항목 수는 events의 command_execution 수이며 모델 도구 호출 수와 동일하지 않습니다. CLI 내부의 API 요청·파일 읽기·폴링 횟수와도 구분합니다. 원본에는 실행 인자, 명령, 실패·재시도, API 접근, 캐시 포함/제외 토큰, 평균·범위·편차와 각 쌍의 변화율을 보관합니다. 실패한 모델 실행을 교체하거나 불리한 쌍을 제외하지 않습니다.', '',
        '최초 검증은 23/24였습니다. 입력 작성이 허용된 공통 치환에서 입력 파일 이름을 spec.json만 허용한 검증기 오류 1건을 발견했습니다. batch-spec.json의 기록된 SHA와 실행 명령으로 정확한 입력을 복원해 같은 파일·계획 결과를 재검증했고 조건 보정 후 24/24입니다. 원래 실패·summary와 재검증 결과를 함께 공개합니다. 모델 재실행·실패 제외·토큰/시간 교체는 0회이며 표는 같은 24회 전체의 수치입니다.', '',
        '실제 CLI·Git·gh와 Linux 파일시스템을 사용하고 GitHub API만 로컬 GraphQL/REST 서버로 연결했습니다. 작은 고정 표본의 결과이며 실제 GitHub 성능·금액 절감·macOS/Windows 런타임·모든 옵션·작업을 대표하지 않습니다. 공유 compact 변경의 CI 긴 로그 등 다른 작업은 이번 모델 측정에 포함되지 않았습니다. 원문 보존으로 한 응답이 커지는 경우도 포함해 작업 완료 비용을 비교했습니다.', '',
        '## 원본과 재현', '',
        '- [작업 전체 결과·프로토콜](data/study.json) · [사전 기능 검증](data/native.json)',
        '- [고정 소스·실행기](data/study-source.tar.gz) · [모든 이벤트·답변·상태 검증](data/study-events.tar.gz)', '',
        '압축 파일 SHA-256은 원본 JSON의 evidence.sha256에 있습니다. source_manifest는 파일별 SHA이며 저장소 현재 코드의 결과와 과거 고정 코드 결과를 혼합하지 않습니다.', '',
        '```sh', 'python3 -m pip install --require-hashes -r scripts/benchmarks/requirements.txt',
        '# 모델 없는 사전 검증', 'python3 scripts/benchmarks/run_actionable_benchmark.py --root /tmp/actionable-study',
        '# 고정 실행기의 같은 기록에서 24회 수행',
        'python3 /tmp/actionable-study/harness/run_actionable_benchmark.py --root /tmp/actionable-study --resume --run-models',
        'python3 scripts/benchmarks/publish_actionable_benchmark.py --root /tmp/actionable-study',
        '# 모델 호출 없이 문서만 재생성', 'python3 scripts/benchmarks/publish_actionable_benchmark.py --render-only', '```', '',
        '과거 소스 재현은 source 압축의 source와 harness를 풀고 harness를 source/scripts/benchmarks로 복사한 뒤 그 고정 실행기에서 새 root를 생성합니다. 개선 전 Go 소스·API 주입 overlay도 before/source에 보관합니다. 기본 설정이 같아도 모델 응답·캐시·토큰 수의 동일성을 보장하지 않습니다. 개선 전 CLI 비교는 사전 기능 검증이며 작업 전체 모델 토큰 실측이 아닙니다.', '']
    (root/'README.md').write_text('\n'.join(lines), encoding='utf-8')
    for target, cases in [('fs/apply.md', ['apply-errors', 'apply-groups']), ('github/pr/reviews.md', ['pr-reviews']), ('github/pr/inspect.md', ['pr-inspect'])]:
        render_command(repo, report, target, cases, common)
    for group in ('github', 'fs'):
        path = repo/'docs/benchmarks'/group/'README.md'
        text = path.read_text(encoding='utf-8')
        note = f'이번 변경 동작의 최신 완료 작업 비교는 [공통 조건·원본](../actionable/README.md)에 있습니다. 원문 조회·입력 작성·실제 반영/파일 보존·재검증까지 포함하며 기존의 다른 시나리오와 수치를 합산하지 않습니다.'
        text = '\n'.join(remove_notes(text.splitlines(), ['이번 변경 동작의 최신 완료 작업 비교는']))
        lines = text.splitlines()
        if group == 'fs':
            updated = []
            apply_inserted = False
            for line in lines:
                if line.startswith('| [`fs apply'):
                    if not apply_inserted:
                        for case, label in [('apply-errors', 'fs apply: 오류 진단'), ('apply-groups', 'fs apply: 공통 치환')]:
                            s = report['summary'][case]
                            a, b = s['direct'], s['tools']
                            updated.append(f"| [`{label}`](apply.md) | {a['metrics']['uncached_plus_output']['mean']:,.0f} | {b['metrics']['uncached_plus_output']['mean']:,.0f} | {change(s,'uncached_plus_output')} | {change(s,'input_plus_output')} | {a['correct']+b['correct']}/{a['n']+b['n']} | {assessment(s)} |")
                        apply_inserted = True
                    continue
                if line.startswith('명령별 최신 결과입니다. 고정 실험 9개'):
                    line = '명령별 최신 결과입니다. apply의 최신 두 시나리오는 아래에 구분해 표시합니다. 보관된 과거 FS 실험 9개·108회와 그 실패 6회는 원본에서 유지합니다.'
                updated.append(line)
            lines = updated
        lines[2:2] = [note, '']
        path.write_text('\n'.join(lines)+'\n', encoding='utf-8')
    patch_readmes(repo, report)
    index = repo/'docs/benchmarks/README.md'
    text = index.read_text(encoding='utf-8')
    row = '| [변경 동작 완료 비교](actionable/README.md) | 일괄 오류·공통 치환·리뷰 반영·PR 복귀, 24회 및 원래 실패/재검증 근거 |'
    if '[변경 동작 완료 비교]' not in text:
        at = text.find('\n\n[전체 명령 점검]')
        if at < 0:
            text = text.rstrip()+'\n'+row+'\n'
        else:
            text = text[:at]+'\n'+row+text[at:]
        index.write_text(text, encoding='utf-8')

    from usage_benchmark_docs import render as render_usage
    render_usage(repo)


def patch_readmes(repo, report):
    date = measured_date(report)
    for relative, korean in [('README.md', False), ('docs/README.ko.md', True)]:
        path = repo/relative
        text = path.read_text(encoding='utf-8')
        lines = text.splitlines()
        base = 'benchmarks/' if korean else 'docs/benchmarks/'
        output = []
        fs_replaced = False
        for line in lines:
            case = next((c for c in ('pr-reviews', 'pr-inspect') if f'[`github {c.replace("-", " ", 1)}`]' in line), None)
            if case:
                s = report['summary'][case]
                a, b = s['direct'], s['tools']
                target = CASES[case][1]
                line = f"| [`github {case.replace('-', ' ', 1)}`]({base}{target}) | {date} | {a['metrics']['uncached_plus_output']['mean']:,.0f} | {b['metrics']['uncached_plus_output']['mean']:,.0f} | {change(s,'uncached_plus_output')} | {change(s,'input_plus_output')} |"
            if '[`fs apply' in line:
                if fs_replaced:
                    continue
                fs_replaced = True
                for fs_case, label in [('apply-errors', 'fs apply: 오류 진단' if korean else 'fs apply: validation'), ('apply-groups', 'fs apply: 공통 치환' if korean else 'fs apply: shared edits')]:
                    s = report['summary'][fs_case]
                    a, b = s['direct'], s['tools']
                    output.append(f"| [`{label}`]({base}fs/apply.md) | {a['metrics']['uncached_plus_output']['mean']:,.0f} | {b['metrics']['uncached_plus_output']['mean']:,.0f} | {change(s,'uncached_plus_output')} | {change(s,'input_plus_output')} | {a['correct']+b['correct']}/{a['n']+b['n']} | {assessment(s,korean)} |")
                continue
            output.append(line)
        # This paragraph applies to the new rows only, keeping older scopes explicit.
        note = (f'리뷰·PR 복귀·apply 두 시나리오의 최신 수치는 입력 작성·실제 완료 검증을 포함합니다. [범위·캐시 한계·원본]({base}actionable/README.md).' if korean else
                f'Latest review, PR return, and two apply scenarios include input authoring and final-state checks. [Scopes, cache limits, and evidence]({base}actionable/README.md).')
        clean = remove_notes(output, ['리뷰·PR 복귀·apply 두 시나리오의 최신 수치는', 'Latest review, PR return, and two apply scenarios include'])
        at = next(i for i, line in enumerate(clean) if line == ('## 설치' if korean else '## Install'))
        clean[at:at] = [note, '']
        path.write_text('\n'.join(clean)+'\n', encoding='utf-8')
