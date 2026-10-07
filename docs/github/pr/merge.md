# `tools github pr merge`

한 번 호출하면 PR 검사 완료를 기다리고, 조건을 충족하면 머지 완료 후 성공을 반환합니다. 검사 실패나 머지 차단이 확인되면 원인을 조회해 최종 결과에 포함합니다. 별도의 검사·대기 명령을 호출할 필요가 없습니다.

```sh
tools github pr merge --number 123 --json
tools github pr merge --number 123 --timeout 20m --method squash --json
```

## 옵션

| 옵션 | 동작 |
|---|---|
| `--number NUMBER` | 필수. 양의 PR 번호 |
| `--repo OWNER/REPO` | 기본은 현재 `origin`. Git 밖에서는 명시 |
| `--timeout DURATION` | 검사·머지 완료 대기 전체 제한 시간. 기본 `10m` |
| `--interval DURATION` | 내부 조회 간격. 기본 `10s` |
| `--method squash\|merge\|rebase` | 직접 머지 방식. 기본 `squash`. 머지 큐는 저장소 설정 사용 |
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

## 실패 진단

실패한 Check Run의 설명·오류 주석을 조회합니다. GitHub Actions 작업 링크가 있으면 실패 단계와 작업 로그도 조회합니다. 로그는 최대 2 MiB만 읽고 오류 표시가 있는 줄을 추려 반환합니다. 전체 로그와 로그 다운로드용 임시 URL은 출력하지 않습니다.

`reasons`에는 원인 코드, 검사 이름, 확인된 설명, 상세 링크, 다음 조치가 포함됩니다. `details`는 실제 검사 설명·주석·작업 단계·로그의 근거를 담으며 추측한 근본 원인을 생성하지 않습니다. 추가 진단에 실패해도 원래 검사 실패를 유지하고 `diagnostic_error`를 표시합니다.

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
  "merge_requested": true
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
| `merged` | 실제 머지 완료 확인. 종료 코드 `0` |
| `blocked` | 검사 실패 또는 머지 불가. 원인을 고친 뒤 재실행 |
| `timeout` | 제한 시간 만료. `reasons`에 미완료 항목 포함 |
| `cancelled` | 호출 컨텍스트 취소 |
| `error` | 머지 요청 전 인증·조회 등 실행 오류 |
| `unknown` | 머지 요청 이후 API 오류 등으로 결과 확인 불가 |

`merged` 외 결과는 종료 코드 `1`, 잘못된 옵션은 `2`입니다.

`merge_requested: true`이면 머지 요청을 전송한 상태입니다. `request_id`는 수락된 비동기 요청 UUID이며 `queued: true`는 큐 등록을 확인했다는 뜻입니다. **시간 제한·취소는 GitHub에 수락된 요청이나 큐 등록을 취소하지 않습니다.** 이후에도 머지될 수 있으므로, 해당 필드가 있거나 결과가 `unknown`이면 원격 PR 상태를 확인한 뒤 재실행합니다. 머지 큐에서 제거되면 `merge_queue_removed`와 확인 가능한 진단을 반환합니다.

[공통 안내](../README.md) · [PR 생성](create.md) · [브랜치 정리](../branch/cleanup.md)
