# my-agent-tools

**코딩 에이전트와 개발자를 위한, 반복 작업을 한 명령으로 처리하는 자동화 도구.**

[English](../README.md) · [작업 그룹](#사용-가능한-작업-그룹) · [벤치마크](benchmarks/README.md) · [기여 안내](../CONTRIBUTING.md)

`tools`는 반복 작업을 자동화하는 크로스 플랫폼 CLI입니다. 여러 단계를 명령으로 묶어 에이전트와 개발자가 프로젝트마다 재사용하고, 프로젝트별 선호는 설정으로 관리합니다.

작업은 `tools <group> <command>` 형태로 구분합니다. GitHub는 현재 제공하는 첫 작업 그룹이며, 앞으로 다른 자동화도 별도 명령 그룹으로 추가합니다.

## 사용 가능한 작업 그룹

| 그룹 | 자동화하는 작업 | 문서 |
|---|---|---|
| `github` | 저장소 설정, 이슈, 연결 브랜치, PR·리뷰, CI 진단·재실행, Dependabot 경고, 종료된 브랜치 정리 | [GitHub 명령어](github/README.md) |

## 토큰 사용량 실측

아래 표는 기존 `gpt-6.1-sol` 실측입니다. [Astra·Luna 비교](benchmarks/github/models.md)에 9개 작업, 모델별 새 세션 54회의 평균·범위·성공률을 추가했습니다.

현재 측정 대상은 GitHub 그룹입니다. 브랜치 정리는 직접 `gh + git` 처리보다 총 토큰을 56.1% 줄였습니다. 14개 브랜치 상황을 고정한 실험 결과이며, 이슈 계획의 차이는 작았습니다.

| 명령 | 직접 `gh` / `gh + git` | `tools` | 총 토큰 차이 |
|---|---:|---:|---:|
| [`github context`](benchmarks/github/context.md) | 30,221 | 29,894 | −1.1% |
| [`github setup`](benchmarks/github/setup.md) | 53,970 | 30,473 | −43.5% |
| [`github dependabot list / view`](benchmarks/github/dependabot.md) | 31,209 | 30,715 | −1.6% |
| [`github issue create --dry-run`](benchmarks/github/issue-create.md) | 46,630 | 46,279 | −0.75% |
| [`github issue create`](benchmarks/github/issue-create.md#실제-실험-이슈브랜치-생성) | 116,141 | 34,768 | −70.1% |
| [`github issue branch`](benchmarks/github/issue-branch.md) | 113,659 | 29,904 | −73.7% |
| [`github pr create`](benchmarks/github/pr-create.md) | 102,598 | 45,076 | −56.1% |
| [`github branch cleanup`](benchmarks/github/branch-cleanup.md#미리보기) | 53,351 | 30,987 | −41.9% |
| [`github branch cleanup --apply`](benchmarks/github/branch-cleanup.md) | 70,552 | 31,006 | −56.1% |

`gpt-6.1-sol`, high 추론, 방식별 새 세션 3회의 평균입니다. 지시문·캐시 입력·출력을 포함한 에이전트 작업 전체를 측정했고, CLI 자체는 모델을 호출하지 않습니다. 새로 측정한 7개 작업은 로컬 실험 API와 실제 임시 Git 저장소를 사용했으며 최종 비교 42회 모두 정답·상태 검증을 통과했습니다. 이슈 생성은 연결 브랜치 체크아웃, 이슈 브랜치는 반복 재개까지 포함하며 Dependabot은 목록·상세를 합친 작업입니다. 기존 dry-run·실제 정리는 별도 조건의 결과입니다. 캐시를 통제하지 않았고 보정한 직접 처리 기준은 나중에 실행했으므로 총 토큰 차이가 금액 절감을 입증하지 않습니다. [원본·제외 기록·재현 방법](benchmarks/README.md).

## 설치

소스 저장소에서 **Go 1.26 이상**으로 빌드합니다.

```sh
go build -o tools ./cmd/tools
```

Windows는 `go build -o tools.exe ./cmd/tools`를 사용합니다. 바이너리 디렉터리를 PATH에 추가하세요. Linux·macOS·Windows에서 실행하며 Go 런타임은 필요하지 않습니다.

## 빠른 시작

사용 가능한 작업과 명령을 확인합니다.

```sh
tools --help
```

### GitHub

인증은 `GH_TOKEN` → `GITHUB_TOKEN` → 기존 `gh auth login` 순으로 사용합니다. 환경변수로 토큰을 제공하면 `gh` 설치는 선택 사항입니다. 이 그룹은 현재 `github.com`을 지원하며 로컬 브랜치 작업에는 Git이 필요합니다.

대상 프로젝트의 Git 작업 디렉터리에서 실행합니다. 이슈·PR 설명을 UTF-8 Markdown 파일 `issue.md`, `pr.md`로 준비하세요. 본문 언어의 기본값은 한국어입니다.

```sh
# 기존 GitHub 라벨·유형을 가져와 프로젝트 설정 저장
tools github setup

# 이슈 생성, 담당자 지정, 연결 브랜치 생성·전환
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json

# 구현·검증 후 Git으로 커밋·푸시하고 PR 생성
tools github pr create --prefix fix --title "handle duplicate requests" --body-file pr.md --json

# 제출된 리뷰 본문·미해결 대화·코드 위치 수집
tools github pr reviews --number 123 --json

# 한 번 호출해 검사 완료 대기·조건 충족 시 머지·실패 원인 반환
tools github pr merge --number 123 --json
```

생성 명령이 필요한 목록을 조회하므로 `context`는 선택 사항입니다. 생성 계획만 확인하려면 `--dry-run --json`, Git 밖에서는 `--repo OWNER/REPO`를 추가합니다. 다른 언어는 `tools github setup --body-language any`로 허용하고, 라벨 지정 등은 [setup 문서](github/setup.md)를 참고하세요.

실패한 CI job을 재실행하고 새 회차의 완료까지 기다립니다. 다시 실패하면 실패 자료를 함께 반환합니다.

```sh
tools github ci rerun --run 123456789 --json
```

전체 workflow는 `--all`, 요청 수락 후 반환은 `--wait=false`, 미리보기는 `--dry-run`을 사용합니다. 재실행은 원래 커밋을 사용하므로 수정 커밋을 푸시했다면 새 실행을 확인하세요. [옵션과 복구](github/ci-rerun.md).

의존성 보안 경고와 패치 정보를 조회합니다.

```sh
tools github dependabot list --severity high,critical --json
tools github dependabot view --number 7 --json
```

목록은 기본적으로 열린 경고를 모든 페이지에서 가져옵니다. Fine-grained 토큰에는 Dependabot alerts 읽기 권한이 필요합니다. [옵션과 결과 필드](github/dependabot.md).

PR이나 이슈 종료 후 다른 브랜치로 이동해 정리 대상을 확인합니다.

```sh
tools github branch cleanup --json
# 후보 확인 후 실제 삭제
tools github branch cleanup --apply --json
```

보호 브랜치, worktree에서 사용 중인 브랜치, 열린 PR 작업, 로컬 미게시 커밋은 보존합니다. 머지 없이 닫힌 원격 작업도 삭제 대상이므로 미리보기를 확인하세요. [정리 조건과 옵션](github/branch/cleanup.md).

## 문서

- [GitHub 명령어 안내](github/README.md) — 명령, 옵션, 설정, 실패 복구.
- [GitHub 에이전트용 빠른 사용법](agent-usage.md) — 코딩 에이전트에 전달할 짧은 작업 흐름.
- [입력·설정 예제](../examples/github) — 이슈·PR JSON, Markdown, `.tools.json`.
- [벤치마크](benchmarks/README.md) — 명령별 측정, 원본 자료, 실험 워크플로.
- [개발·개인정보 검사](development.md) · [CI 보고서 실험](ci-reports.md).

명령 목록은 `tools --help`, 세부 옵션은 각 명령의 `--help`에서 확인합니다.

## 기여

새 작업 그룹, 다른 프로젝트의 사용 사례, 버그 제보, 코드 기여를 환영합니다. [기여 안내](../CONTRIBUTING.md), [보안 정책](../SECURITY.md), [저장소 정책](repository.md)을 참고하세요.

[MIT 라이선스](../LICENSE)로 배포합니다.
