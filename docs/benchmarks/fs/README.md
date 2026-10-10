# 파일시스템 벤치마크

이번 변경 동작의 최신 완료 작업 비교는 [공통 조건·원본](../actionable/README.md)에 있습니다. 원문 조회·입력 작성·실제 반영/파일 보존·재검증까지 포함하며 기존의 다른 시나리오와 수치를 합산하지 않습니다.

명령별 최신 결과입니다. apply의 최신 두 시나리오는 아래에 구분해 표시합니다. 보관된 과거 FS 실험 9개·108회와 그 실패 6회는 원본에서 유지합니다.

| 명령 | 직접 평균 | tools 평균 | 캐시 제외 변화 | 캐시 포함 변화 | 정답·상태 | 동시 개선 판단 |
|---|---:|---:|---:|---:|---:|---|
| [`fs inspect: Markdown`](inspect.md) | 21,294 | 16,324 | −23.3% | −42.0% | 6/6 | 표본에서 AND 충족 |
| [`fs inspect: raw file`](inspect.md) | 16,459 | 15,601 | −5.2% | −2.3% | 6/6 | 표본에서 AND 충족 |
| [`fs delta`](delta.md) | 18,026 | 16,674 | −7.5% | −12.9% | 6/6 | 표본에서 AND 충족 |
| [`fs apply: 오류 진단`](apply.md) | 17,178 | 15,768 | −8.2% | −50.7% | 6/6 | 표본에서 AND 충족 |
| [`fs apply: 공통 치환`](apply.md) | 18,583 | 17,300 | −6.9% | −1.6% | 6/6 | 표본에서 AND 충족 |
| [`fs test-results`](test-results.md) | 21,844 | 25,794 | +18.1% | −11.5% | 4/4 | 조건부·캐시 지표 상충 |

## 공통 조건과 한계

측정 당시 실험별 기본 설정: 2026-10-10: `gpt-6.1-sol` / `low`; 2026-10-11: `gpt-6.1-sol` / `high`. 모델·강도를 지정하지 않았으며 원본 설정 스냅샷과 실행 인자로 기록했습니다. 명령별 최신 원본을 확인합니다.

실제 프로덕션 CLI를 고정 소스에서 빌드하고 프롬프트·스키마·seed 20261010·각 실험의 순서·파일 작업을 모델 호출 전에 고정했습니다. 각 명령의 방식 순서를 교차하고 매번 새 세션·새 합성 작업 폴더를 사용했습니다. 직접 처리에도 rg·필터·Python·배치를 허용했고 명령 수를 강제하지 않았습니다. 직접 스크립트를 작성하는 비용은 포함하고 CLI 설치·빌드 비용은 제외했습니다.

캐시 제외 입력+출력은 input_tokens − cached_input_tokens + output_tokens, 캐시 포함 총 토큰은 input_tokens + output_tokens입니다. 추론 출력을 중복 합산하지 않습니다. 동시 개선은 총 토큰·완료 시간이 줄고 모델의 셸 호출이 늘지 않으며 캐시 제외 지표도 증가하지 않는 표본에서만 충족으로 표시합니다. 두 토큰 지표가 상충하면 조건부, 한쪽만 좋아지면 부분 개선으로 표시합니다. 캐시는 강제로 초기화하지 못했고 소표본 판정은 일반적 절감률을 뜻하지 않습니다. 둘 중 어느 방식이든 3회 모두 정답·상태를 만족하지 않으면 비교를 보류합니다. 실패·재시도도 비용에 포함하고 실패한 모델 실행을 교체하지 않았습니다.

실사용의 기본 평가 조건은 명령의 독립 1회 사용입니다. 다른 작업 후 재사용과 연속 반복은 별도 조건으로 구분합니다. 현재 자료는 고정된 실험 순서에서 새 세션으로 수행한 반복 표본이며, 입력 캐시를 통제하지 않았고 두 사용 조건을 별도로 검증하지 않았습니다. 첫 번째 표본에도 캐시가 있으므로 캐시 없는 최초 사용 결과가 아닙니다. 표의 AND 충족은 관측한 평균의 판정이며 독립 1회 사용·비연속 재사용에서도 성립한다고 주장하지 않습니다.

합성 자료의 대표 시나리오와 방식별 3회의 소표본입니다. 모든 파일 크기·옵션·오류 경로·실운영 평균·금액 절감을 대표하지 않습니다. 결과는 후처리 작성·추가 조회·실패 복구·반영·재검증을 포함한 에이전트 작업 전체 토큰이며 CLI 자체는 모델을 호출하지 않습니다. 모델 호출 전 샌드박스에서 실제 출력·파일 바이트·권한을 검증했습니다. 파일 읽기·수정만 사용하고 실제 GitHub 리소스는 변경하지 않았습니다.

토큰 측정은 Linux의 Codex sandbox와 /tmp에서 수행했습니다. CLI는 Go 표준 파일 API를 사용하며 Linux·macOS·Windows 빌드와 세 OS의 CI 테스트 구성을 제공합니다. 이번 Windows·macOS 실행 테스트를 완료했다는 뜻은 아닙니다.

최초 실험의 직접 delta 3회는 없는 python 명령을 사용한 뒤 변경 공개 단계에 실패해 비교에서 절감률을 보류했습니다. 오류·토큰을 원본에 유지했습니다. 추가 실험은 양쪽에 python3 실행 환경을 명시했고 같은 CLI·기본 설정·파일 작업으로 두 명령을 다시 비교했습니다. 도구 방식은 내부 검증이 있는 --apply 한 번과 필요한 줄 조회를 사용합니다.

