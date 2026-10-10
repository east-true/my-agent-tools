# `tools github pr reviews`

PR의 미해결 리뷰 스레드와 제출된 리뷰 본문을 한 번에 수집합니다. 에이전트가 수정 요청의 원문·대화·코드 위치를 확인할 때 사용합니다.

```sh
tools github pr reviews --number 123 --json
tools github pr reviews --number 123 --all --json
tools github pr reviews --repo OWNER/REPO --number 123
tools github pr reviews --number 123 --conversation --save-result .tools/review-123.json --json
tools github pr reviews --number 123 --source-path src/service.go --json
```

## 수집 범위

기본은 미해결 스레드입니다. 코드 변경으로 오래된 스레드도 미해결이면 포함하고 `is_outdated: true`로 구분합니다. `--all`은 해결된 스레드도 포함합니다. 스레드와 각 스레드의 답글을 모든 페이지에서 조회합니다.

각 스레드는 파일 경로, 현재·원래 줄과 시작 줄, diff 방향, 해결·오래됨 상태, 전체 대화를 반환합니다. 파일 단위 코멘트나 오래된 위치에는 `line: null`이 올 수 있으므로 `original_line`, `original_start_line`, `diff_side`도 확인합니다. 삭제된 작성자는 `author: null`로 보존합니다.

리뷰 본문은 스레드 필터와 별개로 제출된 리뷰 이력 전체를 반환합니다. `APPROVED`, `CHANGES_REQUESTED`, `COMMENTED`, `DISMISSED` 상태와 작성자·본문·대상 커밋·제출 시각·링크를 유지하며 미제출 `PENDING` 리뷰는 제외합니다. 과거 변경 요청이 현재도 유효하다고 판단하지 않습니다. 현재 집계는 `review_decision`에서 확인합니다. 필수 리뷰 정책이 없으면 이 값은 비어 있을 수 있습니다.

본문을 요약하거나 작업 목록으로 해석하지 않고 그대로 반환합니다. `--conversation`을 지정하면 일반 PR 대화 댓글도 모든 페이지에서 함께 수집해 `conversation` 배열에 담습니다. 빈 대화는 빈 배열이며 기본 조회에는 이 필드와 추가 API 조회가 없습니다. 댓글 ID·작성자(`user`, 삭제 시 null)·전체 본문·URL·생성/수정 시각을 유지하고 리뷰 이력·inline 스레드와 구분합니다. 빈 스레드 목록만으로 리뷰가 실행되었다고 판단하지 않습니다.

## 옵션과 결과

