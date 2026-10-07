# my-agent-tools

**코딩 에이전트와 개발자를 위한, 한 명령으로 처리하는 GitHub 워크플로.**

[English](../README.md) · [명령어](github/README.md) · [벤치마크](benchmarks/README.md) · [기여 안내](../CONTRIBUTING.md)

`tools`는 반복되는 GitHub 작업을 처리하는 크로스 플랫폼 CLI입니다.

- **이슈 → 브랜치:** 기존 라벨·유형을 선택하고, 담당자 `@me` 지정과 Development 브랜치 연결을 처리합니다.
- **PR 생성:** 현재 브랜치에서 연결 이슈를 찾고 저장소 정책을 적용합니다.
- **브랜치 정리:** 종료된 작업을 판별하고, 미리보기 후 대상 로컬·원격 브랜치를 삭제합니다.

```sh
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json
```

이슈 번호가 123이면 `fix: handle duplicate requests`와 `123-fix-handle-duplicate-requests` 브랜치를 생성합니다. 현재 origin이 대상 저장소와 일치하면 로컬 브랜치도 전환합니다. 프로젝트별 라벨·유형 매핑은 `.tools.json`에서 설정합니다.

## 토큰 사용량 실측

브랜치 정리는 직접 `gh + git` 처리보다 총 토큰을 56.1% 줄였습니다. 14개 브랜치 상황을 고정한 실험 결과이며, 이슈 계획의 차이는 작았습니다.

| 명령 | 직접 `gh` / `gh + git` | `tools` | 총 토큰 차이 |
|---|---:|---:|---:|
| [`github context`](github/context.md) | — | — | 개별 미측정 |
| [`github setup`](github/setup.md) | — | — | 미측정 |
| [`github issue create --dry-run`](benchmarks/github/issue-create.md) | 46,630 | 46,279 | −0.75% |
| [`github issue create`](github/issue/create.md) | — | — | 미측정 |
| [`github issue branch`](github/issue/branch.md) | — | — | 개별 미측정 |
| [`github pr create`](github/pr/create.md) | — | — | 미측정 |
| [`github branch cleanup`](github/branch/cleanup.md) | — | — | 미리보기 개별 미측정 |
| [`github branch cleanup --apply`](benchmarks/github/branch-cleanup.md) | 70,552 | 31,006 | −56.1% |

`gpt-6.1-sol`, high 추론, 방식별 새 세션 3회의 평균입니다. 지시문·캐시 입력·출력을 포함한 에이전트 작업 전체를 측정했고, CLI 자체는 모델을 호출하지 않습니다. 브랜치 정리는 로컬 실험 API와 실제 임시 Git 저장소를 사용했으며, 캐시 제외 입력은 43.3% 감소하고 비교한 6회 모두 삭제·보존 결과가 기준과 일치했습니다. 캐시를 통제하지 않아 금액 절감이나 다른 명령의 효과를 입증한 수치는 아닙니다. [원본·한계·재현 방법](benchmarks/README.md).

## 설치

소스 저장소에서 **Go 1.26 이상**으로 빌드합니다.

```sh
go build -o tools ./cmd/tools
```

Windows는 `go build -o tools.exe ./cmd/tools`를 사용합니다. 바이너리 디렉터리를 PATH에 추가하세요. Linux·macOS·Windows에서 실행하며 Go 런타임은 필요하지 않습니다. 로컬 브랜치 작업에는 Git이 필요합니다.

GitHub 인증은 `GH_TOKEN` → `GITHUB_TOKEN` → 기존 `gh auth login` 순으로 사용합니다. 환경변수로 토큰을 제공하면 `gh` 설치는 선택 사항입니다. 현재 지원 호스트는 `github.com`입니다.

## 빠른 시작

대상 프로젝트의 Git 작업 디렉터리에서 실행합니다. 이슈·PR 설명을 UTF-8 Markdown 파일 `issue.md`, `pr.md`로 준비하세요. 본문 언어의 기본값은 한국어입니다.

```sh
# 기존 GitHub 라벨·유형을 가져와 프로젝트 설정 저장
tools github setup

# 이슈 생성, 담당자 지정, 연결 브랜치 생성·전환
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json

# 구현·검증 후 Git으로 커밋·푸시하고 PR 생성
tools github pr create --prefix fix --title "handle duplicate requests" --body-file pr.md --json
```

생성 명령이 필요한 목록을 조회하므로 `context`는 선택 사항입니다. 생성 계획만 확인하려면 `--dry-run --json`, Git 밖에서는 `--repo OWNER/REPO`를 추가합니다. 다른 언어는 `tools github setup --body-language any`로 허용하고, 라벨 지정 등은 [setup 문서](github/setup.md)를 참고하세요.

PR이나 이슈 종료 후 다른 브랜치로 이동해 정리 대상을 확인합니다.

```sh
tools github branch cleanup --json
# 후보 확인 후 실제 삭제
tools github branch cleanup --apply --json
```

보호 브랜치, worktree에서 사용 중인 브랜치, 열린 PR 작업, 로컬 미게시 커밋은 보존합니다. 머지 없이 닫힌 원격 작업도 삭제 대상이므로 미리보기를 확인하세요. [정리 조건과 옵션](github/branch/cleanup.md).

## 문서

- [명령어 안내](github/README.md) — 명령 6개, 옵션, 설정, 실패 복구.
- [에이전트용 빠른 사용법](agent-usage.md) — 코딩 에이전트에 전달할 짧은 작업 흐름.
- [입력·설정 예제](../examples/github) — 이슈·PR JSON, Markdown, `.tools.json`.
- [벤치마크](benchmarks/README.md) — 명령별 측정, 원본 자료, 실험 워크플로.
- [개발·개인정보 검사](development.md) · [CI 보고서 실험](ci-reports.md).

지원 prefix와 옵션은 `tools github --help` 또는 각 명령의 `--help`에서 확인합니다.

## 기여

다른 프로젝트의 사용 사례, 버그 제보, 기능 제안, 코드 기여를 환영합니다. [기여 안내](../CONTRIBUTING.md), [보안 정책](../SECURITY.md), [저장소 정책](repository.md)을 참고하세요.

[MIT 라이선스](../LICENSE)로 배포합니다.
