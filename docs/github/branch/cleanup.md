# `tools github branch cleanup`

머지·클로즈된 PR 또는 닫힌 이슈에 해당하는 로컬·원격 브랜치와 연결된 워크트리를 정리합니다.
기본 실행은 읽기 전용 미리보기이며 `--apply`가 실제 삭제를 수행합니다.

## 기본 사용

명령을 실행 중인 워크트리는 보존됩니다. 완료된 워크트리를 정리하려면 주 작업 폴더 등 다른 워크트리에서 실행합니다.

```sh
tools github branch cleanup --json
tools github branch cleanup --include-skipped --json
tools github branch cleanup --apply --json
```

미리보기에서 대상과 사유를 확인합니다. `--apply`는 머지 없이 닫힌 원격 작업도 삭제합니다.

## 옵션

| 옵션 | 기본값·동작 |
|---|---|
| `--repo OWNER/REPO` | 기본은 선택한 Git 원격의 저장소 |
| `--remote NAME` | 기본 `origin` |
| `--scope both\|local\|remote` | 기본 `both`. 워크트리 제거는 `both`·`local`에 포함 |
| `--branch NAME` | 해당 원격 브랜치와 이를 추적하는 로컬 별칭만 대상으로 계획·적용. 정확한 이름, glob 불가 |
| `--protect PATTERNS` | 추가 보호 이름/glob. 쉼표로 구분 |
| `--include-skipped` | 제외된 대상과 사유까지 출력 |
| `--apply` | 적합한 워크트리 제거 및 참조 실제 삭제 |
| `--dry-run` | 명시적 미리보기. `--apply`와 함께 사용 불가 |
| `--json` | 계획·항목별 결과·건수 출력 |

```sh
tools github branch cleanup --scope local --apply --json
tools github branch cleanup --remote upstream --scope remote --json
tools github branch cleanup --protect 'develop,release/*' --json
tools github branch cleanup --branch 123-fix-login --apply --json
```

`--config`는 지원하지 않습니다. 보호 설정은 `--protect`로 지정합니다.
선택한 원격의 fetch URL과 단일 push URL이 대상 `github.com` 저장소와 일치해야 합니다.
GitHub API 인증과 별도로 Git 원격 삭제가 가능한 전송 인증·권한이 필요합니다.

## 대상 판별

같은 저장소의 최신 PR이 머지·클로즈되었거나 연결된 이슈가 닫히면 후보입니다.
이슈 연결은 Development 참조 또는 `<issue-number>-<type>-<slug>` 이름으로 찾습니다.
다른 fork의 동명 브랜치 PR은 해당 저장소 브랜치의 근거로 쓰지 않습니다.

`--branch`는 전체 원격 브랜치 목록과 모든 닫힌 이슈의 Development 연결을 조회하지 않습니다. 지정한 브랜치와 해당 head의 PR만 조회하고, 번호 기반 이슈가 필요하면 그 이슈를 확인합니다. PR·번호 규칙 없이 Development 연결만 있는 브랜치는 전체 계획에서 판별합니다. 워크트리와 로컬 upstream 정보는 지정한 브랜치의 별칭·체크아웃 보호에 필요하므로 확인합니다. [pr merge](../pr/merge.md)는 이 범위 지정 계획을 사용합니다.

다음 대상은 보존합니다.

- 기본 브랜치, `main`, `master`, GitHub 보호 브랜치와 `--protect`에 일치하는 이름.
- 주 작업 폴더와 실행 중인 워크트리, 잠겼거나 사용할 수 없는 워크트리와 연결된 브랜치.
- 수정 파일·미추적 파일·무시된 파일이 있는 워크트리와 연결된 브랜치. 강제로 제거하지 않습니다.
- 열린 PR이 있는 브랜치. 이슈가 닫혀 있어도 열린 PR이 우선합니다.
- 원격 tip이 최신 닫힌 PR의 head와 다른 브랜치. 해당 로컬 대상도 제외합니다.
- 다른 원격을 upstream으로 사용하는 로컬 브랜치.
- 게시된 커밋임을 확인할 수 없는 로컬 변경.

