# 변경 작업 완료 비교

A1–A6의 변경된 작업 7개만 직접 처리와 tools로 각 3회 비교한다. 원문 복구 뒤 최초 선택 응답으로 개선한 세 작업을 별도 재측정했다. 현재 표는 작업당 최신 6건이며 총 60개 실제 모델 세션의 입력·출력 usage, 정답·최종 파일/권한·호출·시간을 보존한다. 실패와 불리한 실행도 제외하거나 교체하지 않는다. OS 런타임 검증은 Linux이며 다른 OS는 빌드와 구분한다.

기본 설정: `{'model': 'gpt-6.1-sol', 'model_reasoning_effort': 'high', 'profile': None}`. 옵션으로 덮어쓰지 않았으며 provider 입력 캐시는 통제하지 않았다. 토큰은 캐시 포함 입력+출력이 주지표이고 캐시 제외 입력+출력도 함께 판정한다. 둘 중 유리한 수치만 고르지 않는다.

| 작업 | 총 토큰 변화 | 캐시 제외 변화 | 시간 변화 | 정답·상태 | 판단 |
|---|---|---|---|---|---|
| [리뷰 경로 연결·긴 원문 반영](github/pr/reviews.md) | +8.2% | +8.8% | +32.3% | 6/6 | 부분 개선 또는 AND 미충족 |
| [다른 작업 후 제출 리뷰 복귀](github/pr/inspect.md) | -3.5% | -5.8% | -42.3% | 6/6 | 조건부: 토큰·시간 감소, 호출 부담 증가 |
| [기본 상한을 넘은 JUnit 진단 복구](fs/test-results.md) | -37.3% | -13.2% | -72.2% | 6/6 | 소표본 평균의 AND 충족; 안정적 실사용 효과는 미확인 |
| [삭제 원문 복구·기준 보존](fs/delta.md) | -35.6% | -30.0% | -55.0% | 6/6 | 소표본 평균의 AND 충족; 안정적 실사용 효과는 미확인 |
| [큰 변경 보고서·최종 상태 확인](fs/apply.md) | -57.0% | -85.8% | -63.4% | 6/6 | 소표본 평균의 AND 충족; 안정적 실사용 효과는 미확인 |
| [설정 저장·실제 SHA·권한 검증](github/setup.md) | -42.9% | -16.5% | -65.1% | 6/6 | 조건부: 토큰·시간 감소, 호출 부담 증가 |
| [저장 CI 원문 선택·완전성 확인](github/ci/failures.md) | -47.3% | -35.5% | -66.1% | 6/6 | 소표본 평균의 AND 충족; 안정적 실사용 효과는 미확인 |

AND의 소표본 판정은 두 토큰 지표·시간 감소와 셸/API 호출 비증가를 함께 확인한다. 두 토큰 지표가 상충하거나 호출 부담이 증가하면 조건부로 기록한다. 새로운 보관 I/O도 있으므로 수치가 좋아졌다고 모든 비용이 감소하거나 실사용에서 안정적이라고 보장하지 않는다.

최신 fs 비교는 --diagnostic-pattern/--report-pattern으로 실제 요청 구간과 검증 영수증을 첫 응답에서 받는다. apply의 mode_preserved도 실제 read-back 근거다. 저장 뒤 복구하는 이전 18건도 첫 코호트에 보존했다. 일반 크기의 한 번 조회와 기본 상한을 넘은 보고서 복구를 구분한다. 리뷰 복귀와 delta에는 이전에 만들어진 관찰을 작업 입력으로 공급했다. 기존 관찰 생성 비용은 복귀 작업 밖이며 동일 명령 연속 실행이나 이전 모델 문맥을 가정하지 않는다. CI 원문도 공급된 이전 자료이며 수집부터 수행한 CI 실패/재실행/제출/머지 전체의 새 측정이 아니다.

직접 처리의 자연스러운 배치·필터·Python 저장/검증을 허용했다. tools의 큰 보고서는 저장과 필요한 부분 읽기까지 포함한다. 모델의 셸 실행, 내부 tools CLI, API 호출을 나눴다. 실제 syscall/I/O 횟수는 계측하지 않았으며 저장·해시 확인 비용은 시간과 호출에 포함한다.

공유 실행기의 이전 usage 코호트용 helper·malformed JUnit 제한 문구가 원본 protocol에 남아 있지만 이번 코호트에는 integer helper나 malformed JUnit 작업이 없다. 이번 입력은 재현한 결함의 합성 fixture이며 모든 개인 세션을 재생한 시험은 아니다. 로컬 API 지연은 실제 GitHub 네트워크 지연과 다르다.

제출 리뷰 복귀 6건의 원채점 기대값에 오류가 있었다. 과거 CHANGES_REQUESTED를 현재 종합 상태로 하드코딩했으나 고정 fixture와 native 응답은 APPROVED이다. 원본의 false 채점·usage·이벤트를 모두 유지하며 adjudications에 독립 native 근거와 정정 채점을 별도로 기록했다. 위 표는 정정 채점이다. 재실행·대체·제외는 하지 않았다.

첫 코호트 뒤 자동 저장의 os.Root 경계와 삭제된 부모 경로 검증을 보강했다. 최초 선택 응답 코호트는 보강된 소스를 고정해 측정했고 일반 복구·오류 경로는 Go 시험으로 검증했다. 각 표의 토큰은 해당 코호트의 보관된 소스/입력에 대한 결과다.

소스·실행기는 코호트별로 먼저 고정하고 사전 검증했다 (원문 복구 21건, 최초 선택 응답 9건). 정답·파일 상태 검증은 모델 밖의 실행기가 담당한다. 출력 사용성 [관찰 재현](../command-usability-audit.json)은 토큰 측정과 별도이다.

재현:

```sh
python3 scripts/benchmarks/run_completion_benchmark.py --root /tmp/fresh-completion
python3 /tmp/fresh-completion/harness/run_completion_benchmark.py --root /tmp/fresh-completion --resume --run-models
python3 scripts/benchmarks/publish_completion_benchmark.py --root /tmp/fresh-completion --name NEW_NAME
```

기존 원본은 불변 코호트로 보존한다. 최신 문서는 각 명령의 최신 코호트만 표시한다. [개선안·적용 범위](../../command-improvements.md).
