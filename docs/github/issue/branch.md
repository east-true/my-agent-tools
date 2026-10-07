# `tools github issue branch`

이미 존재하는 이슈의 번호와 제목에서 Development 브랜치를 만들거나 재사용합니다.
이슈를 다시 생성하지 않고, `issue create` 이후 브랜치 단계의 실패를 이어갈 때도 사용합니다.

## 기본 사용

```sh
tools github issue branch --number 123 --json
tools github issue branch --number 123 --dry-run --json
tools github issue branch --repo OWNER/REPO --number 123 --no-checkout --json
```

## 옵션

| 옵션 | 기본값·동작 |
|---|---|
| `--number NUMBER` | 필수인 양의 이슈 번호 |
| `--repo OWNER/REPO` | 기본은 현재 `origin` |
| `--config FILE` | 프로젝트 설정 읽기·검증. 브랜치는 이슈 제목에서 도출 |
| `--no-checkout` | 원격 브랜치 생성·연결만 수행 |
| `--dry-run` | 기본 브랜치 조회와 계획만 수행 |
| `--json` | 결과를 구조화해 출력 |

`--prefix`, `--title`, `--file`, `--no-branch` 옵션은 이 명령에 없습니다.

## 이름과 생성 기준

이슈 제목이 `fix: handle login error`이고 번호가 123이면 이름은 `123-fix-handle-login-error`입니다.
제목은 `<type>: <summary>` 형식이며 소문자 type과 영어·숫자·ASCII 문구에서 slug를 도출할 수 있어야 합니다.
현재 이슈 제목을 기준으로 이름을 계산하므로 제목을 변경했다면 새 이름이 도출될 수 있습니다.
슬러그는 소문자 영어·숫자와 `-`로 정리하고 최대 180자로 제한합니다.
시작점은 로컬 HEAD가 아닌 저장소 기본 브랜치의 원격 SHA입니다.

입력 번호가 PR을 가리키면 오류입니다.
이슈 제목·번호·URL·node ID나 기본 브랜치 SHA가 불완전하면 진행하지 않습니다.
이 명령은 이슈의 제목·본문·라벨·유형·담당자를 수정하지 않습니다.

## 기존 브랜치와 로컬 전환

동명 원격 브랜치가 이 이슈의 Development에 연결되어 있으면 `reused: true`로 재사용합니다.
동명 브랜치가 연결되어 있지 않으면 오류로 중단합니다. 강제 덮어쓰기나 임의 연결은 하지 않습니다.
원격에 없으면 연결된 브랜치를 생성합니다.

Git worktree가 있고 origin이 대상 저장소와 같으면 fetch 후 checkout합니다.
기존 로컬 동명 브랜치는 예상 `origin/<branch>`를 upstream으로 가져야 합니다.
upstream이 다르거나 Git 전환이 작업 파일과 충돌하면 로컬 전환에 실패합니다.
origin이 다르거나 Git 밖이면 원격만 생성하며 안내를 남깁니다.
`--no-checkout`으로 로컬 동작을 명시적으로 생략할 수 있습니다.

## 결과

| 상태·필드 | 의미 |
|---|---|
| `status: planned` | 쓰기 없는 계획. `plan`과 `issue_url` 포함 |
| `status: created` | 브랜치 작업 완료. 기존 브랜치 재사용도 이 상태를 사용 |
| `status: partial` | 원격 생성·연결 또는 로컬 전환 중 일부 실패 |
| `kind: branch` | 이슈 생성이 아닌 브랜치 작업 |
| `number`, `url` | 기존 이슈 번호와 URL |
| `branch.linked` | Development 연결 확인 여부 |
| `branch.checked_out` | 로컬 전환 완료 여부 |
| `branch.reused` | 기존 연결 브랜치 재사용 여부. 해당할 때 표시 |

결과의 `url`은 이슈 URL이며, 브랜치 URL은 `branch.url`입니다.
미리보기는 쓰기 없이 계획만 만들므로 기존 동명 브랜치의 재사용 가능 여부를 완료 상태로 보장하지 않습니다.

## 실패 후 재개

원격 브랜치는 생성되었으나 로컬 전환에 실패했다면 작업 파일·upstream을 확인한 후 같은 명령을 다시 실행합니다.
이미 연결된 브랜치를 확인하고 재사용하므로 새 이슈를 만들 필요가 없습니다.
생성 응답을 잃었거나 연결되지 않은 동명 브랜치가 있으면 원격 상태부터 확인합니다.
기본 브랜치가 없는 빈 저장소에서는 먼저 기본 커밋을 준비해야 합니다.

[공통 안내](../README.md) · [이슈 생성](create.md) · [PR 생성](../pr/create.md)
