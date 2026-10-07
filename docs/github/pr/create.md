# `tools github pr create`

원격 head와 base의 변경을 확인해 PR을 만들고 기존 라벨을 적용합니다.
기본 브랜치 규칙과 이슈 번호 연결을 자동으로 처리합니다.

## 기본 사용

현재 브랜치에서 변경 내용을 먼저 커밋하고 원격에 푸시합니다.
변경 내용과 실제 검증 결과를 UTF-8 Markdown `pr.md`에 준비합니다.

```sh
tools github pr create --prefix feat --title "add login" --body-file pr.md --json
tools github pr create --prefix feat --title "add login" --body-file pr.md --dry-run --json
```

도구가 커밋·변경 내용 푸시·테스트·PR 머지를 실행하지는 않습니다.
생성 전에 실제 GitHub 라벨·Markdown 템플릿을 조회합니다.

## 옵션

| 옵션 | 동작 |
|---|---|
| `--prefix PREFIX` | 지원 prefix. 입력 제목/JSON과 충돌하면 오류 |
| `--title TITLE` | 영어·숫자·ASCII 문구. prefix를 추가하고 문구 대소문자 보존 |
| `--body-file FILE` | 직접 작성한 Markdown. `-`이면 표준입력 |
| `--file FILE` | JSON 명세. `-`이면 표준입력 |
| `--repo OWNER/REPO` | 기본은 현재 `origin` |
| `--config FILE` | 프로젝트의 본문 언어·라벨 정책 |
| `--dry-run` | 원격 비교·검증·계획만 수행 |
| `--json` | 생성·부분 실패 결과 출력 |

`--file`과 `--title`/`--body-file` 입력은 함께 사용할 수 없습니다.
`base`, `head`, `issue`, `draft`, `labels`, `verification`은 JSON 필드이며 별도 CLI 플래그가 아닙니다.
이 명령에는 `--draft`나 `--no-checkout` 옵션이 없습니다.

## JSON으로 이슈·브랜치·초안 지정

```json
{
  "prefix": "feat",
  "title": "add login",
  "summary": "로그인 기능을 추가합니다.",
  "changes": ["로그인 요청과 화면을 구현합니다."],
  "verification": [
    {
      "command": "go test ./...",
      "result": "not-run",
      "details": "형식 예시입니다. 실제 검증 결과로 바꾸세요."
    }
  ],
  "issue": 123,
  "head": "123-feat-add-login",
  "draft": true
}
```

```sh
tools github pr create --file pr.json --json
```

| JSON 필드 | 기본값·동작 |
|---|---|
| `prefix`, `title` | PR 제목 구성 |
| `body` | 직접 작성한 Markdown |
| `summary`, `changes`, `verification` | 구조화된 본문 구성 |
| `base` | 저장소 기본 브랜치 |
| `head` | 현재 로컬 브랜치. 명시하면 Git 밖에서도 사용 가능 |
| `issue` | 양의 이슈 번호. 생략 시 브랜치 이름에서 추론 |
| `draft` | 기본 `false`. `true`이면 초안 PR |
| `labels` | 기존 이름 명시. `[]`이면 자동 라벨 선택 생략 |

`body`와 구조화된 본문 필드는 함께 사용할 수 없습니다.
`issue_type`·`acceptance`는 이슈용 필드이므로 PR 입력에 사용하지 않습니다.
기본 본문 언어는 `ko`이며 다른 언어는 설정의 `body_language: "any"`를 사용합니다.

## 원격 검증과 이슈 연결

브랜치 이름은 제목의 prefix와 맞아야 합니다.
이슈가 있으면 `<issue-number>-<type>-<slug>`, 없으면 `<type>/<slug>` 형식을 사용합니다.
예를 들어 `123-feat-add-login` 또는 `feat/add-login`입니다.
fork head는 JSON `head`의 `owner:branch` 형식으로 지정합니다.

`issue`를 생략하고 브랜치가 번호로 시작하면 해당 번호를 추론해 본문에 `Closes #123`을 추가합니다.
명시한 이슈도 동일한 연결 문구를 사용합니다. 번호가 실제 이슈인지 조회하며 PR 번호는 거부합니다.
이슈가 실제로 종료되는 시점은 GitHub의 PR 연결·머지 규칙을 따릅니다.

원격 head에 base보다 앞선 커밋이 있어야 합니다.
로컬에만 변경이 있거나 원격 head가 없으면 먼저 커밋·푸시해야 합니다.
기존 라벨만 선택하며 PR 생성 후 라벨을 별도 적용합니다. 이슈 유형·담당자를 PR에 자동 지정하지 않습니다.
현재 Markdown 템플릿을 적용하며 YAML issue form을 해석하지 않습니다.

## 검증 기록

`verification`은 작성자가 제공한 기록이며 도구가 실행하지 않습니다.
`result`는 `passed`, `failed`, `not-run` 중 하나입니다.
`failed`·`not-run`에는 `details`가 필요합니다.
구조화된 본문에서 기록을 생략하면 미실행 안내를 표시합니다.
직접 `body`를 작성할 때는 변경 내용과 실제 검증 결과를 직접 포함합니다.

## 결과와 복구

미리보기는 `status: planned`와 `plan`을 반환합니다.
`plan.payload`에서 최종 제목·본문·base·head·draft를 확인할 수 있습니다.
생성 성공은 `status: created`, `kind: pr`, `number`, `url`, `selection`을 반환합니다.
라벨 적용 실패 등은 `status: partial`과 기존 PR URL을 유지합니다.

부분 실패 시 그 PR을 수정하며 다시 생성하지 않습니다.
생성 응답을 잃었으면 GitHub에 동일 head의 PR이 있는지 먼저 확인합니다.
원격 쓰기는 자동 재시도하지 않습니다.

[공통 안내](../README.md) · [이슈 생성](../issue/create.md) · [브랜치 정리](../branch/cleanup.md) · [JSON 예제](../../../examples/github/pr.json)
