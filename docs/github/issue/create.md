# `tools github issue create`

이슈를 생성하고 인증된 사용자를 담당자(`@me`)로 지정합니다.
기본적으로 이슈 번호 기반 브랜치를 만들고 Development에 연결한 뒤 로컬에서 전환합니다.

## 기본 사용

한국어 본문을 UTF-8 Markdown 파일 `issue.md`에 준비합니다.

```sh
tools github issue create --prefix feat --title "add login" --body-file issue.md --json
tools github issue create --prefix feat --title "add login" --body-file issue.md --dry-run --json
```

첫 명령은 쓰기 작업을 수행합니다. 두 번째 명령은 조회·검증·계획만 수행합니다.
제목은 `feat: add login`이 됩니다. 이미 유효한 prefix가 포함된 제목도 사용할 수 있습니다.

## 옵션

| 옵션 | 동작 |
|---|---|
| `--prefix PREFIX` | 지원 prefix 선택. JSON 또는 제목의 prefix와 충돌하면 오류 |
| `--title TITLE` | 영어·숫자·ASCII 기호 제목. 문구 대소문자 보존 |
| `--body-file FILE` | UTF-8 Markdown 본문. `-`이면 표준입력 |
| `--file FILE` | JSON 명세. `-`이면 표준입력 |
| `--repo OWNER/REPO` | 기본은 현재 `origin` |
| `--config FILE` | 기본은 Git 루트의 `.tools.json` |
| `--dry-run` | 계획 출력. GitHub 쓰기·fetch·checkout 생략 |
| `--json` | 생성·부분 실패 결과를 구조화해 출력 |
| `--no-checkout` | 연결된 원격 브랜치는 생성하고 로컬 전환 생략 |
| `--no-branch` | 이슈만 생성하고 브랜치 생성·연결 생략 |

`--file`과 `--title`/`--body-file` 입력은 함께 사용할 수 없습니다.
JSON 입력과 같은 값의 `--prefix`는 함께 사용할 수 있습니다.

## JSON 입력

```json
{
  "prefix": "feat",
  "title": "add login",
  "summary": "로그인 기능을 추가합니다.",
  "changes": ["로그인 요청과 화면을 구현합니다."],
  "acceptance": ["정상 계정으로 로그인할 수 있습니다."]
}
```

```sh
tools github issue create --file issue.json --json
tools github issue create --file - --dry-run --json
```

두 번째 명령은 표준입력에서 JSON 객체 하나를 읽습니다.
알 수 없는 필드나 추가 JSON 객체는 오류입니다.

| JSON 필드 | 동작 |
|---|---|
| `prefix`, `title` | prefix를 붙여 제목 구성 |
| `body` | 직접 작성한 Markdown 본문 |
| `summary`, `changes`, `acceptance` | 구조화된 본문 구성 |
| `verification` | 선택적 검증 기록. 명령 실행은 하지 않음 |
| `labels` | 기존 라벨 이름을 명시. `[]`이면 자동 라벨 선택 생략 |
| `issue_type` | 활성 기존 유형을 명시 |

`body`는 `summary`·`changes`·`acceptance`·`verification`과 함께 사용할 수 없습니다.
담당자를 바꾸는 입력은 없습니다.
`issue`, `base`, `head`, `draft`는 PR용 필드이므로 이슈 입력에 지정하지 않습니다.

## 라벨·유형·본문

기존 라벨·활성 유형과 프로젝트 정책을 조회해 선택합니다. 새 라벨·유형을 만들지 않습니다.
명시한 이름이 없으면 생성 전에 오류입니다. 자동 매핑이 없으면 안내와 함께 해당 메타데이터를 생략합니다.
이슈 유형을 설정하려면 저장소 push 권한이 필요합니다.
유형 선택을 끄려면 설정에서 해당 prefix의 `issue_type_map`을 빈 배열로 지정합니다.

기본 본문 검사는 `ko`로, 작성한 문구에 한국어가 포함되어야 합니다.
다른 언어는 [setup](../setup.md)의 `--body-language any` 또는 직접 설정으로 허용합니다.
기존 Markdown 템플릿을 적용하되 작성한 내용도 보존합니다. YAML issue form은 해석하지 않습니다.

## 연결 브랜치

번호가 123이면 예제의 브랜치는 `123-feat-add-login`입니다.
저장소 기본 브랜치의 원격 SHA에서 생성하고 이슈의 Development에 연결합니다.
slug는 제목에서 prefix를 제외한 문구를 소문자화하고 특수기호·공백을 `-`로 정리합니다. 최대 180자입니다.

현재 Git worktree의 `origin`이 대상 저장소와 일치하면 원격 브랜치를 fetch하고 로컬에서 전환합니다.
Git 밖이거나 origin이 다르면 안내와 함께 원격 브랜치만 생성합니다.
원격 전용 작업은 `--repo OWNER/REPO --no-checkout`을 사용합니다.
기존 로컬 브랜치·작업 파일을 강제로 덮어쓰지 않습니다.
커밋·변경 내용 푸시는 별도로 수행합니다.

## 결과와 복구

미리보기 결과는 `status: planned`와 `plan`입니다. 브랜치 이름의 번호는 `<issue-number>`로 표시됩니다.
생성 결과의 주요 필드:

| 필드 | 의미 |
|---|---|
| `status` | `created` 또는 생성 이후 일부 실패인 `partial` |
| `number`, `url` | 생성된 이슈 식별자 |
| `selection` | 선택한 라벨·유형·선택 이유 |
| `suggested_branch` | 번호 기반 이름 제안. 이 필드만으로 실제 생성 완료를 뜻하지 않음 |
| `branch` | 실제 확인된 브랜치 이름·URL·연결·checkout 상태 |
| `notes`, `error` | 생략·부분 실패·복구 안내 |

이슈 생성 후 브랜치·checkout 단계가 실패했다면 이슈를 다시 만들지 않습니다.

```sh
tools github issue branch --number 123 --json
```

같은 이름의 원격 브랜치가 해당 이슈에 연결되어 있으면 재사용합니다. 연결되지 않은 동명 브랜치는 변경하지 않습니다.
메타데이터 적용 실패는 기존 이슈 URL에서 수정합니다.
생성 응답을 잃었으면 GitHub에 이슈가 있는지 확인한 뒤 다음 작업을 결정합니다.

[공통 안내](../README.md) · [브랜치 재개](branch.md) · [PR 생성](../pr/create.md) · [JSON 예제](../../../examples/github/issue.json)
