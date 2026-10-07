# GitHub 명령어 안내

`tools github`의 명령별 사용법입니다. 예시는 한국어 본문과 소문자 영어 제목을 사용합니다.

## 명령어 목록

| 명령 | 하는 일 | 변경 범위 |
|---|---|---|
| [setup](setup.md) | 기존 라벨·유형을 가져와 프로젝트 설정 저장 | 로컬 설정 파일 |
| [context](context.md) | 저장소 라벨·유형·적용 정책 조회 | 읽기 전용 |
| [issue create](issue/create.md) | 이슈 생성·담당자 지정·Development 브랜치 연결 | GitHub 및 선택적 로컬 checkout |
| [issue branch](issue/branch.md) | 기존 이슈의 연결 브랜치 생성·재개 | GitHub 및 선택적 로컬 checkout |
| [pr create](pr/create.md) | 원격 변경을 확인하고 PR 생성·라벨 적용 | GitHub |
| [branch cleanup](branch/cleanup.md) | 종료된 작업의 브랜치 판별·정리 | 미리보기 또는 로컬·원격 참조 삭제 |

## 기본 흐름

```sh
tools github setup
tools github issue create --prefix feat --title "add login" --body-file issue.md --json
# 구현·검증 후 Git으로 커밋·푸시
tools github pr create --prefix feat --title "add login" --body-file pr.md --json
# PR/이슈 종료 후 다른 브랜치로 이동해 정리 대상 확인
tools github branch cleanup --json
```

`setup`은 프로젝트 설정을 처음 저장하거나 갱신할 때 사용합니다.
생성 명령이 필요한 GitHub 목록을 직접 조회하므로 매번 `context`나 `setup`을 먼저 실행할 필요는 없습니다.

## 설치와 인증

저장소 루트에서 빌드한 바이너리를 PATH에 둡니다.

```sh
go build -o tools ./cmd/tools
# Windows용 파일 이름
go build -o tools.exe ./cmd/tools
```

PowerShell에서 현재 폴더의 바이너리를 실행할 때는 `./tools.exe`를 사용합니다.
빌드에는 Go 1.26 이상이 필요하며 실행 시 Go 런타임은 필요하지 않습니다.

GitHub API 인증은 `GH_TOKEN` → `GITHUB_TOKEN` → 기존 `gh auth token` 순으로 확인합니다.
환경변수에 토큰을 제공하면 `gh` 설치는 필수가 아닙니다.
GitHub CLI로 인증하려면 `gh auth login`을 사용합니다.
인증 토큰은 프로젝트 설정이나 명령 결과에 저장하지 않습니다.
현재 지원 호스트는 `github.com`입니다.

`--repo OWNER/REPO`를 생략하면 Git의 `origin`에서 저장소를 찾습니다.
Git 밖에서는 명시적인 `--repo`를 사용합니다.
로컬 checkout과 브랜치 정리에는 Git이 필요합니다.
원격 브랜치 삭제는 Git에 설정된 전송 인증도 필요합니다.

## 프로젝트 정책

기본 설정 경로는 Git 루트의 `.tools.json`이며, Git 밖에서는 현재 폴더입니다.
`--config FILE`로 다른 파일을 선택합니다. `branch cleanup`은 자체 옵션을 사용하며 `--config`를 받지 않습니다.

```json
{
  "github": {
    "body_language": "any",
    "label_map": {"feat": ["type: feature", "enhancement"]},
    "issue_type_map": {"feat": ["Feature", "Task"]}
  }
}
```

기본 본문 언어 검사는 `ko`입니다. 다른 언어의 본문은 `body_language: "any"`로 허용합니다.
배열은 우선순위 후보이며 자동 매핑은 일치하는 라벨 하나·이슈 유형 하나를 선택합니다.
기본 후보는 코드에 있고 설정한 prefix만 덮어씁니다. 빈 배열은 해당 매핑을 끕니다.
라벨은 먼저 이름을 직접 대조하고, 직접 일치가 없으면 `type:`, `type/`, `kind:`, `kind/`, `category:` namespace도 대조합니다.
이슈 유형은 활성화된 기존 이름과 대조합니다. 이름 비교는 대소문자를 구분하지 않습니다.
GitHub 라벨·유형을 새로 생성하지 않습니다.
자연어 `AGENTS.md`를 정책으로 해석하지 않습니다.

지원 prefix:

```text
feat, fix, refactor, docs, ci, test, chore, build, perf, style, revert
```

## 출력과 오류

`--json`은 자동화에서 사용할 구조화된 결과를 출력합니다.
`--help`와 `-h`는 인증 없이 도움말을 표시합니다.
각 명령 문서의 결과 필드를 확인하세요.

| 종료 코드 | 의미 |
|---|---|
| `0` | 명령 완료 또는 정상적인 미리보기 |
| `1` | 인증·API·Git·설정 저장 등 실행 오류 또는 생성 후 일부 작업 실패 |
| `2` | 잘못된 옵션·입력·설정 읽기/검증·매핑 |

생성 결과가 `partial`이면 이미 생성된 URL을 확인하고 해당 항목에서 이어갑니다.
응답을 잃었다면 GitHub 상태를 먼저 확인합니다. 생성 요청은 자동 재시도하지 않습니다.

## 관련 문서

- [에이전트용 빠른 사용법](../agent-usage.md)
- [설정 예제](../../examples/github/tools-config.json)
- [명령어별 토큰 측정](../../README.md#token-usage-measurements)
- [한국어 프로젝트 안내](../README.ko.md)
