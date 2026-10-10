# 필요한 정보를 유지하는 완료 작업 비교

2026-10-10 기준 네 변경 시나리오를 직접 처리와 각 3회 비교했습니다. 기본 모델·추론 강도를 상속했고 코드·실행기·프롬프트·정답·순서를 모델 실행 전에 고정했습니다.

| 시나리오 | 총 토큰 변화 | 캐시 제외 변화 | 시간 변화 | 셸 실행 직접→tools | 정답·상태 | 판단 |
|---|---:|---:|---:|---:|---:|---|
| [fs apply: 일괄 오류 진단](../fs/apply.md) | −50.7% | −8.2% | −46.3% | 3.00→1.00 | 6/6 | 표본에서 AND 충족 |
| [fs apply: 공통 치환](../fs/apply.md) | −1.6% | −6.9% | −61.2% | 2.00→2.00 | 6/6 | 표본에서 AND 충족 |
| [github pr reviews: 리뷰 반영](../github/pr/reviews.md) | −1.2% | −24.2% | +9.6% | 5.00→2.00 | 6/6 | 부분 개선·AND 미충족 |
| [github pr inspect: 복귀 후 반영](../github/pr/inspect.md) | −14.3% | −8.8% | −11.7% | 4.67→2.33 | 6/6 | 표본에서 AND 충족 |

기준은 같은 요청·정답·파일 상태·검증을 정확히 끝내면서 작업 효율 향상 AND 전체 입력+출력 절감을 함께 충족하는 것입니다. 시간은 줄고 셸 실행 항목 수는 늘지 않아야 하며, 캐시 제외 토큰이 증가하면 조건부로 표시합니다. 어느 방식이든 3회 모두 정답·상태 검증에 통과하지 않으면 변화율 비교를 보류합니다.

각 실행은 새 세션·임시 작업 폴더로 시작하며 직접 처리도 자연스러운 배치·필터·스크립트 작성을 허용했습니다. 그룹 치환은 자연어 지시부터 입력 작성·전체 검증·저장·반영·실제 파일/계획 재검증을 포함합니다. 오류 진단은 실제 수정 없이 전체 오류 보고와 파일 보존을 완료합니다. 리뷰는 자료 조회 뒤 실제 파일 수정·검증까지 포함합니다.

실행 인자에는 모델·추론 강도·ignore-user-config를 지정하지 않았습니다. 모델 이름·강도는 사용자 설정 스냅샷이며 실행 이벤트가 실제 선택 값을 별도로 제공하지 않는 한계가 있습니다.

PR inspect는 변화 없음·새 답글·해결 완료를 각각 한 쌍씩 사용했습니다. 이전 상태 기준은 작업 입력으로 제공했으므로 그 생성 비용은 복귀 측정에서 제외했습니다. 실제 다른 모델 작업을 수행한 뒤 복귀하거나 최초 기준 생성의 절감을 검증한 것은 아닙니다. 새 세션은 모델 입력 캐시 없는 실행을 뜻하지 않으며 캐시 적중을 강제로 통제하지 못했습니다.

셸 실행 항목 수는 events의 command_execution 수이며 모델 도구 호출 수와 동일하지 않습니다. CLI 내부의 API 요청·파일 읽기·폴링 횟수와도 구분합니다. 원본에는 실행 인자, 명령, 실패·재시도, API 접근, 캐시 포함/제외 토큰, 평균·범위·편차와 각 쌍의 변화율을 보관합니다. 실패한 모델 실행을 교체하거나 불리한 쌍을 제외하지 않습니다.

최초 검증은 23/24였습니다. 입력 작성이 허용된 공통 치환에서 입력 파일 이름을 spec.json만 허용한 검증기 오류 1건을 발견했습니다. batch-spec.json의 기록된 SHA와 실행 명령으로 정확한 입력을 복원해 같은 파일·계획 결과를 재검증했고 조건 보정 후 24/24입니다. 원래 실패·summary와 재검증 결과를 함께 공개합니다. 모델 재실행·실패 제외·토큰/시간 교체는 0회이며 표는 같은 24회 전체의 수치입니다.

실제 CLI·Git·gh와 Linux 파일시스템을 사용하고 GitHub API만 로컬 GraphQL/REST 서버로 연결했습니다. 작은 고정 표본의 결과이며 실제 GitHub 성능·금액 절감·macOS/Windows 런타임·모든 옵션·작업을 대표하지 않습니다. 공유 compact 변경의 CI 긴 로그 등 다른 작업은 이번 모델 측정에 포함되지 않았습니다. 원문 보존으로 한 응답이 커지는 경우도 포함해 작업 완료 비용을 비교했습니다.

## 원본과 재현

- [작업 전체 결과·프로토콜](data/study.json) · [사전 기능 검증](data/native.json)
- [고정 소스·실행기](data/study-source.tar.gz) · [모든 이벤트·답변·상태 검증](data/study-events.tar.gz)

압축 파일 SHA-256은 원본 JSON의 evidence.sha256에 있습니다. source_manifest는 파일별 SHA이며 저장소 현재 코드의 결과와 과거 고정 코드 결과를 혼합하지 않습니다.

```sh
python3 -m pip install --require-hashes -r scripts/benchmarks/requirements.txt
# 모델 없는 사전 검증
python3 scripts/benchmarks/run_actionable_benchmark.py --root /tmp/actionable-study
# 고정 실행기의 같은 기록에서 24회 수행
python3 /tmp/actionable-study/harness/run_actionable_benchmark.py --root /tmp/actionable-study --resume --run-models
python3 scripts/benchmarks/publish_actionable_benchmark.py --root /tmp/actionable-study
# 모델 호출 없이 문서만 재생성
python3 scripts/benchmarks/publish_actionable_benchmark.py --render-only
```

과거 소스 재현은 source 압축의 source와 harness를 풀고 harness를 source/scripts/benchmarks로 복사한 뒤 그 고정 실행기에서 새 root를 생성합니다. 개선 전 Go 소스·API 주입 overlay도 before/source에 보관합니다. 기본 설정이 같아도 모델 응답·캐시·토큰 수의 동일성을 보장하지 않습니다. 개선 전 CLI 비교는 사전 기능 검증이며 작업 전체 모델 토큰 실측이 아닙니다.
