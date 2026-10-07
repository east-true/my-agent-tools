# 벤치마크

측정 문서는 `docs/benchmarks/<명령어>/<하위-명령어>.md`에 둡니다. 명령어의 공백은 파일명에서 하이픈으로 바꾸며, 예를 들어 `github issue create`는 `github/issue-create.md`입니다. 옵션이나 측정 조건이 다른 결과는 같은 문서에서 구분합니다.

원본 JSON은 설명 문서와 같은 디렉터리에 짧은 이름으로 보관합니다. 여러 명령을 함께 측정한 원본은 공유하고 각 문서에서 해당 결과를 연결합니다. 재현 스크립트는 `scripts/benchmarks`에 있습니다.

| 명령 또는 실험 | 문서 | 원본 자료 |
|---|---|---|
| `github context` | [context](github/context.md) | [명령별 공유 실측](github/commands.json) |
| `github setup` | [setup](github/setup.md) | [명령별 공유 실측](github/commands.json) |
| `github dependabot list / view` | [dependabot](github/dependabot.md) | [목록·상세 실측](github/dependabot.json) |
| `github issue create` | [issue-create](github/issue-create.md) | [실제 생성](github/commands.json), [읽기 전용 계획](github/issue-create.json), [초기 측정](github/issue-create-unguided.json) |
| `github issue branch` | [issue-branch](github/issue-branch.md) | [명령별 공유 실측](github/commands.json) |
| `github pr create` | [pr-create](github/pr-create.md) | [명령별 공유 실측](github/commands.json) |
| `github branch cleanup` | [branch-cleanup](github/branch-cleanup.md) | [미리보기](github/commands.json), [실제 삭제](github/branch-cleanup.json) |
| CI 오류 추출 프로토타입 (`ci`, `ci-facts`) | [ci](github/ci.md) | [공유 후보 측정](github/candidates.json), [후속 측정](github/ci-internal.json) |
| 리뷰 추출 프로토타입 (`reviews`) | [reviews](github/reviews.md) | [공유 후보 측정](github/candidates.json) |
| PR 증분 조회 프로토타입 (`delta`) | [delta](github/delta.md) | [공유 후보 측정](github/candidates.json) |
| 반복 CI 코드 수정 실험 | [workflow](github/workflow.md) | [측정 자료](github/workflow.json) |

수치는 CLI 자체의 비용이 아니라 지시문·도구 상호작용·캐시 입력·출력을 포함한 에이전트 작업 전체의 토큰입니다. CLI와 내부 파서는 모델을 호출하지 않습니다. 실험별 측정 범위와 한계를 확인해야 하며, 총 토큰 감소만으로 금액 절감을 판단하지 않습니다.

추가 6개 작업은 [공통 조건·제외 기록·재현](github/command-suite.md)에 설명했습니다. 총 54회 중 최종 비교 36회가 정답·상태 검증을 통과했으며 제외한 18회도 공유 원본에 보존했습니다. 이후 README에 추가된 Dependabot 목록·상세 6회도 별도 보고서에 기록했습니다. 이번 추가 측정은 총 60회, 최종 비교는 모두 통과한 42회입니다.
