# `tools github ci rerun`

완료된 GitHub Actions 실행을 한 번 재실행하고 새 실행 회차의 완료까지 기다립니다. 기본은 실패한 job과 그에 의존하는 job이며, 다시 실패하면 해당 회차의 실패 자료를 함께 반환합니다.

```sh
tools github ci rerun --run 123456789 --json
tools github ci rerun --run 123456789 --all --json
tools github ci rerun --run 123456789 --wait=false --json
tools github ci rerun --run 123456789 --dry-run --json
# 시간 초과·대기 생략 후 같은 예상 회차만 다시 확인
tools github ci rerun --run 123456789 --resume --json
```

`--run`은 Actions 실행 ID이며 PR 번호와 다릅니다. 재실행은 원래 실행의 커밋을 사용하므로 수정 코드를 새로 푸시한 뒤 그 새 커밋을 검사하려면 새 실행을 확인합니다.

## 요청과 대기

- 실행 메타데이터와 재실행 직전 상태를 확인합니다. 이미 진행 중이거나 상태·회차가 바뀌면 `blocked`와 사유를 반환하고 요청하지 않습니다.
- 기본 모드에서 성공한 실행은 `blocked`입니다. 전체 workflow를 다시 실행하려면 `--all`을 사용합니다. 최종 재실행 가능 여부는 GitHub가 판단합니다.
- 재실행 요청은 한 번만 보냅니다. 요청이 수락된 뒤에도 이전 회차가 잠시 반환될 수 있으므로 `run_attempt`가 이전 회차보다 증가할 때까지 기다립니다. 이전 완료·성공 결과로 대기를 끝내지 않습니다.
- 예상 회차보다 더 새 회차가 관찰되면 `superseded`를 반환합니다. 다른 재실행의 결과를 이번 결과로 사용하지 않습니다. GitHub API는 요청별 회차 식별자를 제공하지 않아 동시에 시작된 같은 다음 회차의 요청자를 구분할 수는 없습니다.
- `--wait=false`는 수락 직후 `requested`로 끝납니다. 이는 CI 성공을 뜻하지 않습니다.
- 실패 시 관찰한 회차의 job·step·annotation·로그를 [ci failures](failures.md) 방식으로 수집합니다. 이후 다른 재실행이 시작되어도 실패 자료는 관찰한 회차를 사용합니다. 자료 일부를 얻지 못하면 `failure.complete: false`와 `failure.notes` 또는 job별 `notes`를 확인합니다.

## 옵션

