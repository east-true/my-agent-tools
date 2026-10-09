# `tools github pr reviews`

PR의 미해결 리뷰 스레드와 제출된 리뷰 본문을 한 번에 수집합니다. 에이전트가 수정 요청의 원문·대화·코드 위치를 확인할 때 사용합니다.

```sh
tools github pr reviews --number 123 --json
tools github pr reviews --number 123 --all --json
tools github pr reviews --repo OWNER/REPO --number 123
```

## 수집 범위

기본은 미해결 스레드입니다. 코드 변경으로 오래된 스레드도 미해결이면 포함하고 `is_outdated: true`로 구분합니다. `--all`은 해결된 스레드도 포함합니다. 스레드와 각 스레드의 답글을 모든 페이지에서 조회합니다.

각 스레드는 파일 경로, 현재·원래 줄과 시작 줄, diff 방향, 해결·오래됨 상태, 전체 대화를 반환합니다. 파일 단위 코멘트나 오래된 위치에는 `line: null`이 올 수 있으므로 `original_line`, `original_start_line`, `diff_side`도 확인합니다. 삭제된 작성자는 `author: null`로 보존합니다.

리뷰 본문은 스레드 필터와 별개로 제출된 리뷰 이력 전체를 반환합니다. `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`, `DISMISSED` 상태와 작성자·본문·대상 커밋·제출 시각·링크를 유지하며 미제출 `PENDING` 리뷰는 제외합니다. 과거 변경 요청이 현재도 유효하다고 판단하지 않습니다. 현재 집계는 `review_decision`에서 확인합니다. 필수 리뷰 정책이 없으면 이 값은 비어 있을 수 있습니다.

본문을 요약하거나 작업 목록으로 해석하지 않고 그대로 반환합니다. 일반 PR Conversation 코멘트는 수집 대상에 포함되지 않습니다. 리뷰 원문으로 수정 작업을 준비합니다.

## 옵션과 결과

| 옵션 | 의미 |
|---|---|
| `--number NUMBER` | 양의 PR 번호, 필수 |
| `--repo OWNER/REPO` | 기본은 현재 Git `origin`. Git 밖에서는 명시 |
| `--all` | 해결된 스레드까지 포함 |
| `--json` | 구조화된 최종 JSON 한 개 |
| `--compact` | 긴 본문·diff hunk를 원문 파일에 보존하고 간결한 JSON 출력 |
| `--artifact-dir DIR` | 간결한 출력의 원문 저장 경로, 기본 `.tools/state/evidence` |
| `--token-encoding`, `--artifact-retention`, `--artifact-limit` | [토큰 비교와 원문 보관](../README.md#간결한-출력) |

JSON은 `status`, `repo`, `number`, `url`, `head_sha`, `review_decision`, `complete`, `all`, `reviews`, `threads`, 선택적 `notes`를 포함합니다. 리뷰·스레드가 없으면 빈 배열을 반환합니다.

`--compact`는 빈 선택적 위치 필드와 현재 위치와 같은 원래 위치를 생략합니다. 같은 시작 diff 방향, 생성 시각과 같은 댓글 수정 시각도 생략합니다. 현재 `line: null`과 다른 원래 위치·실제 수정 시각은 유지하며 정확한 전체 필드는 `evidence_file`에 보관합니다. [중복 제거의 출력 토큰 검증](../../benchmarks/github/pr/reviews.md).

`complete`는 선택한 범위의 자료 수집이 완료되었는지를 뜻합니다. 후속 페이지·답글·리뷰 이력 조회에 실패하면 이미 모은 자료를 유지하고 `status: "partial"`, `complete: false`, `notes`를 반환합니다. 조회 중 head 커밋이 바뀌면 부분 결과로 표시하므로 새 커밋에 대해 다시 수집합니다. 여러 API 요청으로 얻은 스냅샷이며 조회 후 새 리뷰가 추가될 수 있습니다.

| 종료 코드 | 의미 |
|---|---|
| `0` | 자료 수집 완료. 미해결 리뷰·변경 요청이 있어도 0 |
| `1` | 인증·API 오류 또는 부분 수집 |
| `2` | 잘못된 옵션·PR 번호 |

## 인증과 관련 기능

읽기 전용이며 리뷰 제출·답글 작성·스레드 해결·머지를 수행하지 않습니다. Fine-grained 토큰에는 **Pull requests: read**가 필요합니다. [GraphQL 리뷰 스레드](https://docs.github.com/en/graphql/reference/pulls#pullrequestreviewthread), [리뷰 목록 API](https://docs.github.com/en/rest/pulls/reviews#list-reviews-for-a-pull-request).

[공통 안내](../README.md) · [통합·변경분 조회](inspect.md) · [간결한 출력](../README.md#간결한-출력) · [PR 생성](create.md) · [PR 머지](merge.md)
