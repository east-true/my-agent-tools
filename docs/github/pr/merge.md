# `tools github pr merge`

한 번 호출하면 PR 검사 완료를 기다리고, 조건을 충족하면 실제 머지를 확인한 뒤 해당 PR의 브랜치와 연결 워크트리 정리까지 이어갑니다. 검사 실패나 머지 차단이 확인되면 원인을 조회해 최종 결과에 포함합니다.

```sh
tools github pr merge --number 123 --json
tools github pr merge --number 123 --timeout 20m --method squash --json
# 머지만 수행하고 정리는 생략
tools github pr merge --number 123 --cleanup=false --json
```

## 옵션

| 옵션 | 동작 |
|---|---|
| `--number NUMBER` | 필수. 양의 PR 번호 |
| `--repo OWNER/REPO` | 기본은 현재 `origin`. Git 밖에서는 명시 |
| `--timeout DURATION` | 검사·머지 완료 대기 전체 제한 시간. 기본 `10m` |
| `--interval DURATION` | 내부 조회 간격. 기본 `10s` |
| `--method squash\|merge\|rebase` | 직접 머지 방식. 기본 `squash`. 머지 큐는 저장소 설정 사용 |
| `--cleanup=false` | 기본으로 이어지는 해당 PR 브랜치 정리 생략 |
| `--remote NAME`, `--scope both\|local\|remote` | 정리 원격·범위. 기본 `origin`, `both` |
| `--annotations=false` | 진단용 CI·Check annotation 조회 생략. 기본은 조회 |
| `--max-log-bytes N` | 실패 job별 다운로드 상한. 기본 8 MiB, 범위 1–134217728 바이트 |
| `--compact=false`, `--artifact-dir DIR` | 기본 간결한 출력 대신 전체 JSON 반환, 원문 저장 경로 변경 |
| `--token-encoding ENCODING` | 출력 토큰 비교 인코딩. 기본 `o200k_base`, `cl100k_base`도 지원 |
| `--artifact-retention DURATION`, `--artifact-limit N` | 원문 보관 기간·개수. 기본 `720h`, `200`; 각각 `0`이면 제한 해제 |
| `--json` | 중간 상태 없이 최종 JSON 한 개 출력 |

기간은 `30s`, `10m` 같은 양수이며, `--wait` 없이 항상 내부에서 기다립니다. `--config`·`--dry-run` 옵션은 받지 않습니다.

## 검사와 머지 조건

