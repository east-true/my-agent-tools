"""명령별 최신 결과를 렌더링하며 진단 실험은 별도로 보존한다."""
import json
import statistics
from datetime import datetime, timedelta, timezone
from pathlib import Path
from actionable_benchmark_docs import assessment as base_assessment, change, measured_date, metrics_table, remove_notes

def assessment(summary, korean=True):
    if summary['comparison_eligible']:
        direct = summary['direct']['metrics']
        tools = summary['tools']['metrics']
        if all(tools[key]['mean'] >= direct[key]['mean'] for key in ('seconds', 'command_calls', 'input_plus_output', 'uncached_plus_output')):
            return 'AND 미충족·실측 이득 없음' if korean else 'AND unmet; no measured advantage'
    result = base_assessment(summary, korean)
    if result == ('표본에서 AND 충족' if korean else 'AND met in sample'):
        direct_api = summary['direct']['metrics'].get('api_calls')
        tools_api = summary['tools']['metrics'].get('api_calls')
        if direct_api and tools_api and tools_api['mean'] > direct_api['mean']:
            return '조건부·API 호출 증가' if korean else 'Conditional: more API calls'
    return result
CASES = {'pr-reviews': ('리뷰 반영', 'github/pr/reviews.md', '리뷰 원문·diff·위치를 수집하고 실제 파일 수정, CRLF·권한·timeout 보존 및 수정 후 새 head 확인까지 수행했습니다. 입력 작성·저장할 때의 읽기·재시도도 포함합니다.'), 'pr-conversation': ('일반 대화 포함 조회', 'github/pr/reviews.md', 'inline 스레드·제출 리뷰(PENDING 제외)·일반 대화의 원문을 수집해 수·최신 안내·현재 head를 확인했습니다. 직접 처리에는 세 범위의 GraphQL 자연 배치도 허용했습니다.'), 'junit': ('JUnit 결과 판독', 'fs/test-results.md', '보고서 합계와 모든 실패·오류·skip의 정확한 진단, 파일 보존을 확인했습니다. 중첩 suite를 중복 합산하지 않고 보고서 판독을 이번 테스트 실행의 증명으로 해석하지 않았습니다.'), 'markdown': ('Markdown 구조·섹션', 'fs/inspect.md', '두 문서의 전체 제목 위치·순번과 지정한 Build 섹션의 원문·SHA를 반환했습니다. fenced code의 가짜 제목, 중복 제목의 두 번째 선택, Setext·CRLF·마지막 개행 없음도 포함합니다. 문서 조회 작업이며 과거 파일 편집 실험과 같은 범위가 아닙니다.'), 'fs-file': ('명시한 raw 파일 반영', 'fs/inspect.md', '초기 값을 가정하지 않고 파일의 실제 원문·SHA를 읽어 retry_budget을 3만큼 늘렸습니다. CRLF·마지막 개행 없음·다른 바이트·권한과 실제 반영 후 SHA를 확인했습니다. 직접 처리에는 한 스크립트의 읽기·수정·검증을 허용했습니다.')}

def summarize(rows, repetitions):
    groups = {}
    for method in ('direct', 'tools'):
        selected = [r for r in rows if r['method'] == method]
        measured = [r for r in selected if r['usage'] is not None]
        metrics = {}
        for key in ('input_plus_output', 'uncached_plus_output', 'seconds', 'command_calls', 'cli_calls', 'api_calls'):
            values = [r[key] for r in measured]
            if values:
                metrics[key] = {'mean': statistics.mean(values), 'min': min(values), 'max': max(values), 'stdev': statistics.stdev(values) if len(values) > 1 else 0}
        groups[method] = {'n': len(selected), 'correct': sum((r['correct'] for r in selected)), 'metrics': metrics}
    groups['comparison_eligible'] = all((g['n'] == g['correct'] == repetitions for g in groups.values()))
    return groups

def published(repo):
    path = repo / 'docs/benchmarks/usage/data/index.json'
    if not path.exists():
        return None
    index = json.loads(path.read_text())
    reports = {name: json.loads((path.parent / value['file']).read_text()) for name, value in index['cohorts'].items()}
    return (index, reports)

