# 사용 기록에서 찾은 작업의 완료 비교

실제 사용 기록의 필요를 합성 작업으로 재현했습니다. 공개된 72회 모두의 기록을 유지하며 정답·상태는 72/72입니다. 최신 행은 해당 작업의 가장 최근 고정 소스·안내·검증 조건만 사용합니다. 서로 다른 실험을 합산하거나 이전 수치와의 차이를 코드 효과로 해석하지 않습니다.

| 작업 | 방식별 n | 총 토큰 | 캐시 제외 | 시간 | 셸 직접→tools | CLI tools | API 직접→tools | 판단 |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| [Markdown 구조·섹션](../fs/inspect.md) | 3 | −42.0% | −23.3% | −50.2% | 2.00→1.00 | 1.00 | 0.00→0.00 | 표본에서 AND 충족 |
| [JUnit 결과 판독](../fs/test-results.md) | 2 | −11.5% | +18.1% | −53.9% | 3.00→1.00 | 1.00 | 0.00→0.00 | 조건부·캐시 지표 상충 |
| [리뷰 반영](../github/pr/reviews.md) | 3 | +0.6% | +31.6% | +6.4% | 3.33→2.00 | 1.00 | 2.00→3.00 | 부분 개선·AND 미충족 |
| [일반 대화 포함 조회](../github/pr/reviews.md) | 3 | −0.7% | −1.2% | −13.1% | 1.00→1.00 | 1.00 | 1.00→3.00 | 조건부·API 호출 증가 |
| [명시한 raw 파일 반영](../fs/inspect.md) | 3 | −2.3% | −5.2% | −47.9% | 1.00→1.00 | 2.00 | 0.00→0.00 | 표본에서 AND 충족 |

JUnit의 일반 판독 2쌍과 잘못된 XML 처리 1쌍은 구분합니다. 원래 실패한 테스트의 보고서를 읽는 것은 일반 판독이며 XML 구조 오류 처리는 별도 시나리오입니다. 그 오류 처리 실행도 전체 기록에서 제외하지 않습니다.

작업 효율은 시간과 셸 실행 항목 수를 함께 평가하며, 캐시 포함 총 입력+출력을 기준으로 토큰을 비교합니다. 캐시 제외 지표가 증가하면 조건부로 표시합니다. 표본 평균의 AND 조건 충족과 안정적인 실사용 효과는 구분합니다.

기본 모델·추론 강도를 상속했습니다. 실행 인자에 모델/강도/ignore-user-config를 넣지 않았으며 설정 스냅샷은 각 원본 protocol에 있습니다. 모델 입력 캐시는 초기화하거나 통제하지 못했습니다. 첫 표본을 캐시 없는 실행이라고 표현하지 않습니다.

입력 작성·후처리·잘못된 호출·조회·수정·최종 파일 검증이 모두 측정 범위입니다. 직접 처리에는 표준 라이브러리·필터·자연스러운 배치를 허용했습니다. 최소 호출·작은 페이지 상한·필수 미리보기는 강제하지 않았습니다.

셸 실행은 command_execution 이벤트 수입니다. 모델 도구 호출 수와 같지 않습니다. CLI 수는 실제 CLI 시작/완료의 별도 로그이고 API 수는 로컬 서버 접근 로그입니다. CLI 내부 파일 읽기 횟수를 이 숫자로 대신하지 않습니다. 시간에는 모델 생성·대기와 작업 수행이 함께 포함됩니다.

Linux의 실제 CLI/Git/gh와 로컬 GraphQL/REST에서 수행했습니다. API overlay는 주소 변경과 실행 기록만 추가하며 FS는 production CLI로 바로 전달합니다. 실제 GitHub 지연·요금·다른 OS의 실행 성능을 대표하지 않습니다.

최초 진단 실험에서는 PR view라는 없는 하위 명령 호출·불필요한 inspect·저장 경로 충돌을 포함했습니다. 작업 폴더는 새로 만들었지만 상위 임시 경로의 파일은 공유됐습니다. 이후 head_verified 근거·저장 경로 사전 검사·셸 실행 파일/필드 안내와 작업 폴더 안의 저장 규칙을 보완한 리뷰 실험을 별도로 고정했습니다. 원래 24회는 진단 자료로 모두 보존하며 좋은 실행만 교체하지 않았습니다.

