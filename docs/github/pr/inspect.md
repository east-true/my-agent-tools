# `tools github pr inspect`

PR 상태·검사·미해결 리뷰·CI 실행 ID·실패 자료를 한 번에 조회합니다. GitHub를 변경하지 않으며, 조합 명령은 `--json` 없이도 최종 JSON 한 개를 반환합니다.

```sh
tools github pr inspect --number 123 --json
tools github pr inspect --number 123 --wait --json
tools github pr inspect --number 123 --state-file .tools/state/pr-123.json --json
tools github pr inspect --number 123 --sections reviews --state-file .tools/state/pr-123-reviews.json --json
tools github pr inspect --number 123 --sections reviews --conversation --state-file .tools/state/pr-123-conversation.json --json
```

## 수집과 출력

현재 head·base·test merge commit, 리뷰 집계와 머지 상태, 최신 Check Runs·Commit Statuses를 수집합니다. test merge commit에 검사 결과가 없으면 head를 사용합니다. 미해결 리뷰와 제출된 리뷰 이력은 [pr reviews](reviews.md)와 같습니다.

GitHub Actions 검사 링크에서 실행 ID를 찾고, 해당 실행의 커밋이 검사 대상과 일치할 때 그 실행 회차의 실패 자료를 수집합니다. 검사에 Actions 실행 링크가 없으면 run ID나 작업 로그를 추측하지 않습니다. 외부 CI는 검사 설명·링크로 확인합니다.

`--sections checks,reviews,failures`로 필요한 조회만 선택합니다. 기본 `all`은 세 범위 전체이며 PR 메타데이터는 항상 포함합니다. `reviews`만 선택하면 검사·Actions 자료를 조회하지 않습니다. `failures`는 실행 연결을 찾기 위한 검사 조회를 포함하지만 검사 목록은 출력하지 않습니다. `--wait`는 `checks` 또는 `failures` 선택 시 사용합니다.

`--wait`는 진행 중인 검사를 내부에서 기다립니다. 검사 실패·PR 종료가 확인되면 대기를 끝냅니다. 읽는 동안 head·base·검사 커밋·리뷰/머지 조건이 바뀌거나 자료 수집이 불완전하면 부분 결과로 표시합니다. 여러 API 요청으로 얻은 스냅샷이므로 반환 후 상태가 바뀔 수 있습니다.