- GitHub Check Runs와 Commit Statuses를 모든 페이지에서 조회합니다. Commit Statuses는 같은 context의 최신 결과만 사용합니다. 필수 여부와 관계없이 조회된 검사 중 하나라도 실패하면 머지하지 않습니다.
- GitHub에서 사용하는 test merge commit에 검사 결과가 있으면 해당 커밋을 검사하고, 없으면 최신 head 커밋을 검사합니다. [GitHub 검사 대상 규칙](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks).
- 완료된 Check Run의 `success`·`skipped`·`neutral`은 GitHub 기준에 따라 통과로 취급합니다. Commit Status는 `success`만 통과합니다. 진행 중인 검사는 기다리고, 검사 자체가 없으면 `checks_missing`으로 시간 제한까지 기다립니다.
- 닫힌 PR·초안·충돌·필수 리뷰 부족·변경 요청·필수 브랜치 갱신은 차단 사유를 반환합니다. 커밋이나 base가 바뀌면 기존 검사 결과로 머지하지 않습니다. head 변경은 `head_changed`로 종료하며 새 커밋은 새 호출에서 검사합니다.
- 검사 완료 후 head·base·test merge commit과 머지 조건을 다시 확인하고, 검사한 head SHA를 지정해 머지를 요청합니다. 저장소 규칙을 우회하지 않습니다.
- GitHub의 비동기 머지 API를 사용하며, 머지 큐가 설정된 브랜치는 큐를 따릅니다. 요청 수락·큐 등록을 성공으로 반환하지 않고 실제 머지까지 기다립니다. [머지 API와 큐 결과](https://docs.github.com/en/rest/pulls/pulls#merge-a-pull-request-asynchronously).
- 머지 요청은 한 번만 전송하며 자동으로 재시도하지 않습니다. 이미 머지된 PR은 기존 머지 SHA를 반환합니다. 이미 큐에 있는 PR은 새 요청 없이 완료를 기다립니다.

## 머지 후 정리

머지 요청 수락이나 큐 등록만으로 정리하지 않습니다. 실제 머지가 확인되면 PR head 브랜치만 지정해 정리 계획을 만들고 적용합니다. 다른 종료 브랜치는 조회·정리 대상에 넣지 않으며, 해당 브랜치를 추적하는 로컬 별칭은 같은 안전 검사를 거칩니다.

[branch cleanup](../branch/cleanup.md)의 보호 조건을 그대로 적용합니다. 열린 PR, 보호 브랜치, 미게시 커밋, 실행 중이거나 잠긴 워크트리, 변경 파일이 있는 워크트리는 보존합니다. 머지된 head와 다른 SHA도 삭제하지 않습니다. fork·삭제된 원본 저장소의 브랜치는 정리를 건너뛰고 `notes`로 알립니다.

정리 실패·일부 대상 보존은 `status: "partial"`, `merged: true`, `merge_sha`와 `cleanup.actions` 또는 `error`로 구분합니다. 이미 성공한 머지를 실패로 되돌리지 않습니다. 현재 작업 워크트리가 보존되었다면 주 작업 폴더로 이동해 `tools github branch cleanup --branch BRANCH --json`으로 확인한 뒤 `--apply`로 정리를 마칠 수 있습니다. 기본 정리에는 해당 저장소의 로컬 Git 작업 폴더와 원격 전송 권한이 필요합니다. Git 밖에서 머지만 수행하려면 `--repo`와 `--cleanup=false`를 사용합니다.

## 실패 진단

실패 자료는 [ci failures](../ci/failures.md)와 같은 수집기를 사용합니다. `ci failures`는 해당 기능만 사용하는 독립 명령으로 유지합니다. `pr merge`의 검사·대기·머지·정리 흐름에는 실패 진단이 그대로 연결됩니다.

이미 조회한 Check 설명을 재사용하며 annotation은 모든 페이지에서 수집합니다. GitHub Actions job 링크가 있으면 job의 저장소·Check ID·커밋·실행 회차를 검증한 뒤 해당 attempt의 job·step·annotation·로그·구조화된 오류 자료를 수집합니다. 최신 재실행 회차를 대신 사용하지 않으며 같은 실행·attempt는 머지 호출 안에서 한 번만 수집합니다. 외부 CI는 Check 설명과 annotation을 반환합니다. 확인할 수 없는 실행·job·다른 커밋의 로그는 포함하지 않습니다.

JSON의 `failures`는 실행 회차별 `ci failures` 형식입니다. `reasons[].run_id`, `run_attempt`로 관련 자료를 찾습니다. `reasons[].check`에는 축약 전 Check 설명과 수집 완전성, 외부 CI 또는 Actions 진단을 얻지 못한 경우의 annotation이 있습니다. Actions annotation은 해당 `failures[].failed_jobs[].annotations`에 보관합니다. 지원하는 컴파일 오류는 사실로 구조화하고, 테스트 실패·미지원 로그는 원문 문맥을 보존합니다.

다운로드 상한을 넘기면 `truncated: true`, `failures[].complete: false`로 표시합니다. 공통 수집기의 긴 실패 자료를 반복 출력하지 않도록 JSON의 `--compact`는 기본 `true`입니다. 바이트와 지정 인코딩의 토큰이 모두 줄어들 때만 축약하며 생략한 자료는 파일과 SHA-256으로 보존합니다. `--compact=false --json`은 전체 수집 결과를 반환합니다. `--json`을 생략하면 기존 텍스트 출력을 유지하며 `--compact`를 명시하면 간결한 JSON을 선택합니다. 텍스트 출력과 `reasons[].details`는 제한된 읽기용 근거입니다. 로그 다운로드용 임시 URL은 출력하지 않습니다.

`reasons`에는 원인 코드, 검사 이름, 확인된 설명, 상세 링크, 다음 조치가 포함됩니다. `details`는 실제 검사 설명·주석·작업 단계·로그의 근거를 담으며 추측한 근본 원인을 생성하지 않습니다. 추가 진단 실패·부분 수집에도 원래 `blocked`와 검사 실패를 유지하고 `diagnostic_error`, `check.complete`, `failures[].complete`로 구분합니다.

Fine-grained 토큰에는 Pull requests·Checks·Commit statuses 읽기, 머지용 Contents 쓰기 권한이 필요합니다. GitHub Actions 단계·로그 진단에는 Actions 읽기도 필요합니다. 인증 방식은 [공통 안내](../README.md)를 따릅니다.

## 결과와 복구

성공 예시:

```json
{
  "status": "merged",
  "repo": "owner/repo",
  "number": 123,
  "url": "https://github.com/owner/repo/pull/123",
  "head_sha": "checked-head-sha",
  "merge_sha": "merge-commit-sha",
  "merge_requested": true,
  "merged": true,
  "cleanup": {"status": "completed", "repo": "owner/repo", "actions": []}
}
```

실패 예시:

```json
{
  "status": "blocked",
  "repo": "owner/repo",
  "number": 123,
  "url": "https://github.com/owner/repo/pull/123",
  "head_sha": "checked-head-sha",
  "reasons": [
    {
      "code": "check_failed",
      "name": "test",
      "summary": "Check concluded failure",
      "details": "login_test.go:12: expected 200, received 500",
      "url": "https://github.com/owner/repo/actions/runs/456/job/789",
      "next_action": "Fix the reported failure and rerun checks"
    }
  ]
}
```

| `status` | 의미 |
|---|---|
| `merged` | 실제 머지 완료와 요청한 정리 완료 확인. fork 등의 정리 생략은 `notes` 확인. 종료 코드 `0` |
| `partial` | 머지는 완료했지만 정리 실패·일부 보존. `merged`, `cleanup`, `error` 확인 |
| `blocked` | 검사 실패 또는 머지 불가. 원인을 고친 뒤 재실행 |
| `timeout` | 제한 시간 만료. `reasons`에 미완료 항목 포함 |
| `cancelled` | 호출 컨텍스트 취소 |
| `error` | 머지 요청 전 인증·조회 등 실행 오류 |
| `unknown` | 머지 요청 이후 API 오류 등으로 결과 확인 불가 |

`merged` 외 결과는 종료 코드 `1`, 잘못된 옵션은 `2`입니다.

`merge_requested: true`이면 머지 요청을 전송한 상태입니다. `request_id`는 수락된 비동기 요청 UUID이며 `queued: true`는 큐 등록을 확인했다는 뜻입니다. **시간 제한·취소는 GitHub에 수락된 요청이나 큐 등록을 취소하지 않습니다.** 이후에도 머지될 수 있으므로, 해당 필드가 있거나 결과가 `unknown`이면 원격 PR 상태를 확인한 뒤 재실행합니다. 머지 큐에서 제거되면 `merge_queue_removed`와 확인 가능한 진단을 반환합니다.

[공통 안내](../README.md) · [PR 생성](create.md) · [브랜치 정리](../branch/cleanup.md)

## 저장된 CI 원문 읽기

발췌에 없는 문맥은 `--read-evidence`와 예상 SHA·run·attempt·job·구간/패턴으로 선택해 읽는다. 원래 작업과 인증을 실행하지 않는 [공통 읽기 전용 계약](../ci/evidence.md)을 따른다.