두 번째 실험의 apply 안내에 delta 전용 변경 공개 문장이 섞여 불필요한 조회·실패 호출이 발생했습니다. 해당 비용과 정답 기록을 보존하되 안내와 쓰기 범위를 바로잡은 별도 6회 실험도 보존했습니다. 처음 세 실험의 CLI 구현은 동일합니다. 최신 개선 실험은 새 CLI와 확장된 작업으로 별도 비교했습니다.

전체 편집 실험의 최초 apply 안내는 저장 계획 형식과 치환 번호의 기준을 명시하지 않았지만 검사는 특정 version 1 형식·1부터 시작하는 번호를 요구했습니다. 결함 발견 시 중단한다는 계획과 달리 고정 순서를 끝까지 실행했으며 해당 편차를 원본 evidence.measurement_issues에 기록했습니다. 관련 실패·비용은 그대로 보존하고 변화율을 보류했습니다. 후속 실험에서는 양쪽에 같은 형식·번호 기준을 명시했습니다. inspect 안내도 실제 치환 스키마를 제시했습니다. 안내·CLI가 함께 바뀌어 각각의 효과를 분리하지 못합니다.

## 원본과 재현

- study: [전체 원본](data/study.json) · [고정 소스·실행기](data/study-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-events.tar.gz)
- study-completion: [전체 원본](data/study-completion.json) · [고정 소스·실행기](data/study-completion-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-completion-events.tar.gz)
- study-completion-v2: [전체 원본](data/study-completion-v2.json) · [고정 소스·실행기](data/study-completion-v2-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-completion-v2-events.tar.gz)
- study-completion-v3: [전체 원본](data/study-completion-v3.json) · [고정 소스·실행기](data/study-completion-v3-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-completion-v3-events.tar.gz)
- study-completion-v4: [전체 원본](data/study-completion-v4.json) · [고정 소스·실행기](data/study-completion-v4-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-completion-v4-events.tar.gz)
- study-efficient: [전체 원본](data/study-efficient.json) · [고정 소스·실행기](data/study-efficient-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-efficient-events.tar.gz)
- study-efficient-v2: [전체 원본](data/study-efficient-v2.json) · [고정 소스·실행기](data/study-efficient-v2-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-efficient-v2-events.tar.gz)
- study-workflows: [전체 원본](data/study-workflows.json) · [고정 소스·실행기](data/study-workflows-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-workflows-events.tar.gz)
- study-workflows-compact: [전체 원본](data/study-workflows-compact.json) · [고정 소스·실행기](data/study-workflows-compact-source.tar.gz) · [이벤트·답변·파일 상태·사전 검증](data/study-workflows-compact-events.tar.gz)

원본 JSON의 evidence.sha256과 protocol의 소스·실행기·CLI 지문으로 감사할 수 있습니다.

```sh
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-study
# 사전 검증 후, 같은 고정 자료로 18회 모델 실행
python3 /tmp/fs-study/harness/run_fs_benchmark.py --root /tmp/fs-study --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-study
# 안내·호출 방식 개선 실험은 두 명령만 별도 고정
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-efficient --tasks delta apply --efficient-usage
python3 /tmp/fs-efficient/harness/run_fs_benchmark.py --root /tmp/fs-efficient --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-efficient
# apply 안내를 바로잡은 최신 조건만 6회 실행
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-apply --tasks apply --efficient-usage
python3 /tmp/fs-apply/harness/run_fs_benchmark.py --root /tmp/fs-apply --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-apply
# 최신 여섯 개선을 세 작업으로 묶어 18회 실행
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-workflows --workflows
python3 /tmp/fs-workflows/harness/run_fs_benchmark.py --root /tmp/fs-workflows --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-workflows
# delta 본문 선택을 보완한 같은 작업만 6회 실행
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-delta-compact --workflows --compact-content --tasks delta
python3 /tmp/fs-delta-compact/harness/run_fs_benchmark.py --root /tmp/fs-delta-compact --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-delta-compact
# 원문 조회→실제 편집, 실제 반복 관찰, 오류 복구→반영의 전체 작업
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-completion --completion-workflows
python3 /tmp/fs-completion/harness/run_fs_benchmark.py --root /tmp/fs-completion --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-completion
# 계획 형식 안내를 보완한 작업만 12회 별도 측정
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-completion-v2 --completion-workflows --tasks inspect apply
python3 /tmp/fs-completion-v2/harness/run_fs_benchmark.py --root /tmp/fs-completion-v2 --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-completion-v2
# 실제 저장 계획 증명과 연속 실행으로 delta·apply의 AND 조건 재검증
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-and --batch-workflows --tasks delta apply
python3 /tmp/fs-and/harness/run_fs_benchmark.py --root /tmp/fs-and --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-and
# 이미 지시된 특정 항목의 횟수 수정을 통합한 apply만 6회
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-recount --recount-workflows --tasks apply
python3 /tmp/fs-recount/harness/run_fs_benchmark.py --root /tmp/fs-recount --resume --run-models
python3 scripts/benchmarks/publish_fs_benchmark.py --root /tmp/fs-recount
```

현재 저장소 실행기는 안내 수정본입니다. 과거 조건 그대로의 재현에는 각 실험에 동봉한 고정 실행기를 사용합니다.

공개 source.tar.gz의 source에서 CLI를 빌드하고 동봉 실행기로 같은 파일 작업을 재현할 수 있습니다. 모델 답변과 토큰 수가 동일해지는 것을 보장하지 않습니다. [사용법](../../fs/README.md).


사용 기록에서 찾은 최신 작업의 토큰·시간·실제 CLI/API 수와 조건부 결과는 [별도 완료 비교](../usage/README.md)에 있습니다. JUnit 표는 일반 판독 2쌍이며 XML 오류 1쌍은 별도 공개합니다.
