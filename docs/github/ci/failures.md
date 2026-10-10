# `tools github ci failures`

명시한 GitHub Actions 실행의 실패 자료만 한 번에 수집하는 독립 읽기 전용 명령입니다. [pr merge](../pr/merge.md)·[pr inspect](../pr/inspect.md)·[ci rerun](rerun.md)도 내부에서 같은 수집기를 사용합니다.

완료 회차의 완전한 자료는 다른 명령과 [공통 캐시](../README.md#조회-재사용과-호출-제한)를 공유합니다. 실행 메타데이터는 다시 확인하며 부분 자료와 옵션이 다른 자료는 재사용하지 않습니다.

```sh
tools github ci failures --run 123456789 --json
tools github ci failures --repo OWNER/REPO --run 123456789 --json

# Checks 권한이 없으면 annotation 조회를 생략
tools github ci failures --run 123456789 --annotations=false --json
```

`--run`은 Actions 실행 ID이며 PR 번호와 다릅니다. 저장소를 생략하면 Git `origin`에서 찾습니다. 기존 `GH_TOKEN` → `GITHUB_TOKEN` → 선택적 `gh auth token` 인증을 사용합니다. 프로젝트 설정 파일은 읽지 않습니다.

## 수집 범위

실행 메타데이터를 먼저 조회하고 그 `run_attempt`의 job 목록을 모든 페이지에서 가져옵니다. 이전 attempt와 새 재실행을 섞지 않습니다. 실패·시간 초과·취소 등 성공하지 못한 job만 로그를 다운로드하며, 결과에는 해당 job과 실패한 step의 메타데이터를 포함합니다.

각 job의 Check run에서 `failure`·`warning` annotation을 모든 페이지에서 수집합니다. GitHub가 Check run URL을 제공하지 않는 job은 annotation 조회를 생략합니다.

step 시각과 로그의 timestamp를 확실히 연결할 수 있을 때 실패 step의 로그를 추출합니다. timestamp가 없거나 순서·경계가 불확실하면 job 전체 로그를 남깁니다.

지원하는 완전한 Go 컴파일 오류는 다음 사실로 구조화합니다.

- 파일·줄·열·컴파일러 메시지와 원문 위치
- `want (...)`에 근거한 함수별 예상 인자 수
- 인터페이스의 누락 메서드
- 프로세스 종료 코드

Go의 `--- FAIL: TestName`과 pytest의 `FAILED path::test - message`에서도 명시된 프레임워크·테스트 이름·경로·메시지를 `tests`로 추출합니다. 이 경우에도 해당 구간의 원문 로그를 함께 남깁니다.

서로 같은 구조화된 오류는 matrix job 간에 합치고, 각 job·step·원문 줄·URL을 `occurrences`에 보존합니다. 로그 비교에서는 timestamp와 ANSI 표시만 제거하며, 시간만 다른 원문은 `log_variants`에 보존합니다. 다른 assertion 메시지와 오류는 별도로 반환합니다. 네트워크 오류, 잘린 signature 등 지원하지 않는 자료는 해당 step 또는 job의 사용 가능한 로그 전체를 남깁니다. 오류의 근본 원인을 추론하지 않습니다.

## 옵션과 결과

| 옵션 | 의미 |
|---|---|
| `--run ID` | 양의 Actions 실행 ID, 필수 |
| `--repo OWNER/REPO` | 대상 저장소 |
| `--json` | 구조화된 JSON 출력 |
| `--annotations=false` | annotation 조회 생략, 기본은 조회 |
| `--max-log-bytes N` | job별 다운로드 상한, 기본 8 MiB, 범위 1–134217728 바이트 |
| `--compact`, `--artifact-dir DIR` | 긴 자료의 원문 보존과 간결한 출력, 저장 경로 변경 |
| `--token-encoding`, `--artifact-retention`, `--artifact-limit` | [토큰 비교와 원문 보관](../README.md#간결한-출력) |

JSON 결과에는 `status`, `repo`, `run`, `complete`, `failed_jobs`, `evidence`, 선택적 `notes`가 있습니다.

`evidence.kind: "go_compiler"`는 지원하는 구조화된 컴파일러 사실입니다. `kind: "test_failure"`는 명시된 테스트 실패 사실과 원문, `kind: "log"`는 전체 사용 가능한 로그를 `lines`로 반환합니다. `--compact`가 적용되면 `log_variants`는 원문 파일로 옮기고 `log_variants_omitted`로 표시합니다. `occurrences.start_line/end_line`은 다운로드한 job 로그의 1 기반 줄 번호이며, diagnostic과 test의 `evidence_line`은 해당 구간 안의 1 기반 위치입니다.

`complete`는 자료 수집의 완전성을 뜻하며 원인 분석의 완전성이나 CI 성공을 뜻하지 않습니다. 로그 삭제·다운로드 실패·annotation 권한 오류·실행 진행 중·상한 초과는 `status: "partial"`, `complete: false`와 사유를 반환합니다. 이미 수집한 자료는 유지합니다. 로그 상한 초과에는 `truncated: true`도 표시하고 구조화된 사실로 축약하지 않습니다.

완료된 성공 실행은 `failed_jobs: []`, `evidence: []`를 반환합니다. 실패한 실행인데 실패 job을 얻지 못했다면 빈 성공 결과로 처리하지 않습니다.

| 종료 코드 | 의미 |
|---|---|
| `0` | 자료 수집 완료. CI 실패가 있어도 수집이 완전하면 0 |
| `1` | 인증·API 오류 또는 부분 자료 |
| `2` | 잘못된 옵션·실행 ID·상한 |

## 인증과 관련 기능

Fine-grained 토큰에는 **Actions: read**, annotation 조회에는 **Checks: read**가 필요합니다. 비공개 저장소의 classic 토큰은 `repo` scope가 필요합니다. 서명된 로그 다운로드 URL에는 GitHub 인증 토큰을 전달하지 않습니다. [Actions 공식 문서](https://docs.github.com/en/rest/actions/workflow-jobs#download-job-logs-for-a-workflow-run), [Checks 공식 문서](https://docs.github.com/en/rest/checks/runs#list-check-run-annotations).

이 명령은 CI 대기·재실행·PR merge·코드 수정을 수행하지 않으며 모델을 호출하지 않습니다. 재실행부터 새 회차의 결과 확인까지 처리하려면 [ci rerun](rerun.md)을 사용합니다. [최신 작업 전체 측정](../../benchmarks/github/ci/failures.md)에서 대표 수집 시나리오와 검증 범위를 확인할 수 있습니다.

[공통 안내](../README.md) · [간결한 출력](../README.md#간결한-출력) · [PR 통합 조회](../pr/inspect.md) · [CI 재실행](rerun.md) · [PR 머지](../pr/merge.md)

## 저장된 CI 원문 읽기

발췌에 없는 문맥은 `--read-evidence`와 예상 SHA·run·attempt·job·구간/패턴으로 선택해 읽는다. 원래 작업과 인증을 실행하지 않는 [공통 읽기 전용 계약](evidence.md)을 따른다.