def summary_for(report, case, error=False):
    rows = [r for r in report['trials'] if r['task'] == case and (case != 'junit' or (r['repetition'] == 3 if error else r['repetition'] != 3))]
    return summarize(rows, 1 if case == 'junit' and error else 2 if case == 'junit' else 3)

def table(summary):
    lines = metrics_table(summary)
    for key, label in [('cli_calls', 'CLI 실제 실행 수'), ('api_calls', 'API 요청 수')]:

        def fmt(method):
            metric = summary[method]['metrics'][key]
            return f'{metric['mean']:.2f} [{metric['min']}–{metric['max']}]'
        lines.append(f'| {label} | {fmt('direct')} | {fmt('tools')} | {change(summary, key)} |')
    return lines

def render(repo):
    loaded = published(repo)
    if not loaded:
        return
    index, reports = loaded
    latest = {case: (name, reports[name]) for case, name in index['latest'].items()}
    directory = repo / 'docs/benchmarks/usage'
    rows = ['| 작업 | 방식별 n | 총 토큰 | 캐시 제외 | 시간 | 셸 직접→tools | CLI tools | API 직접→tools | 판단 |', '|---|---:|---:|---:|---:|---:|---:|---:|---|']
    for case, (name, report) in latest.items():
        s = summary_for(report, case)
        a, b = (s['direct'], s['tools'])
        rows.append(f'| [{CASES[case][0]}](../{CASES[case][1]}) | {a['n']} | {change(s, 'input_plus_output')} | {change(s, 'uncached_plus_output')} | {change(s, 'seconds')} | {a['metrics']['command_calls']['mean']:.2f}→{b['metrics']['command_calls']['mean']:.2f} | {b['metrics']['cli_calls']['mean']:.2f} | {a['metrics']['api_calls']['mean']:.2f}→{b['metrics']['api_calls']['mean']:.2f} | {assessment(s)} |')
    count = sum((len(r['trials']) for r in reports.values()))
    correct = sum((t['correct'] for r in reports.values() for t in r['trials']))
    lines = ['# 사용 기록에서 찾은 작업의 완료 비교', '', f'실제 사용 기록의 필요를 합성 작업으로 재현했습니다. 공개된 {count}회 모두의 기록을 유지하며 정답·상태는 {correct}/{count}입니다. 최신 행은 해당 작업의 가장 최근 고정 소스·안내·검증 조건만 사용합니다. 서로 다른 실험을 합산하거나 이전 수치와의 차이를 코드 효과로 해석하지 않습니다.', ''] + rows + ['', 'JUnit의 일반 판독 2쌍과 잘못된 XML 처리 1쌍은 구분합니다. 원래 실패한 테스트의 보고서를 읽는 것은 일반 판독이며 XML 구조 오류 처리는 별도 시나리오입니다. 그 오류 처리 실행도 전체 기록에서 제외하지 않습니다.', '', '작업 효율은 시간과 셸 실행 항목 수를 함께 평가하며, 캐시 포함 총 입력+출력을 기준으로 토큰을 비교합니다. 캐시 제외 지표가 증가하면 조건부로 표시합니다. 표본 평균의 AND 조건 충족과 안정적인 실사용 효과는 구분합니다.', '', '기본 모델·추론 강도를 상속했습니다. 실행 인자에 모델/강도/ignore-user-config를 넣지 않았으며 설정 스냅샷은 각 원본 protocol에 있습니다. 모델 입력 캐시는 초기화하거나 통제하지 못했습니다. 첫 표본을 캐시 없는 실행이라고 표현하지 않습니다.', '', '입력 작성·후처리·잘못된 호출·조회·수정·최종 파일 검증이 모두 측정 범위입니다. 직접 처리에는 표준 라이브러리·필터·자연스러운 배치를 허용했습니다. 최소 호출·작은 페이지 상한·필수 미리보기는 강제하지 않았습니다.', '', '셸 실행은 command_execution 이벤트 수입니다. 모델 도구 호출 수와 같지 않습니다. CLI 수는 실제 CLI 시작/완료의 별도 로그이고 API 수는 로컬 서버 접근 로그입니다. CLI 내부 파일 읽기 횟수를 이 숫자로 대신하지 않습니다. 시간에는 모델 생성·대기와 작업 수행이 함께 포함됩니다.', '', 'Linux의 실제 CLI/Git/gh와 로컬 GraphQL/REST에서 수행했습니다. API overlay는 주소 변경과 실행 기록만 추가하며 FS는 production CLI로 바로 전달합니다. 실제 GitHub 지연·요금·다른 OS의 실행 성능을 대표하지 않습니다.', '', '최초 진단 실험에서는 PR view라는 없는 하위 명령 호출·불필요한 inspect·저장 경로 충돌을 포함했습니다. 작업 폴더는 새로 만들었지만 상위 임시 경로의 파일은 공유됐습니다. 이후 head_verified 근거·저장 경로 사전 검사·셸 실행 파일/필드 안내와 작업 폴더 안의 저장 규칙을 보완한 리뷰 실험을 별도로 고정했습니다. 원래 24회는 진단 자료로 모두 보존하며 좋은 실행만 교체하지 않았습니다.', '']
    for name, report in reports.items():
        lines += [f'## {index['cohorts'][name]['label']}', '', f'측정일: {measured_date(report)}, 실행 {len(report['trials'])}회. [원본](data/{index['cohorts'][name]['file']}) · [소스](data/{name}-source.tar.gz) · [이벤트·상태](data/{name}-events.tar.gz)', '']
        if name == index['latest'].get('pr-conversation'):
            s = summary_for(report, 'pr-conversation')
            metric = s['direct']['metrics']['seconds']
            lines += [f'일반 대화의 직접 처리 시간 범위는 {metric['min']:.3f}–{metric['max']:.3f}초입니다. 큰 편차도 평균에 포함하며 평균 시간 차이 전체를 CLI 개선 효과로 해석하지 않습니다. 직접 GraphQL 배치보다 CLI의 API 수가 많으면 그 상충도 유지합니다.', '']
        if name == 'workflows-v6':
            s = summary_for(report, 'fs-file')
            lines += [f'연결 수정의 첫 비교도 보존합니다. 보존할 timeout의 실제 값이 결과에 없어 재조회한 실행과 도움말·원문 재조회가 추가된 실행을 포함하며 총 토큰은 {change(s, "input_plus_output")}, 캐시 제외는 {change(s, "uncached_plus_output")}입니다. 후속 receipt-v7는 실제 보존 값 확인·반환을 추가한 별도 6회이며 좋은 실행으로 교체하지 않았습니다.', '', '이 코호트는 고정한 정상 작업 소스의 측정입니다. 이후 일부 GraphQL 필드 오류에서도 반환된 스레드 원문을 유지하도록 오류 경로를 보완했고 별도 회귀 테스트로 검증했습니다. 최종 오류 경로의 작업 전체 토큰은 측정하지 않았습니다.', '']
        if 'helper_sha256' in report['protocol']:
            lines += ['연결 수정 예제는 설치된 상태이며 최초 설치·발견 비용은 제외했습니다. 내부 inspect와 apply의 실제 CLI 호출·검증 비용은 포함합니다.', '']
    lines += ['## 재현', '', '압축의 source·harness·API overlay와 protocol의 SHA를 확인합니다. harness 파일을 압축의 source/scripts/benchmarks에 복사하고 그 고정 실행기에서 새 root를 생성합니다. diagnostic-v3는 --tasks 옵션 없이 원래 네 작업, reviews-v4는 --tasks pr-reviews pr-conversation, fsfile-v5와 receipt-v7는 --tasks fs-file, workflows-v6는 --tasks pr-reviews pr-conversation markdown fs-file을 사용합니다. helper_sha256가 있는 코호트의 예제는 source/examples/fs에서 실행 파일을 설치하며 protocol의 SHA를 확인합니다. --run-models를 명시할 때만 모델이 실행됩니다. 기본 설정을 상속하므로 응답·캐시·토큰의 동일성을 보장하지 않습니다.', '', '```sh', 'python3 -m pip install --require-hashes -r scripts/benchmarks/requirements.txt', 'python3 scripts/benchmarks/run_usage_benchmark.py --root /tmp/new-usage-study --tasks pr-reviews pr-conversation markdown fs-file', '# --resume --run-models를 추가하면 지정한 작업만 방식별 3회 비교', 'python3 scripts/benchmarks/publish_usage_benchmark.py --render-only', '```', '']
    (directory / 'README.md').write_text('\n'.join(lines))
    targets = {value[1] for value in CASES.values()}
    for target in targets:
        cases = [c for c in latest if CASES[c][1] == target]
        if not cases:
            continue
        prefix = '../' * (len(Path(target).parts) - 1)
        page = [f'# `tools {target.removesuffix('.md').replace('/', ' ')}` 벤치마크', '', f'[공통 조건·전체 기록·재현]({prefix}usage/README.md). 가장 최근의 해당 작업만 표시하며 이전의 다른 요구 범위와 비교하지 않습니다.', '']
        for case in cases:
            name, report = latest[case]
            s = summary_for(report, case)
            a, b = (s['direct'], s['tools'])
            config = report['protocol']['configured_defaults']
            page += ['## ' + CASES[case][0], '', CASES[case][2], '', f'측정일: {measured_date(report)}(한국시간). 방식별 {a['n']}회, 기본 `{config['model']}` / `{config['model_reasoning_effort']}`.', ''] + table(s) + ['', f'정답·상태: 직접 {a['correct']}/{a['n']}, tools {b['correct']}/{b['n']}. **{assessment(s)}**.', '', f'[해당 원본]({prefix}usage/data/{index['cohorts'][name]['file']})', '']
            if case == 'junit':
                error = summary_for(report, case, True)
                page += ['### 잘못된 XML 처리', '', '누락을 0개 성공으로 만들지 않고 유효한 보고서와 오류 경로를 반환한 한 쌍입니다. 일반 판독 평균에 합산하지 않습니다.', ''] + table(error) + ['']
            if case == 'fs-file' and 'helper_sha256' in report['protocol']:
                page += ['연결 실행 예제를 설치해 둔 환경의 비교입니다. Python 예제는 실제 inspect·apply를 실행하며 두 CLI 호출 모두 집계합니다. 예제의 최초 설치·발견 비용은 측정하지 않았습니다. 직접 처리에는 한 스크립트도 허용하며 이 결과를 일반적인 파일 편집 전체로 확대하지 않습니다.', '']
        page += ['원문·필요한 검증을 제거하거나 반복 사용·모델 캐시 적중을 절감의 전제로 삼지 않았습니다. 새 세션은 입력 캐시 없는 실행의 증명이 아닙니다. 평균·범위와 전체 불리한 기록을 함께 확인합니다.', '', f'[사용법]({prefix}../{target})', '']
        (repo / 'docs/benchmarks' / target).write_text('\n'.join(page))
    patch_summaries(repo, latest)
    refresh_settings(repo, latest)