로컬 tip은 원격 tip·PR head·기존 추적 참조·원격 기본 브랜치 참조 등의 조상인지 확인합니다.
squash 머지처럼 기본 브랜치의 조상이 아니어도 게시된 PR head로 판별할 수 있습니다.
필요한 커밋 객체가 로컬에 없으면 자동 fetch 대신 해당 로컬 브랜치를 보존합니다.
정리 가능한 로컬 브랜치의 깨끗한 연결 워크트리는 함께 제거합니다. 자동 생성 경로 밖에 있는 연결 워크트리도 같은 기준을 적용합니다.
`--scope remote`는 워크트리를 제거하지 않으며 체크아웃된 브랜치와 추적하는 원격 브랜치를 보존합니다.

원격에서 이미 사라진 종료 브랜치의 오래된 remote-tracking 참조도 정리합니다.
이는 `local` 범위에 포함되며, 단순히 오래되었다는 이유만으로 모든 추적 참조를 지우지는 않습니다.
Development의 원격 참조가 사라진 경우 임의 이름의 로컬 브랜치는 이슈 연결을 복원하지 못할 수 있습니다.
PR 연결이나 번호 기반 이름이 있어야 해당 종료 근거를 찾을 수 있습니다.

## 삭제와 변경 보호

적용 전에 GitHub 상태·보호 설정을 다시 조회하고 각 삭제 직전에 worktree를 확인합니다.
워크트리는 경로·브랜치·HEAD·파일 상태를 다시 확인한 뒤 `git worktree remove`로 제거하며 강제 옵션을 사용하지 않습니다.
순서는 워크트리 제거 → 원격 참조 삭제 → 로컬 참조 삭제입니다. `local` 범위에서는 원격 삭제를 생략합니다.
기준 SHA가 변했으면 로컬·원격 삭제를 거부합니다.
워크트리 제거 실패 시 연결된 로컬·원격 브랜치 삭제도 중단합니다.
원격 삭제 실패 시 대응 로컬 브랜치를 보존합니다.
앞서 제거된 워크트리는 복구하지 않으며 남은 브랜치로 다시 만들 수 있습니다.

GitHub 상태와 worktree 변경을 참조 삭제와 원자적으로 잠글 수는 없습니다.
다른 Git 쓰기 작업과 동시에 실행하지 않습니다.

## JSON 결과

| 미리보기 필드 | 의미 |
|---|---|
| `status: planned` | 읽기 전용 계획 |
| `plan.targets` | 기본은 후보만. `--include-skipped`로 제외 대상 포함 |
| `summary.eligible`, `summary.kept` | 후보·보존 대상 건수 |

| 적용 결과 필드 | 의미 |
|---|---|
| `status` | `completed` 또는 항목별 오류가 있는 `partial` |
| `actions` | 항목별 삭제·오류·새롭게 제외된 후보. 기존 제외 대상은 옵션으로 포함 |
| `actions[].scope` | `worktree`, `local`, `remote`, `tracking` |
| `plan.targets[].worktree_path`, `actions[].worktree_path` | 해당 워크트리 경로 |
| `actions[].status` | `deleted`, `skipped`, `error` |
| `actions[].reasons`, `skip` | 종료 근거·보존 이유 |
| `actions[].error`, `warning` | 삭제 오류 또는 삭제 후 설정 정리 안내 |
| `summary.deleted`, `skipped`, `error` | 전체 대상의 결과 건수 |

`completed`에도 상태 변경 때문에 `skipped`된 대상이 있을 수 있으므로 `actions`를 확인합니다.
건수는 워크트리·참조 단위입니다. 같은 이름의 워크트리·로컬·원격은 각각 세며, 원격 삭제에 따른 자동 추적 참조 제거는 별도 작업으로 세지 않습니다.

## 실패와 측정 근거

일부 삭제 실패는 종료 코드 `1`과 `partial`을 반환합니다. 성공한 삭제는 되돌리지 않습니다.
권한·전송 인증·변경된 SHA를 확인하고 새 미리보기로 현재 상태를 다시 판단합니다.
기존 브랜치 설정 정리에만 실패한 경우 `warning`에 표시되며 참조 삭제는 완료된 상태일 수 있습니다.

이 명령의 실제 임시 Git 참조 삭제·보존 작업은 [명령별 토큰 실측](../../../README.md#token-usage-measurements)에서 근거 문서와 함께 확인할 수 있습니다.
실제 GitHub 삭제·금액 절감을 측정한 결과는 아니며, 미리보기만의 토큰 수치도 따로 측정하지 않았습니다.

[공통 안내](../README.md) · [이슈 브랜치](../issue/branch.md) · [PR 생성](../pr/create.md)
