# `tools github setup`

GitHub의 기존 라벨·활성 이슈 유형을 읽어 prefix별 실제 이름을 `.tools.json`에 저장합니다.
저장소별 정책을 처음 준비하거나 갱신할 때 사용합니다.

## 기본 사용

```sh
tools github setup
tools github setup --dry-run --json
tools github setup --repo OWNER/REPO --config project.tools.json
```

기본 저장 위치는 현재 Git 루트의 `.tools.json`이며 Git 밖에서는 현재 폴더입니다.
GitHub에는 읽기 요청만 보내고 설정 파일을 로컬에 저장합니다.

## 옵션

| 옵션 | 기본값·동작 |
|---|---|
| `--repo OWNER/REPO` | 생략하면 현재 `origin` |
| `--config FILE` | 만들거나 갱신할 설정 파일 |
| `--dry-run` | 조회·계획만 수행하고 파일 저장 생략 |
| `--json` | 계획·사용 가능한 이름·파일 경로 출력 |
| `--refresh` | 지원 prefix의 매핑을 현재 목록으로 재계산 |
| `--set-label PREFIX=NAME` | 기존 라벨 이름을 지정. 반복 가능 |
| `--set-issue-type PREFIX=NAME` | 활성 이슈 유형 이름을 지정. 반복 가능 |
| `--body-language ko\|any` | 기존 값 보존. 새 설정은 `ko` |

## 자동 매핑과 기존 설정

생성 명령과 같은 이름·별칭·namespace 규칙으로 기존 이름을 선택합니다.
예를 들어 `bug`, `enhancement`, `documentation`이 있으면 `fix`, `feat`, `docs`에 대응할 수 있습니다.
매칭되지 않은 prefix는 빈 후보 배열로 저장하고 `notes`에 표시합니다.
라벨 설명을 의미 분석하거나 모델로 분류하지 않습니다.

기본 재실행은 기존 매핑·본문 언어·명시적으로 비운 매핑을 유지하고 빠진 prefix만 채웁니다.
현재 GitHub에 없는 사용자 지정 후보도 보존하며 안내를 표시합니다.
`--refresh`는 지원 prefix의 매핑을 다시 계산합니다. 본문 언어는 유지합니다.
명시적인 `--set-label`·`--set-issue-type`이 자동 계산보다 우선합니다.

```sh
tools github setup --set-label feat=enhancement --set-label fix=bug
tools github setup --set-label 'feat=type: feature' --set-label feat=enhancement
tools github setup --set-issue-type fix=Bug --body-language any
tools github setup --set-label ci=
tools github setup --refresh --dry-run --json
```

동일 prefix를 반복하면 우선순위 후보로 저장합니다. 후보 이름은 현재 GitHub에 있어야 합니다.
빈 값은 해당 매핑을 비활성화하며 같은 prefix의 다른 후보와 함께 지정할 수 없습니다.
공백이 있는 라벨 이름은 셸에서 따옴표로 감쌉니다.

## 결과

| JSON 필드 | 의미 |
|---|---|
| `status` | `saved`: 저장 완료, `planned`: 미리보기 |
| `path` | 대상 설정 파일 경로 |
| `plan.repo` | 조회한 저장소 |
| `plan.config.github` | 저장할 정책 |
| `plan.available_labels` | 명시적 선택에 사용할 기존 라벨 이름 |
| `plan.available_issue_types` | 활성 유형 이름 |
| `plan.notes` | 매칭 누락·기존 후보의 현재 미존재 등 안내 |

저장한 설정은 이후 `issue create`·`pr create`·`context`가 기본 경로 또는 `--config`로 읽습니다.
설정 파일에 인증 토큰이나 인증된 사용자 계정을 저장하지 않습니다.

## 실패 시 확인

- 없는 라벨·비활성 유형·지원하지 않는 prefix를 지정하면 저장 전에 오류를 반환합니다.
- 기존 JSON이 잘못되었거나 지원하지 않는 필드가 있으면 덮어쓰지 않습니다.
- 설정 경로가 디렉터리·심볼릭 링크라면 일반 파일 경로를 선택합니다.
- 저장 대상의 부모 폴더는 미리 있어야 합니다.
- 조회 도중 설정 파일이 바뀌면 새 설정을 보존하고 재실행하도록 알립니다.
- 유형 API가 HTTP 404를 반환하면 사용 불가 안내와 함께 라벨 설정을 계속 처리합니다. 다른 API 오류는 중단합니다.

설정은 현재 이름의 매핑입니다. GitHub 이름이 변경되면 미리보기 후 `--refresh`나 명시적 옵션으로 갱신하세요.

[공통 안내](README.md) · [목록 조회](context.md) · [이슈 생성](issue/create.md)

## 저장 후 확인

실제 저장 파일을 read-back하고 JSON·바이트·기존 권한을 검증한다. `saved_config`의 `saved`, `changed`, `verified`, `sha256`, `mode`, `mode_preserved`가 결과이다. dry-run에는 저장 검증을 붙이지 않는다. 저장 뒤 검증 실패는 경로와 이미 저장된 사실·오류를 유지한 `partial`과 종료 코드 1로 반환한다. 확인 뒤의 타 프로세스 변경까지 보장하지 않는다.