def patch_summaries(repo, latest):
    for relative, korean in [('README.md', False), ('docs/README.ko.md', True), ('docs/benchmarks/fs/README.md', True), ('docs/benchmarks/github/README.md', True)]:
        path = repo / relative
        lines = path.read_text().splitlines()
        output = []
        inserted = False
        fs_replaced = False
        for line in lines:
            if '[`github pr reviews`]' in line and 'pr-reviews' in latest:
                name, report = latest['pr-reviews']
                s = summary_for(report, 'pr-reviews')
                a, b = (s['direct'], s['tools'])
                base = 'docs/benchmarks/' if relative == 'README.md' else 'benchmarks/' if relative == 'docs/README.ko.md' else ''
                target = 'github/pr/reviews.md' if 'README.md' == relative or relative == 'docs/README.ko.md' else 'pr/reviews.md'
                line = f'| [`github pr reviews`]({base}{target}) | {measured_date(report)} | {a['metrics']['uncached_plus_output']['mean']:,.0f} | {b['metrics']['uncached_plus_output']['mean']:,.0f} | {change(s, 'uncached_plus_output')} | {change(s, 'input_plus_output')} |'
            if ('[`fs inspect`]' in line or '[`fs inspect:' in line) and 'markdown' in latest:
                if not fs_replaced:
                    base = 'docs/benchmarks/fs/' if relative == 'README.md' else 'benchmarks/fs/' if relative == 'docs/README.ko.md' else ''
                    for case, label in [('markdown', 'fs inspect: Markdown'), ('fs-file', 'fs inspect: raw file')]:
                        if case not in latest:
                            continue
                        name, report = latest[case]
                        s = summary_for(report, case)
                        a, b = s['direct'], s['tools']
                        output.append(f'| [`{label}`]({base}inspect.md) | {a['metrics']['uncached_plus_output']['mean']:,.0f} | {b['metrics']['uncached_plus_output']['mean']:,.0f} | {change(s, 'uncached_plus_output')} | {change(s, 'input_plus_output')} | {a['correct'] + b['correct']}/{a['n'] + b['n']} | {assessment(s, korean)} |')
                    fs_replaced = True
                continue
            if '[`fs test-results`]' in line:
                continue
            output.append(line)
            if '[`fs apply:' in line and (not inserted) and ('junit' in latest):
                inserted = True
        if inserted:
            name, report = latest['junit']
            s = summary_for(report, 'junit')
            a, b = (s['direct'], s['tools'])
            base = 'docs/benchmarks/fs/' if relative == 'README.md' else 'benchmarks/fs/' if relative == 'docs/README.ko.md' else ''
            row = f'| [`fs test-results`]({base}test-results.md) | {a['metrics']['uncached_plus_output']['mean']:,.0f} | {b['metrics']['uncached_plus_output']['mean']:,.0f} | {change(s, 'uncached_plus_output')} | {change(s, 'input_plus_output')} | {a['correct'] + b['correct']}/{a['n'] + b['n']} | {assessment(s, korean)} |'
            at = max((i for i, l in enumerate(output) if '[`fs apply:' in l)) + 1
            output.insert(at, row)
        prefix = 'docs/benchmarks/' if relative == 'README.md' else 'benchmarks/' if relative == 'docs/README.ko.md' else '../'
        note = '사용 기록에서 찾은 최신 작업의 토큰·시간·실제 CLI/API 수와 조건부 결과는 [별도 완료 비교](' + prefix + 'usage/README.md)에 있습니다. JUnit 표는 일반 판독 2쌍이며 XML 오류 1쌍은 별도 공개합니다.' if korean else 'Session-derived workflows report total/uncached tokens, time and actual CLI/API counts in the [completion study](' + prefix + 'usage/README.md). JUnit overview uses two normal pairs; the malformed-XML pair is separate.'
        clean = remove_notes(output, ['사용 기록에서 찾은 최신 작업의', 'Session-derived workflows report'])
        if relative in ('README.md', 'docs/README.ko.md'):
            at = next((i for i, l in enumerate(clean) if l == ('## 설치' if korean else '## Install')))
            clean[at:at] = [note, '']
        else:
            clean += ['', note]
        text = '\n'.join(clean) + '\n'
        text = text.replace('Measured on 2026-10-10 with inherited `gpt-6.1-sol` / `low` defaults: three trials per method for each command. Means use uncached input + output; total changes include cached input.', 'Latest per-command work scopes use inherited `gpt-6.1-sol` / `low` defaults on 2026-10-10–11. Most rows use three pairs; JUnit normal reading uses two, with its error pair reported separately. Means use uncached input + output; total changes include cached input.')
        text = text.replace('2026-10-10 기준 `gpt-6.1-sol` / `low`, 명령별 방식마다 3회. 캐시 제외 입력+출력 평균과 캐시 포함 총 토큰 변화를 함께 비교했습니다.', '2026-10-10·11의 명령별 최신 작업을 기본 `gpt-6.1-sol` / `low`로 비교했습니다. 보통 방식별 3회이며 JUnit 일반 판독은 2쌍, XML 오류 1쌍은 별도입니다. 캐시 제외 입력+출력 평균과 캐시 포함 총 토큰 변화를 함께 표시합니다.')
        path.write_text(text)
    path = repo / 'docs/benchmarks/README.md'
    text = path.read_text()
    row = '| [사용 기록 기반 완료 비교](usage/README.md) | 리뷰 재사용·일반 대화, JUnit 판독·Markdown 조회·raw 파일 반영, 원래 불리한 기록 포함 |'
    lines = [l for l in text.splitlines() if not l.startswith('| [사용 기록 기반 완료 비교]')]
    at = next((i for i, l in enumerate(lines) if l.startswith('| [변경 동작 완료 비교]'))) + 1
    lines.insert(at, row)
    path.write_text('\n'.join(lines) + '\n')