| 옵션 | 의미 |
|---|---|
| `--run ID` | 양의 Actions 실행 ID, 필수 |
| `--repo OWNER/REPO` | 기본은 현재 Git `origin` |
| `--all` | 실패 job 대신 전체 workflow 재실행 |
| `--wait` | 기본 `true`. `--wait=false`는 요청 수락 후 반환 |
| `--dry-run` | 실행 적합성을 조회하고 계획만 반환 |
| `--resume` | 저장된 실행·SHA·예상 회차의 조회와 대기 재개. 새 재실행 요청 없음 |
| `--state-file FILE` | 체크포인트 경로 변경. 기본은 저장소 공통 `.tools/state/ci-rerun/<repo-hash>/<run-id>.json` |
| `--timeout DURATION` | 요청·대기·실패 자료 수집을 포함한 제한, 기본 `10m` |
| `--interval DURATION` | 조회 간격, 기본 `10s` |
| `--annotations=false` | 실패 annotation 조회 생략 |
| `--max-log-bytes N` | 실패 job별 로그 상한, 기본 8 MiB, 범위 1–134217728 바이트 |
| `--json` | 중간 상태 없이 최종 JSON 한 개 |
| `--compact`, `--artifact-dir DIR` | 긴 실패 자료의 원문 보존과 간결한 출력, 저장 경로 변경 |
| `--token-encoding`, `--artifact-retention`, `--artifact-limit` | [토큰 비교와 원문 보관](../README.md#간결한-출력) |

## 결과와 복구

결과는 `status`, `repo`, `mode`, `run`, `previous_attempt`, `expected_attempt`, `request_attempted`, `rerun_requested`, 선택적 `failure`, `notes`, `error`, `state_file`, `resumed`를 반환합니다. `run`은 마지막으로 확인한 실행이며 새 회차 반영 전에는 이전 회차일 수 있습니다.

| `status` | 의미 | 종료 코드 |
|---|---|---|
| `planned` | 조회만 완료, 요청하지 않음 | `0` |
| `requested` | 요청 수락, 완료를 기다리지 않음 | `0` |
| `completed` | 예상 새 회차가 성공으로 완료 | `0` |
| `failed` | 새 회차가 성공하지 못함. `failure`에 자료 포함 | `1` |
| `blocked` | 진행 중·성공 상태·직전 변경 등으로 요청하지 않음 | `1` |
| `superseded` | 다른 후속 회차가 진행됨 | `1` |
| `timeout` / `cancelled` | 대기 제한 또는 호출 취소 | `1` |
| `unknown` | 요청 전송 이후 결과 확인 불가 | `1` |
| `error` | 요청 전 오류 또는 GitHub가 명시적으로 요청 거부 | `1` |

잘못된 입력은 종료 코드 `2`입니다. 최초 인증·저장소 확인 실패는 공통 `status: "error"`, `error` 형태입니다.

`request_attempted: true`는 요청 전송을 시도했음을, `rerun_requested: true`는 수락 응답을 받았음을 뜻합니다. **시간 제한이나 취소는 수락된 재실행을 취소하지 않습니다.** `unknown`이거나 수락 후 대기가 끝나면 실행 페이지와 회차를 확인한 뒤 다음 조치를 결정합니다. 자동 재시도는 하지 않습니다.

## 저장과 대기 재개

정상 실행은 요청 전에 저장소·실행 ID·기존/예상 회차·SHA·모드를 저장하고, 수락 후 체크포인트를 갱신합니다. 최초 저장에 실패하면 재실행 요청을 보내지 않습니다. 응답을 잃거나 프로세스가 중단돼도 저장한 예상 회차를 `--resume`으로 확인할 수 있습니다. 요청 전 저장 직후 중단되었다면 실제 요청이 전송되지 않았을 수도 있습니다. 재개는 새 요청을 보내지 않으므로 이 경우에는 제한 시간까지 예상 회차를 기다릴 수 있습니다.

미완료 체크포인트가 있으면 일반 실행은 새 요청을 거부하고 `--resume`을 안내합니다. 예상 회차의 완료·실패를 확인한 뒤 일반 실행으로 새 재실행을 요청할 수 있습니다. `--resume`은 저장한 모드를 사용하며 명시한 `--all`이 다르면 오류입니다. `--dry-run`과 함께 사용할 수 없습니다. `--wait=false`로 재개하면 현재 관찰만 반환합니다.

이미 더 새 회차가 있으면 저장된 예상 attempt를 별도로 조회합니다. 새 회차의 결과로 대체하지 않습니다. 저장소·실행 ID·회차·SHA가 맞지 않거나 파일이 손상되면 오류입니다. `.lock`으로 동시 접근을 거부하며, 비정상 종료 후 남은 잠금은 실행 중인 소비자가 없는지 확인한 뒤 정리합니다. `--dry-run`은 체크포인트를 쓰지 않습니다. 연결 워크트리는 주 저장소의 상태 디렉터리를 공유합니다.

실패 자료는 다른 명령과 [완료 회차 캐시](../README.md#조회-재사용과-호출-제한)를 공유합니다. 기본 재실행·대기·실패 자료 수집 흐름은 한 호출로 이어집니다.

## 인증

Fine-grained 토큰에는 **Actions: write**, 실패 annotation 조회에는 **Checks: read**가 필요합니다. 인증은 공통 `GH_TOKEN` → `GITHUB_TOKEN` → 선택적 `gh auth token` 순서입니다. [재실행 API](https://docs.github.com/en/rest/actions/workflow-runs#re-run-failed-jobs-from-a-workflow-run), [재실행 커밋과 제한](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/re-run-workflows-and-jobs).

[공통 안내](../README.md) · [간결한 출력](../README.md#간결한-출력) · [CI 실패 자료](failures.md) · [PR 통합 조회](../pr/inspect.md) · [PR 머지](../pr/merge.md)

## 저장된 CI 원문 읽기

발췌에 없는 문맥은 `--read-evidence`와 예상 SHA·run·attempt·job·구간/패턴으로 선택해 읽는다. 원래 작업과 인증을 실행하지 않는 [공통 읽기 전용 계약](evidence.md)을 따른다.
