# `tools github context`

저장소의 기존 라벨·이슈 유형과 현재 적용 정책을 확인합니다.
목록을 직접 보거나 설정 후보를 고를 때 사용하는 읽기 전용 명령입니다.

## 기본 사용

```sh
tools github context
tools github context --json
tools github context --repo OWNER/REPO --config project.tools.json --json
```

## 옵션

| 옵션 | 기본값·동작 |
|---|---|
| `--repo OWNER/REPO` | 생략하면 현재 `origin` |
| `--config FILE` | 기본 정책과 병합할 설정 파일 |
| `--json` | 저장소·라벨·유형·정책을 구조화해 출력 |

기본 설정 위치는 Git 루트의 `.tools.json`이며 Git 밖에서는 현재 폴더입니다.
설정 파일이 없으면 기본 정책을 사용합니다. 명시한 `--config` 파일이 없으면 오류입니다.

## 출력

텍스트 출력은 저장소, 기본 브랜치, 라벨 이름과 활성 유형 이름을 표시합니다.
다음은 형식 예시이며 실제 이름은 조회한 저장소에 따라 달라집니다.

```text
repo: OWNER/REPO
default branch: main
labels: bug; enhancement; documentation;
issue types: Bug; Feature; Task;
```

| JSON 필드 | 의미 |
|---|---|
| `status` | 정상 조회 시 `ok` |
| `catalog.repository` | 저장소 이름·기본 브랜치·응답에 포함된 권한 등 |
| `catalog.labels` | 기존 라벨 정보 |
| `catalog.issue_types` | API에서 반환한 유형 정보. `is_enabled`도 확인 |
| `catalog.notes` | 유형 API 사용 불가 등 안내 |
| `policy` | 기본값과 설정을 병합한 정책 |

JSON에는 비활성 유형 정보도 포함될 수 있습니다. 자동 선택에는 활성 유형만 사용합니다.
유형 API의 HTTP 404는 사용 불가로 표시하며 라벨 조회는 유지합니다.
그 외 API 오류나 잘못된 설정은 오류로 반환합니다.

## 다른 명령과의 관계

`context`는 설정 파일을 저장하거나 GitHub 항목을 만들지 않습니다.
현재 이름을 설정에 저장하려면 [setup](setup.md)을 사용합니다.
`issue create`·`pr create`가 필요한 조회를 직접 수행하므로 생성 전에 매번 실행할 필요는 없습니다.
Markdown 템플릿과 인증된 사용자 정보는 이 명령의 출력 대상이 아닙니다.

[공통 안내](README.md) · [설정 저장](setup.md) · [이슈 생성](issue/create.md)
