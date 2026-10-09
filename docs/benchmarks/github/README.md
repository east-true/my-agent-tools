# GitHub 벤치마크

명령별 최신 결과입니다. 경로는 [사용법 문서](../../github/README.md)와 같습니다. 측정일은 한국시간입니다. 작업 전체 토큰과 출력 토큰은 별도 지표로 다룹니다.

| 명령 | 최신 검증 |
|---|---|
| [`context`](context.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`setup`](setup.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`issue create`](issue/create.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`issue branch`](issue/branch.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`pr create`](pr/create.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`pr reviews`](pr/reviews.md) | 2026-10-10: 작업 전체, 방식별 3회 |
| [`pr inspect`](pr/inspect.md) | 2026-10-10: 작업 전체, 방식별 3회 |
| [`pr delta`](pr/delta.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`pr submit`](pr/submit.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`pr merge`](pr/merge.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`ci failures`](ci/failures.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`ci rerun`](ci/rerun.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`dependabot list`](dependabot/list.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`dependabot view`](dependabot/view.md) | 2026-10-08: 작업 전체, 방식별 3회 |
| [`branch cleanup --apply`](branch/cleanup.md) | 2026-10-08: 작업 전체, 방식별 3회 |

## 공통 측정 조건

| 측정일 | 범위 | 실행·정답/상태 통과 | 당시 기본 설정 |
|---|---|---|---|
| 2026-10-10 | `pr reviews`, `pr inspect` | 12회 · 12/12 | `gpt-6.1-sol` / `low` |
| 2026-10-08 | 15개 전체 명령 | 90회 · 90/90 | `gpt-6.1-sol` / `high` |

모델·추론 강도는 매 실험의 사용자 기본값을 상속했습니다. 실행 인자에 모델·추론 강도와 ignore-user-config 옵션을 지정하지 않았습니다. JSON 이벤트가 실제 선택 모델·추론 강도를 노출하지 않아 설정 스냅샷과 옵션 생략으로 기록했습니다. 설정이 다른 실험을 합산하거나 과거 수치와의 차이를 코드 개선 효과로 해석하지 않습니다.

소스·CLI·실행기·프롬프트·정답 스키마·seed 20261008·실행 순서를 모델 호출 전에 고정했습니다. 명령 순서는 매 반복 섞고 각 명령의 방식 순서를 교차했으며, 매번 새 세션·임시 Git 저장소·CLI 캐시로 시작했습니다. 직접 처리도 배치·jq·로컬 스크립트를 허용했습니다. 실패·재시도도 포함하고 실패한 모델 실행을 교체하지 않았습니다.

캐시 제외 입력+출력은 input_tokens − cached_input_tokens + output_tokens, 총 토큰은 input_tokens + output_tokens입니다. 추론 출력을 중복 합산하지 않습니다. 어느 방식이든 3회 전부 정답·상태 검증을 통과하지 않으면 변화율 비교를 보류합니다. 원본에는 평균·중앙값·표본 표준편차·범위·각 쌍의 변화율·명령·API 수가 있습니다.

고정 합성 자료의 제어 실험입니다. 실제 CLI·gh·Git과 임시 bare 원격을 사용하고, API는 선택 필드·별칭·페이지를 처리하는 로컬 GraphQL 서버로 연결했습니다. 캐시는 강제로 초기화할 수 없어 캐시 제외 지표에도 적중 차이가 남습니다. 방식별 3회와 대표 시나리오에 한정하며 모든 옵션·대형 자료·오류 경로·실제 GitHub 성능·금액 절감을 대표하지 않습니다.

최신 재측정은 리뷰 compact 출력이 바뀌는 두 명령에 한정했습니다. PR 제출의 기존 ready 시나리오는 리뷰 스레드가 비어 있고 머지 성공 시나리오에도 대상 메타데이터가 없어 기존 결과를 유지합니다.

## 원본과 재현

- 2026-10-10: [작업 전체 원본](data/study-20261010-435bde.json) · [고정 소스·실행기](data/study-20261010-435bde-source.tar.gz) · [이벤트·답변·API·사전 검증](data/study-20261010-435bde-events.tar.gz)
- 2026-10-08: [작업 전체 원본](data/study.json) · [고정 소스·실행기](data/study-source.tar.gz) · [이벤트·답변·API·사전 검증](data/study-events.tar.gz)

압축 파일의 SHA-256은 각 JSON의 evidence.sha256에 있습니다. 명령별 문서에서는 최신 결과만 사용하고, 실험의 고정 소스·원본은 감사와 재현을 위해 한 번씩 보관합니다.

```sh
python3 -m pip install --require-hashes -r scripts/benchmarks/requirements.txt
# 변경된 명령만 모델 없는 사전 검증
python3 scripts/benchmarks/run_github_study.py --root /tmp/github-study --tasks pr-reviews pr-inspect
# 같은 기록에서 선택한 2개 명령 × 2개 방식 × 3회 실행
python3 /tmp/github-study/harness/run_github_study.py --root /tmp/github-study --resume --run-models
python3 scripts/benchmarks/publish_github_study.py --root /tmp/github-study
```

`--tasks`를 생략하면 15개 전체 명령을 측정합니다. 문서만 갱신하려면 `python3 scripts/benchmarks/publish_github_study.py --render-only`를 실행합니다. 모델 호출과 압축 원본 변경 없이 같은 명령별 구조를 생성합니다.

고정 소스 재현은 해당 source.tar.gz를 새 디렉터리에 풀고 scripts를 source/scripts로 복사한 뒤 그 안의 run_github_study.py를 실행합니다. 소스·작업의 재현을 위한 자료이며 모델 응답·토큰 수의 동일성을 보장하지 않습니다.