def refresh_settings(repo, latest):
    """원본 설정으로 최신 표의 모델·추론 강도 설명을 갱신한다."""
    from github_benchmark_docs import published_reports
    records = list(published_reports(repo).values())
    fs_records = []
    for relative in ('docs/benchmarks/actionable/data/study.json', 'docs/benchmarks/fs/data/study-completion-v3.json'):
        path = repo / relative
        if path.exists():
            fs_records.append(json.loads(path.read_text()))
    fs_records += list({name: report for case, (name, report) in latest.items() if case in ('markdown', 'fs-file', 'junit')}.values())
    records += fs_records + list({name: report for name, report in latest.values()}.values())

    def settings(reports):
        entries = sorted({(measured_date(r), r['protocol']['configured_defaults']['model'], r['protocol']['configured_defaults']['model_reasoning_effort']) for r in reports})
        return '; '.join(f'{date}: `{model}` / `{effort}`' for date, model, effort in entries)

    global_settings, fs_settings = settings(records), settings(fs_records)
    for relative in ('README.md', 'docs/README.ko.md', 'docs/benchmarks/fs/README.md'):
        path = repo / relative
        lines = []
        for line in path.read_text().splitlines():
            if line.startswith('Inherited defaults:'):
                line = 'Inherited defaults: ' + global_settings + '. Batches with different settings are not pooled.'
            elif relative == 'docs/README.ko.md' and line.startswith('측정 당시 기본 설정:'):
                line = '측정 당시 기본 설정: ' + global_settings + '. 서로 다른 설정의 실험을 합산하지 않습니다.'
            elif line.startswith(('Latest per-command work scopes use inherited', 'Filesystem defaults by cohort:')):
                line = 'Filesystem defaults by cohort: ' + fs_settings + '. Most rows use three pairs; JUnit normal reading uses two. Means use uncached input + output; total changes include cached input.'
            elif line.startswith(('2026-10-10·11의 명령별 최신 작업을 기본', '파일시스템 실험별 기본 설정:')):
                line = '파일시스템 실험별 기본 설정: ' + fs_settings + '. 보통 방식별 3회이며 JUnit 일반 판독은 2쌍입니다. 캐시 제외 평균과 캐시 포함 총 토큰 변화를 함께 표시합니다.'
            elif relative == 'docs/benchmarks/fs/README.md' and line.startswith(('사용자 기본 설정 `', '측정 당시 실험별 기본 설정:')):
                line = '측정 당시 실험별 기본 설정: ' + fs_settings + '. 모델·강도를 지정하지 않았으며 원본 설정 스냅샷과 실행 인자로 기록했습니다. 명령별 최신 원본을 확인합니다.'
            lines.append(line)
        path.write_text('\n'.join(lines) + '\n')
    path = repo / 'docs/benchmarks/github/README.md'
    date = measured_date(latest['pr-reviews'][1])
    lines = [f'| [`pr reviews`](pr/reviews.md) | {date}: 작업 전체, 방식별 3회 |' if line.startswith('| [`pr reviews`]') else line for line in path.read_text().splitlines()]
    path.write_text('\n'.join(lines) + '\n')