`--compact`는 기본 `true`입니다. 긴 출력은 원문 파일과 SHA-256을 보존한 뒤 필요한 문맥만 반환합니다. 짧은 결과는 그대로 반환합니다. [간결한 출력](../README.md#간결한-출력).

## 변경분 조회

`--conversation`을 선택하면 일반 PR 대화도 [공통 리뷰 수집기](reviews.md)에서 읽습니다. `reviews.conversation`에 정확한 전체 대화가 있고, 증분의 `outstanding.conversation`에도 선택한 현재 대화를 포함합니다. 댓글을 자동으로 수정 요청이나 차단 사유로 해석하지 않습니다. `reviews` 범위가 필요하며 일반 대화 선택 여부가 다른 상태 파일은 재사용하지 않습니다. 같은 head의 새 댓글·본문 수정도 매번 다시 확인합니다.

`--state-file FILE`을 주면 최초에는 전체 현재 결과를, 같은 head의 후속 조회에는 추가·수정·제거된 항목과 현재 미해결 작업을 반환합니다. 변경 목록의 댓글은 개별 ID로 비교합니다. 현재 필요한 대화와 실패 근거는 `outstanding`에 함께 담아 이전 문맥 없이도 처리할 수 있게 합니다.

- 변화가 없으면 `status: "unchanged"`와 현재 `pr_status`, `attention_required`, 상태 파일 경로를 반환합니다.
- 변화가 있으면 `status: "changed"`, `added`, `changed`, `removed`를 반환합니다. 제거는 관찰 범위에서 사라졌다는 뜻이며 모두 해결됐다는 판정은 아닙니다.
- `outstanding`은 현재 `pr`, 차단·대기 `reasons`, 성공하지 않은 `checks`, 해당 회차 `failures`, 미해결 `threads`의 전체 대화·위치를 포함합니다. 선택 범위 내 자료만 담고, 해결된 스레드와 제출된 리뷰 이력은 재출력하지 않습니다. 처리할 자료가 없으면 이 필드를 생략합니다.
- head가 바뀌면 전체 결과로 다시 시작합니다. `--full`은 같은 head에서도 전체 현재 결과를 반환합니다.
- 상태 파일은 저장소·PR·선택 범위별로 구분합니다. 다른 범위·버전·잘못된 파일은 오류로 반환하며 덮어쓰지 않습니다. 같은 옵션의 순서만 다른 경우에는 동일한 범위로 처리합니다.
- 완전한 조회와 출력이 성공한 뒤에만 기준을 저장합니다. 부분 조회·출력 실패는 기존 기준을 유지합니다. 동시 접근은 `.lock` 파일로 거부합니다. 프로세스가 비정상 종료했다면 실행 중인 소비자가 없는지 확인한 뒤 남은 잠금 파일을 정리합니다.

상태 파일은 **관찰 기준**입니다. `unchanged`가 작업 완료나 실패 해결을 뜻하지 않습니다. `attention_required: true`이면 차단·대기 항목이 남아 있으므로 `outstanding`을 확인합니다. 다른 작업 후 복귀했을 때 미해결 원문 때문에 전체 재조회를 할 필요는 없습니다. 성공 검사·전체 실행 목록·제출된 리뷰 이력까지 필요하면 처음부터 `--full`을 사용합니다. 긴 CI 로그의 생략 여부는 compact의 표시를 확인하고 원문이 필요하면 `--compact=false`를 지정합니다.

[최신 작업 전체 측정](../../benchmarks/github/pr/inspect.md)의 시나리오·정답·완료 범위를 확인합니다. 기존 전체 조회와 복귀 시나리오를 서로 같은 작업으로 간주하지 않습니다.

후속 조회는 실행 메타데이터로 저장소·커밋·실행 ID·attempt·완료 상태·결론을 재확인한 뒤, 일치하는 완전한 CI 자료만 재사용합니다. 새 attempt와 부분 자료는 다시 수집합니다. annotation·로그 상한 옵션이 달라져도 다시 수집합니다. 재사용 건수는 `reused_evidence`로 표시합니다.

같은 head·base에서는 댓글 ID와 수정 시각을 먼저 조회하고, 새 댓글·수정된 댓글의 본문과 diff hunk만 추가로 가져옵니다. 변경 댓글은 페이지와 스레드 전체에서 모아 최대 100개씩 일괄 조회합니다. head·base가 바뀌면 이전 본문을 재사용하지 않습니다. 제출된 리뷰 이력은 다시 조회합니다. 상태 파일에는 축약 전 자료를 저장하므로 출력용 원문 보관 파일과 별개입니다.

상태 파일 없이도 완료 회차의 자료는 `ci failures`·`ci rerun`·`pr merge`와 [공통 캐시](../README.md#조회-재사용과-호출-제한)를 사용합니다. `reused_evidence`는 상태 파일에서 재사용한 건수이며 공통 캐시 사용을 모두 집계하는 필드는 아닙니다.

## 옵션과 결과

| 옵션 | 의미 |
|---|---|
| `--number NUMBER` | 양의 PR 번호, 필수 |
| `--repo OWNER/REPO` | 기본은 Git `origin` |
| `--sections checks,reviews,failures` | 쉼표로 수집 범위 선택. 기본 `all` |
| `--conversation` | reviews 범위에서 일반 PR 대화도 포함. 상태 파일의 선택 범위와 연결 |
| `--wait` | 검사 완료 대기, 기본 `false` |
| `--timeout`, `--interval` | 전체 조회 제한 `10m`, 조회 간격 `10s` |
| `--state-file FILE`, `--full` | 변경분 기준 저장, 전체 현재 결과 강제 반환 |
| `--annotations=false` | CI 실패 annotation 조회 생략 |
| `--max-log-bytes N` | 실패 job별 로그 다운로드 상한, 기본 8 MiB |
| `--compact=false`, `--artifact-dir DIR` | 전체 출력, 원문 저장 경로 변경 |
| `--token-encoding`, `--artifact-retention`, `--artifact-limit` | [토큰 비교와 원문 보관](../README.md#간결한-출력) |

전체 결과는 `status`, `repo`, `number`, `sections`, `complete`, `pr`과 선택한 범위의 `checks`, `reviews`, `runs`, `failures`, 선택적 `reasons`, `notes`를 포함합니다. `ready`는 선택한 범위에서 관찰된 조건을 만족한다는 뜻이며 머지 가능성을 보장하지 않습니다. `blocked`, `pending`, `merged`는 상태 보고입니다. 완전한 자료 수집은 종료 코드 `0`, 부분 결과·시간 초과·API·파일 오류는 `1`, 잘못된 입력은 `2`입니다.

Fine-grained 토큰에는 Pull requests·Checks·Commit statuses·Actions 읽기 권한이 필요합니다. [공통 안내](../README.md) · [실패 자료](../ci/failures.md) · [제출 조합](submit.md) · [머지](merge.md).

## 저장된 CI 원문 읽기

발췌에 없는 문맥은 `--read-evidence`와 예상 SHA·run·attempt·job·구간/패턴으로 선택해 읽는다. 원래 작업과 인증을 실행하지 않는 [공통 읽기 전용 계약](../ci/evidence.md)을 따른다.

## 미해결 리뷰의 로컬 소스 연결

`--review-sources`는 수집된 미해결 inline 스레드의 경로를 중복 제거해 원문·SHA와 함께 `source_files`로 반환한다. `--source-root`와 알려진 `--source-path`를 함께 사용할 수 있다. 제출 리뷰 본문에서 경로를 추론하지 않으며 기본 조회 범위는 늘리지 않는다. 로컬 파일을 원격 head의 내용이라고 주장하지 않는다. 오래된 스레드 좌표는 그대로 유지하고 현재 줄로 추측해 바꾸지 않으며 없는 파일은 불완전한 소스 결과로 보고한다. `--compact`도 이 편집용 원문을 자르지 않는다. inspect에서는 reviews section을 선택해야 한다.

같은 head의 `unchanged` 결과도 `outstanding.reviews`에 현재 수집한 제출 리뷰의 원문·작성자·상태·대상 커밋·시각·URL을 제공한다. 이력 본문을 현재 수정 의무로 재해석하지 않으며 checks만 선택한 조회에는 리뷰 이력을 붙이지 않는다.