| 옵션 | 의미 |
|---|---|
| `--number NUMBER` | 양의 PR 번호, 필수 |
| `--repo OWNER/REPO` | 기본은 현재 Git `origin`. Git 밖에서는 명시 |
| `--all` | 해결된 스레드까지 포함 |
| `--conversation` | 일반 PR 대화 댓글도 함께 조회 |
| `--save-result FILE` | 완전한 축약 전 결과를 새 JSON 파일에 저장하고 SHA·read-back 검증 반환 |
| `--source-path FILE` | 지정한 로컬 파일의 전체 원문·SHA를 함께 반환, 반복 가능 |
| `--source-root DIR` | 로컬 조회 루트, 기본 현재 디렉터리. source-path 필요 |
| `--json` | 구조화된 최종 JSON 한 개 |
| `--compact` | 본문·diff hunk를 그대로 유지하고 중복 위치·시각 메타데이터를 줄임 |
| `--artifact-dir DIR` | 간결한 출력의 원문 저장 경로, 기본 `.tools/state/evidence` |
| `--token-encoding`, `--artifact-retention`, `--artifact-limit` | [토큰 비교와 원문 보관](../README.md#간결한-출력) |

JSON은 `status`, `repo`, `number`, `url`, `head_sha`, `review_decision`, `complete`, `all`, `reviews`, `threads`, 선택적 `notes`를 포함합니다. 리뷰·스레드가 없으면 빈 배열을 반환합니다.

`head_verified: true`는 수집 끝의 실제 head 재조회가 `head_sha`와 일치했다는 근거입니다. 전체 선택 자료에는 `complete`도 확인합니다. 이 값은 그 수집 시점의 증명이며 저장 파일을 읽은 시점이나 수정 후의 새로운 head를 보장하지 않습니다. 이 필드가 거짓이거나 없으면 검증 성공으로 해석하지 않습니다.

`--compact`는 빈 선택적 위치 필드와 현재 위치와 같은 원래 위치를 생략합니다. 같은 시작 diff 방향, 생성 시각과 같은 댓글 수정 시각도 생략합니다. 현재 `line: null`과 다른 원래 위치·실제 수정 시각은 유지합니다. 긴 본문과 `diff_hunk`도 출력에서 그대로 유지하므로 그 내용을 확인하기 위한 원문 파일 재조회가 필요 없습니다. 파일 참조까지 포함해 바이트·토큰이 모두 줄어들 때만 적용하며 정확한 전체 필드는 `evidence_file`에 보관합니다. 줄어들지 않으면 원래 전체 결과를 반환합니다. [작업 전체 비교](../../benchmarks/github/pr/reviews.md).

`complete`는 선택한 범위의 자료 수집이 완료되었는지를 뜻합니다. 후속 페이지·답글·리뷰 이력·일반 대화 조회에 실패하면 이미 모은 자료를 유지하고 `status: "partial"`, `complete: false`, `notes`를 반환합니다. 일반 대화를 포함해 수집이 끝난 뒤 head를 다시 확인합니다. 조회 중 head 커밋이 바뀌면 부분 결과로 표시하므로 새 커밋에 대해 다시 수집합니다. 여러 API 요청으로 얻은 스냅샷이며 조회 후 새 리뷰가 추가될 수 있습니다.

## 같은 작업에서 원문 재사용

`--save-result`는 첫 조회에서 완전한 원문을 새 파일에 저장합니다. `saved_result`의 절대 경로·실제 바이트 `sha256`·`verified`를 반환하며 저장 파일은 그 참조 필드가 없는 전체 결과입니다. 지정한 `source_files`도 포함합니다. compact 사용 여부와 관계없이 본문·diff·원래 위치·시각을 모두 보관합니다. 기존 파일을 덮어쓰거나 불완전한 수집을 저장하지 않습니다. 저장 실패 시 이미 수집한 자료와 오류를 `partial`로 반환합니다. 독립 1회 수정에 저장 파일을 의무화하지 않습니다.

이미 존재하는 저장 대상은 인증·API 수집 전에 종료 코드 2로 거부합니다. 사전 검사 뒤 파일 생성 경쟁 등 저장 실패가 생기면 수집한 원문은 `partial`에 유지합니다. 저장 파일은 일반 JSON이므로 알려진 경로에서 Python `json.load` 등으로 바로 읽고 SHA를 확인할 수 있습니다. 파일시스템 inspect의 줄 구간 형식으로 다시 변환할 필요가 없습니다.

수정 요청 해석·대상 확인에는 그 최초 결과를 재사용할 수 있습니다. 파일을 읽을 때 repo·PR·head·complete와 알고 있는 SHA를 확인하고, 매번 전체 리뷰를 다시 가져오는 절차를 추가하지 않습니다. 저장·읽기 비용까지 같은 작업의 비용입니다. 독립적인 리뷰 조회에 저장이나 연속 반복을 의무화하지 않습니다.

최종 검증이 현재 head만 요구하면 새 `gh api repos/OWNER/REPO/pulls/NUMBER --jq .head.sha` 조회와 실제 파일 read-back으로 확인할 수 있습니다. 최신 리뷰 상태까지 필요하면 head만으로 대신하지 않습니다. 같은 head에도 새 댓글·본문 수정·해결 상태가 생길 수 있으므로 [현재 미해결 자료를 포함하는 상태 조회](inspect.md)를 사용합니다. 저장 파일은 최신 원격 상태를 증명하는 캐시가 아닙니다.

| 종료 코드 | 의미 |
|---|---|
| `0` | 자료 수집 완료. 미해결 리뷰·변경 요청이 있어도 0 |
| `1` | 인증·API 오류 또는 부분 수집 |
| `2` | 잘못된 옵션·PR 번호 |

## 인증과 관련 기능

GitHub 읽기 전용이며 리뷰 제출·답글 작성·스레드 해결·머지를 수행하지 않습니다. Fine-grained 토큰에는 **Pull requests: read**가 필요합니다. [GraphQL 리뷰 스레드](https://docs.github.com/en/graphql/reference/pulls#pullrequestreviewthread), [리뷰 목록 API](https://docs.github.com/en/rest/pulls/reviews#list-reviews-for-a-pull-request). 일반 대화는 [issue comment 조회 API](https://docs.github.com/en/rest/issues/comments#list-issue-comments)를 사용하며 해당 저장소의 지원되는 Issues 또는 Pull requests 읽기 권한이 필요합니다.

[공통 안내](../README.md) · [통합·변경분 조회](inspect.md) · [간결한 출력](../README.md#간결한-출력) · [PR 생성](create.md) · [PR 머지](merge.md)

`--source-path`는 명시한 로컬 파일만 기존 `fs inspect --raw --hash`로 읽어 `source_files`에 함께 담습니다. 줄바꿈·마지막 개행·SHA와 완전성을 유지하며 GitHub 리뷰를 해석해 파일을 수정하지 않습니다. 로컬 바이트가 원격 PR head의 파일과 같다고 주장하지 않습니다. 수정할 때는 반환한 SHA를 preimage로 검사하고 적용 후 실제 파일을 검증합니다.

잘못된 경로는 인증 전에 거부합니다. 파일 누락·읽기 오류·출력 페이지가 있으면 리뷰 원문과 소스 문제·커서를 유지하고 전체 결과를 partial로 반환하며 저장하지 않습니다. 소스의 이어 읽기는 같은 root/path/raw/hash 조건으로 fs inspect를 사용합니다. 리뷰 전체를 다시 조회할 필요는 없습니다.

최초 일반 조회는 스레드와 제출 리뷰의 첫 페이지를 GraphQL로 함께 수집합니다. 스레드·리뷰 이력의 커서는 따로 진행하고 64비트 숫자 ID·원문·작성자·커밋·시각·URL을 보존합니다. 일반 댓글은 REST로 수집하며 마지막 head 검증은 유지합니다. 이미 저장된 스레드 본문을 사용하는 증분 조회는 기존 원문 생략 계약을 유지합니다. 선택 필드가 없는 응답은 REST로 보완하므로 모든 응답에서 요청 수가 같은 것은 아닙니다.

## 미해결 리뷰의 로컬 소스 연결

`--review-sources`는 수집된 미해결 inline 스레드의 경로를 중복 제거해 원문·SHA와 함께 `source_files`로 반환한다. `--source-root`와 알려진 `--source-path`를 함께 사용할 수 있다. 제출 리뷰 본문에서 경로를 추론하지 않으며 기본 조회 범위는 늘리지 않는다. 로컬 파일을 원격 head의 내용이라고 주장하지 않는다. 오래된 스레드 좌표는 그대로 유지하고 현재 줄로 추측해 바꾸지 않으며 없는 파일은 불완전한 소스 결과로 보고한다. `--compact`도 이 편집용 원문을 자르지 않는다. inspect에서는 reviews section을 선택해야 한다.