## 최초 진단 실험: 모든 24회 보존

측정일: 2026-10-11, 실행 24회. [원본](data/diagnostic-v3.json) · [소스](data/diagnostic-v3-source.tar.gz) · [이벤트·상태](data/diagnostic-v3-events.tar.gz)

## 리뷰 검증 근거·사전 경로 검사 보완: 12회

측정일: 2026-10-11, 실행 12회. [원본](data/reviews-v4.json) · [소스](data/reviews-v4-source.tar.gz) · [이벤트·상태](data/reviews-v4-events.tar.gz)

## 명시한 raw 파일 조회·반영 보완: 6회

측정일: 2026-10-11, 실행 6회. [원본](data/fsfile-v5.json) · [소스](data/fsfile-v5-source.tar.gz) · [이벤트·상태](data/fsfile-v5-events.tar.gz)

## 리뷰 소스 묶음·API 배치·Markdown 공통 선택·연결 수정

측정일: 2026-10-11, 실행 24회. [원본](data/workflows-v6.json) · [소스](data/workflows-v6-source.tar.gz) · [이벤트·상태](data/workflows-v6-events.tar.gz)

일반 대화의 직접 처리 시간 범위는 10.806–11.602초입니다. 큰 편차도 평균에 포함하며 평균 시간 차이 전체를 CLI 개선 효과로 해석하지 않습니다. 직접 GraphQL 배치보다 CLI의 API 수가 많으면 그 상충도 유지합니다.

연결 수정의 첫 비교도 보존합니다. 보존할 timeout의 실제 값이 결과에 없어 재조회한 실행과 도움말·원문 재조회가 추가된 실행을 포함하며 총 토큰은 +56.4%, 캐시 제외는 +0.2%입니다. 후속 receipt-v7는 실제 보존 값 확인·반환을 추가한 별도 6회이며 좋은 실행으로 교체하지 않았습니다.

이 코호트는 고정한 정상 작업 소스의 측정입니다. 이후 일부 GraphQL 필드 오류에서도 반환된 스레드 원문을 유지하도록 오류 경로를 보완했고 별도 회귀 테스트로 검증했습니다. 최종 오류 경로의 작업 전체 토큰은 측정하지 않았습니다.

연결 수정 예제는 설치된 상태이며 최초 설치·발견 비용은 제외했습니다. 내부 inspect와 apply의 실제 CLI 호출·검증 비용은 포함합니다.

## 실제 보존 값·완료 검증을 반환하는 연결 수정

측정일: 2026-10-11, 실행 6회. [원본](data/receipt-v7.json) · [소스](data/receipt-v7-source.tar.gz) · [이벤트·상태](data/receipt-v7-events.tar.gz)

연결 수정 예제는 설치된 상태이며 최초 설치·발견 비용은 제외했습니다. 내부 inspect와 apply의 실제 CLI 호출·검증 비용은 포함합니다.

## 재현

압축의 source·harness·API overlay와 protocol의 SHA를 확인합니다. harness 파일을 압축의 source/scripts/benchmarks에 복사하고 그 고정 실행기에서 새 root를 생성합니다. diagnostic-v3는 --tasks 옵션 없이 원래 네 작업, reviews-v4는 --tasks pr-reviews pr-conversation, fsfile-v5와 receipt-v7는 --tasks fs-file, workflows-v6는 --tasks pr-reviews pr-conversation markdown fs-file을 사용합니다. helper_sha256가 있는 코호트의 예제는 source/examples/fs에서 실행 파일을 설치하며 protocol의 SHA를 확인합니다. --run-models를 명시할 때만 모델이 실행됩니다. 기본 설정을 상속하므로 응답·캐시·토큰의 동일성을 보장하지 않습니다.

```sh
python3 -m pip install --require-hashes -r scripts/benchmarks/requirements.txt
python3 scripts/benchmarks/run_usage_benchmark.py --root /tmp/new-usage-study --tasks pr-reviews pr-conversation markdown fs-file
# --resume --run-models를 추가하면 지정한 작업만 방식별 3회 비교
python3 scripts/benchmarks/publish_usage_benchmark.py --render-only
```
